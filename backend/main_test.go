package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestSnapshotPausesNormalTimeline(t *testing.T) {
	s := &server{
		path:    filepath.Join(t.TempDir(), "state.json"),
		clients: map[chan struct{}]struct{}{},
		data: persisted{
			CycleStartedAt: 1_000,
			Sync:           &SyncEvent{StartsAt: 12_000, EndsAt: 17_000},
		},
	}
	if got := s.snapshotLocked(14_000).NormalElapsedMs; got != 11_000 {
		t.Fatalf("during sync elapsed = %d, want 11000", got)
	}
	if got := s.snapshotLocked(18_000).NormalElapsedMs; got != 12_000 {
		t.Fatalf("after sync elapsed = %d, want 12000", got)
	}
	if s.data.Sync != nil || s.data.CompletedPauseMs != 5_000 {
		t.Fatal("completed sync was not finalized")
	}
}

func TestAppendPersistsAndPreservesPhase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := newServer(path)
	if err != nil {
		t.Fatal(err)
	}
	before := s.data.Windows[0]
	body := []byte(`{"name":"New slide","type":"image","url":"https://example.com/new.jpg","durationMs":10000}`)
	request := httptest.NewRequest(http.MethodPost, "/api/windows/window-1/items", bytes.NewReader(body))
	response := httptest.NewRecorder()
	s.routes("unused").ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("append status = %d: %s", response.Code, response.Body.String())
	}
	if len(s.data.Windows[0].Items) != len(before.Items)+1 {
		t.Fatal("item was not appended")
	}
	after := s.data.Windows[0]
	now := s.snapshotLocked(time.Now().UnixMilli()).NormalElapsedMs
	oldTotal := playlistDuration(before.Items)
	newTotal := playlistDuration(after.Items)
	cyclePosition := now % cycleMs
	oldPhase := cyclePosition
	if before.PhaseOffsetCycle == now/cycleMs {
		oldPhase += before.PhaseOffsetMs
	}
	newPhase := cyclePosition + after.PhaseOffsetMs
	if ((oldPhase%oldTotal)+oldTotal)%oldTotal != ((newPhase%newTotal)+newTotal)%newTotal {
		t.Fatal("append changed current playlist position")
	}
	reloaded, err := newServer(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.data.Windows[0].Items) != len(before.Items)+1 {
		t.Fatal("appended item did not survive reload")
	}
}

func TestSyncRejectsConcurrentRequest(t *testing.T) {
	s, err := newServer(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	handler := s.routes("unused")
	body, _ := json.Marshal(map[string]any{"mediaId": "horizon", "durationMs": 5000})
	for i, want := range []int{http.StatusCreated, http.StatusConflict} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/sync", bytes.NewReader(body)))
		if response.Code != want {
			t.Fatalf("request %d status = %d, want %d: %s", i, response.Code, want, response.Body.String())
		}
	}
}
