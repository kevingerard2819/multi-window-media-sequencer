# Sequence Studio

A React and Go application for independent media playlists across three display panels. Each panel loops its ordered playlist throughout a five-hour normal playback cycle. A sync command temporarily shows one selected item on every panel, then resumes each panel's normal position. Storage uses a local JSON file by default or PostgreSQL when `DATABASE_URL` is set.

**Live demo:** https://sequence-studio.onrender.com

## Quick start

With Docker installed:

```sh
docker compose up --build
```

Open `http://localhost:8080`. The application seeds three sample windows on first launch. Data is saved in `./data/state.json` through a bind mount and survives restarts. The seed images are bundled with the frontend. The seed video uses an external MDN example URL and needs an internet connection.

To run development servers separately, install Go 1.25+ and Node 24+ with pnpm, then:

```sh
cd backend
go run .
```

In a second terminal:

```sh
cd frontend
pnpm install
pnpm dev
```

The Go server listens on `:8080`; Vite listens on its displayed port and proxies `/api` to Go. The backend's development data path is `backend/data/state.json` when started inside `backend/`. Set `DATA_FILE` to choose another path. If serving a built frontend outside Docker, set `WEB_DIR` to the absolute path of `frontend/dist`.

## Playback rules and assumptions

- A “window” is a panel in one React page. The three panels share one browser clock. Independent browsers fetch the same server timeline, though internet latency and video buffering can cause visible differences between devices.
- The normal playback timeline begins when the data file is first created. Each panel loops its playlist as many times as necessary. At each five-hour boundary, the playlist starts at its first item, even if the last item would otherwise continue past the boundary.
- Every playlist entry has an explicit duration. Images remain visible for that duration. Videos loop if shorter than their entry duration and are cut off if longer. Blank appears only when explicitly included or when a playlist is empty or media fails to load.
- A sync starts 1.2 seconds after the server accepts the command. Its start and end timestamps are shared with all clients. The normal timeline pauses during sync, then resumes where it stopped. A second sync is rejected while one is pending or active.
- Adding media appends it without interrupting the currently playing item. A per-window phase offset preserves the current item position until the next five-hour boundary, where the offset resets.
- Media URLs must be public HTTP or HTTPS addresses. Video autoplay is muted to satisfy browser autoplay rules.
- The assignment references example seed windows and lists but does not include them, so the bundled seed data is illustrative.

The first launch creates these illustrative playlists (each row shows media type and duration):

| Display | Playlist order |
| --- | --- |
| Atrium | Horizon image (12s) -> Flower film video (10s) -> Terrain image (12s) |
| Gallery | Terrain image (14s) -> Orbit image (14s) -> Quiet interval blank (5s) |
| Studio | Orbit image (11s) -> Flower film video (9s) -> Horizon image (11s) |

These are examples, not a claim that they match omitted assignment media lists. Seed images ship with the frontend; the Flower film uses a public MDN video URL.

## API

All errors use `{ "error": "message" }`.

| Method | Path | Description |
|---|---|---|
| GET | `/health` | Health check |
| GET | `/api/state` | Current windows, server time, normal elapsed time, cycle duration, and optional sync |
| POST | `/api/windows/{id}/items` | Append a playlist item |
| POST | `/api/sync` | Trigger synchronized playback |
| GET | `/api/events` | Server-sent `change` events; refetch `/api/state` after each event |

Add an item:

```sh
curl -X POST http://localhost:8080/api/windows/window-1/items \
  -H 'Content-Type: application/json' \
  -d '{"name":"Campaign slide","type":"image","url":"https://example.com/slide.jpg","durationMs":10000}'
```

`type` is `image`, `video`, or `blank`. For blank items, use an empty `url`. Duration must be 1 second to 1 hour.

Trigger sync using a media ID from `/api/state`:

```sh
curl -X POST http://localhost:8080/api/sync \
  -H 'Content-Type: application/json' \
  -d '{"mediaId":"horizon","durationMs":10000}'
```

Sync duration must be 1 second to 5 minutes. The server returns `409` if a sync is already pending or active.

## Deployment

Build the included Dockerfile and run one container with port `8080` exposed, HTTPS supplied by the hosting platform, and a persistent volume mounted at `/data` unless using PostgreSQL. Set `PORT`, `DATA_FILE`, and `WEB_DIR` if the defaults do not fit the host. Set `DATABASE_URL` to a PostgreSQL connection string to store state in the database instead. Both the React build and Go API are served from the same origin, so no CORS configuration is required. Configure the host's health check to `/health`.

The included `render.yaml` creates one free Docker web service and one free Render Postgres database in Frankfurt. Connect a Git repository containing this project to a new Render Blueprint, then review and create both resources. The web service receives `DATABASE_URL` automatically. Render free web services spin down when idle, so the first visit after inactivity can take longer. Free Postgres databases expire after 30 days unless upgraded; export or upgrade the database if the demo must stay live longer. The service runs as one instance because it keeps an in-memory copy of state and uses an in-process event stream. The live service uses the URL above.

## Verification

```sh
cd backend && go test ./...
cd frontend && pnpm build && node --test test/*.test.mjs
```

The frontend scheduling tests cover playlist looping, the five-hour reset, sync timeline pausing, and uninterrupted appends. Backend tests cover persistence, API validation, and sync state.
