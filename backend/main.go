package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const cycleMs int64 = 5 * 60 * 60 * 1000

type Media struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
	URL  string `json:"url"`
}

type Item struct {
	ID         string `json:"id"`
	Media      Media  `json:"media"`
	DurationMs int64  `json:"durationMs"`
}

type Window struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Items            []Item `json:"items"`
	PhaseOffsetMs    int64  `json:"phaseOffsetMs"`
	PhaseOffsetCycle int64  `json:"phaseOffsetCycle"`
}

type SyncEvent struct {
	ID       string `json:"id"`
	Media    Media  `json:"media"`
	StartsAt int64  `json:"startsAt"`
	EndsAt   int64  `json:"endsAt"`
}

type persisted struct {
	CycleStartedAt   int64      `json:"cycleStartedAt"`
	CompletedPauseMs int64      `json:"completedPauseMs"`
	Windows          []Window   `json:"windows"`
	Sync             *SyncEvent `json:"sync,omitempty"`
}

type stateResponse struct {
	ServerTime      int64      `json:"serverTime"`
	CycleMs         int64      `json:"cycleMs"`
	NormalElapsedMs int64      `json:"normalElapsedMs"`
	Windows         []Window   `json:"windows"`
	Sync            *SyncEvent `json:"sync"`
}

type server struct {
	mu      sync.Mutex
	data    persisted
	path    string
	db      *sql.DB
	clients map[chan struct{}]struct{}
}

func seed(now int64) persisted {
	image := func(id, name string) Media { return Media{id, name, "image", "/media/" + id + ".svg"} }
	video := Media{"flower", "Flower film", "video", "https://interactive-examples.mdn.mozilla.net/media/cc0-videos/flower.mp4"}
	blank := Media{"blank", "Quiet interval", "blank", ""}
	item := func(id string, media Media, seconds int64) Item { return Item{id, media, seconds * 1000} }
	return persisted{CycleStartedAt: now, Windows: []Window{
		{ID: "window-1", Name: "Atrium", Items: []Item{item("entry-1", image("horizon", "Horizon"), 12), item("entry-2", video, 10), item("entry-3", image("terrain", "Terrain"), 12)}},
		{ID: "window-2", Name: "Gallery", Items: []Item{item("entry-4", image("terrain", "Terrain"), 14), item("entry-5", image("orbit", "Orbit"), 14), item("entry-6", blank, 5)}},
		{ID: "window-3", Name: "Studio", Items: []Item{item("entry-7", image("orbit", "Orbit"), 11), item("entry-8", video, 9), item("entry-9", image("horizon", "Horizon"), 11)}},
	}}
}

func newServer(path string) (*server, error) {
	s := &server{path: path, clients: make(map[chan struct{}]struct{})}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		s.data = seed(time.Now().UnixMilli())
		return s, s.saveLocked()
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &s.data); err != nil {
		return nil, fmt.Errorf("read data: %w", err)
	}
	if s.data.CycleStartedAt == 0 {
		return nil, errors.New("data file has no cycle start")
	}
	s.finishSyncLocked(time.Now().UnixMilli())
	return s, nil
}

func (s *server) saveLocked() error {
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	if s.db != nil {
		_, err = s.db.Exec("INSERT INTO sequencer_state (id, payload) VALUES (1, $1) ON CONFLICT (id) DO UPDATE SET payload = EXCLUDED.payload", string(raw))
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".state-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	// Windows cannot replace an open destination with Rename, so remove it first there.
	if _, err = os.Stat(s.path); err == nil && os.PathSeparator == '\\' {
		if err = os.Remove(s.path); err != nil {
			return err
		}
	}
	return os.Rename(tmp.Name(), s.path)
}

func (s *server) finishSyncLocked(now int64) {
	if s.data.Sync == nil || now < s.data.Sync.EndsAt {
		return
	}
	completed := s.data.Sync
	s.data.CompletedPauseMs += s.data.Sync.EndsAt - s.data.Sync.StartsAt
	s.data.Sync = nil
	if err := s.saveLocked(); err != nil {
		log.Printf("save finished sync: %v", err)
		s.data.Sync = completed
		s.data.CompletedPauseMs -= completed.EndsAt - completed.StartsAt
		return
	}
	s.broadcastLocked()
}

func (s *server) broadcastLocked() {
	for client := range s.clients {
		select {
		case client <- struct{}{}:
		default:
		}
	}
}

func (s *server) snapshotLocked(now int64) stateResponse {
	s.finishSyncLocked(now)
	elapsed := now - s.data.CycleStartedAt - s.data.CompletedPauseMs
	if current := s.data.Sync; current != nil && now > current.StartsAt {
		pauseEnd := now
		if pauseEnd > current.EndsAt {
			pauseEnd = current.EndsAt
		}
		elapsed -= pauseEnd - current.StartsAt
	}
	if elapsed < 0 {
		elapsed = 0
	}
	windows := make([]Window, len(s.data.Windows))
	for i, window := range s.data.Windows {
		windows[i] = window
		windows[i].Items = append([]Item(nil), window.Items...)
	}
	var current *SyncEvent
	if s.data.Sync != nil {
		copy := *s.data.Sync
		current = &copy
	}
	return stateResponse{now, cycleMs, elapsed, windows, current}
}

func playlistDuration(items []Item) int64 {
	var total int64
	for _, item := range items {
		if item.DurationMs > 0 {
			total += item.DurationMs
		}
	}
	return total
}

func jsonResponse(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func jsonError(w http.ResponseWriter, status int, message string) {
	jsonResponse(w, status, map[string]string{"error": message})
}

func readJSON(r *http.Request, value any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 64*1024)
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return errors.New("only one JSON value is allowed")
	}
	return nil
}

func validMedia(m Media) error {
	if strings.TrimSpace(m.Name) == "" || len(m.Name) > 120 {
		return errors.New("media name must be 1-120 characters")
	}
	if m.Type != "image" && m.Type != "video" && m.Type != "blank" {
		return errors.New("media type must be image, video, or blank")
	}
	if m.Type == "blank" {
		if m.URL != "" {
			return errors.New("blank media must have no URL")
		}
		return nil
	}
	if len(m.URL) > 2048 {
		return errors.New("URL is too long")
	}
	u, err := url.Parse(m.URL)
	if err != nil {
		return errors.New("invalid media URL")
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("media URL must be HTTP or HTTPS")
	}
	return nil
}

func (s *server) routes(webDir string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { jsonResponse(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		state := s.snapshotLocked(time.Now().UnixMilli())
		s.mu.Unlock()
		jsonResponse(w, 200, state)
	})
	mux.HandleFunc("POST /api/windows/{id}/items", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Name       string `json:"name"`
			Type       string `json:"type"`
			URL        string `json:"url"`
			DurationMs int64  `json:"durationMs"`
		}
		if err := readJSON(r, &input); err != nil {
			jsonError(w, 400, err.Error())
			return
		}
		media := Media{ID: fmt.Sprintf("media-%d", time.Now().UnixNano()), Name: strings.TrimSpace(input.Name), Type: input.Type, URL: input.URL}
		if err := validMedia(media); err != nil {
			jsonError(w, 400, err.Error())
			return
		}
		if input.DurationMs < 1000 || input.DurationMs > 60*60*1000 {
			jsonError(w, 400, "duration must be between 1 second and 1 hour")
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		for i := range s.data.Windows {
			if s.data.Windows[i].ID == r.PathValue("id") {
				window := &s.data.Windows[i]
				before := *window
				at := s.snapshotLocked(time.Now().UnixMilli()).NormalElapsedMs
				cycle := at / cycleMs
				cyclePosition := at % cycleMs
				oldTotal := playlistDuration(window.Items)
				oldPosition := int64(0)
				if oldTotal > 0 {
					oldPosition = cyclePosition
					if window.PhaseOffsetCycle == cycle {
						oldPosition += window.PhaseOffsetMs
					}
					oldPosition = ((oldPosition % oldTotal) + oldTotal) % oldTotal
				}
				item := Item{ID: fmt.Sprintf("item-%d", time.Now().UnixNano()), Media: media, DurationMs: input.DurationMs}
				window.Items = append(window.Items, item)
				if oldTotal > 0 {
					window.PhaseOffsetMs = oldPosition - cyclePosition
					window.PhaseOffsetCycle = cycle
				} else {
					window.PhaseOffsetMs = 0
					window.PhaseOffsetCycle = cycle
				}
				if err := s.saveLocked(); err != nil {
					*window = before
					jsonError(w, 500, "could not save item")
					return
				}
				s.broadcastLocked()
				jsonResponse(w, 201, item)
				return
			}
		}
		jsonError(w, 404, "window not found")
	})
	mux.HandleFunc("POST /api/sync", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			MediaID    string `json:"mediaId"`
			DurationMs int64  `json:"durationMs"`
		}
		if err := readJSON(r, &input); err != nil {
			jsonError(w, 400, err.Error())
			return
		}
		if input.DurationMs < 1000 || input.DurationMs > 5*60*1000 {
			jsonError(w, 400, "sync duration must be between 1 second and 5 minutes")
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		now := time.Now().UnixMilli()
		s.finishSyncLocked(now)
		if s.data.Sync != nil {
			jsonError(w, 409, "sync already active")
			return
		}
		var selected *Media
		for _, window := range s.data.Windows {
			for _, item := range window.Items {
				if item.Media.ID == input.MediaID {
					copy := item.Media
					selected = &copy
					break
				}
			}
		}
		if selected == nil {
			jsonError(w, 404, "media not found in playlists")
			return
		}
		starts := now + 1200
		event := &SyncEvent{ID: fmt.Sprintf("sync-%d", time.Now().UnixNano()), Media: *selected, StartsAt: starts, EndsAt: starts + input.DurationMs}
		s.data.Sync = event
		if err := s.saveLocked(); err != nil {
			s.data.Sync = nil
			jsonError(w, 500, "could not save sync")
			return
		}
		s.broadcastLocked()
		time.AfterFunc(time.Duration(event.EndsAt-now)*time.Millisecond, func() { s.mu.Lock(); s.finishSyncLocked(time.Now().UnixMilli()); s.mu.Unlock() })
		jsonResponse(w, 201, event)
	})
	mux.HandleFunc("GET /api/events", func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			jsonError(w, 500, "streaming unavailable")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		ch := make(chan struct{}, 1)
		s.mu.Lock()
		s.clients[ch] = struct{}{}
		s.mu.Unlock()
		defer func() { s.mu.Lock(); delete(s.clients, ch); s.mu.Unlock() }()
		fmt.Fprint(w, "event: change\ndata: {}\n\n")
		flusher.Flush()
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-ch:
				fmt.Fprint(w, "event: change\ndata: {}\n\n")
				flusher.Flush()
			case <-ticker.C:
				fmt.Fprint(w, ": ping\n\n")
				flusher.Flush()
			}
		}
	})
	files := http.FileServer(http.Dir(webDir))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Join(webDir, filepath.Clean(strings.TrimPrefix(r.URL.Path, "/")))
		if stat, err := os.Stat(path); err == nil && !stat.IsDir() {
			files.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			jsonError(w, 404, "not found")
			return
		}
		http.ServeFile(w, r, filepath.Join(webDir, "index.html"))
	})
	return mux
}

func main() {
	path := os.Getenv("DATA_FILE")
	if path == "" {
		path = "data/state.json"
	}
	webDir := os.Getenv("WEB_DIR")
	if webDir == "" {
		webDir = "../frontend/dist"
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	var s *server
	var err error
	if databaseURL := os.Getenv("DATABASE_URL"); databaseURL != "" {
		s, err = newDatabaseServer(databaseURL)
	} else {
		s, err = newServer(path)
	}
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, s.routes(webDir)))
}
