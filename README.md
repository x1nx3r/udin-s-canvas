<p align="center">
  <img src="app/assets/public/logo.svg" width="96" height="96" alt="IMPHISE" />
</p>

<h1 align="center">Ingin Menjadi Programmer Handal Namun Enggan Subscribe Excalidraw</h1>

<p align="center">
  <strong>Live: <a href="https://canvas.x1nx3r.dev" target="_blank">canvas.x1nx3r.dev</a></strong>
</p>

Draw boxes. Connect them. Call it architecture.

An Excalidraw wrapper that actually works. No SaaS emails, no subscription to unlock the felt-tip pen, no "upgrade to pro to export as PNG." Just draw.

**IMPHISE** is a Go + Templ + HTMX + Tailwind v4 app that wraps Excalidraw, persists to SQLite, shares via read-only links, ships as a single binary to a $5 VPS, and supports real-time collaboration via WebSocket — without holding your canvas data in server memory.

---

## Stack

| Layer | Choice | Reason |
|---|---|---|
| Language | Go 1.22+ | Compiles to a binary. No runtime. No `node_modules` on the server. |
| Templates | Templ | Type-safe HTML. Compile-time errors instead of runtime template panics. |
| CSS | Tailwind v4 (standalone binary) | Utility-first. Custom Go tool handles `.templ` file scanning. |
| Interactivity | HTMX | Server-rendered HTML fragments. No virtual DOM, no hydration waterfall. |
| Auth | Firebase Auth Web SDK + Admin SDK | Google sign-in. ID tokens verified server-side. http-only session cookies. |
| Database | SQLite (`mattn/go-sqlite3`) | Single file. WAL mode. Zero config. Survives deploys via symlink. |
| Canvas | Excalidraw 0.18.1 (esbuild bundle) | ~8 MB of someone else's hard work. We just render it. |
| Real-time | gorilla/websocket | Lightweight. No framework. No broker. No CRDT server-side. |
| Dev server | Air | Live reload on Go file changes. |

---

## The Big Idea

This app has one job: let you draw on a canvas and share it with people.

It does this in two modes that trade off seamlessly without you ever noticing.

### Solo Mode (The 1,500+ VU Path)

When you open a drawing alone, the server is **stateless**. Your browser runs a 3-second dirty-bit loop:

```
onChange fires → _dirty = true

setInterval(flushIfDirty, 3000)
  → if !_dirty: no-op
  → fetch POST /api/draw/{id}/save { keepalive: true }
  → on failure: _dirty = true (retry next tick)

beforeunload
  → navigator.sendBeacon('/api/draw/{id}/save', blob)
  → delivered even after the page unloads
```

No WebSocket. No goroutine per tab. No hub room. Just a POST every 3 seconds. The Go server handles this like a static file server — parse request, write to SQLite, respond. **1,500 concurrent users at 94 MB memory.** The bottleneck is the Linux OOM killer at 128 MB, not the code.

### Collaboration Mode (The Lazy Socket)

When someone opens your shared link, the server detects them and signals your page. Your browser upgrades from HTTP to WebSocket on demand:

```
1. Guest opens /shared/{slug}
2. Guest's page connects WebSocket immediately
3. Owner's page polls GET /api/draw/{id}/collab-status every 1s
4. Poll returns online: 1 → owner upgrades to WebSocket
5. Owner sends SCENE_UPDATE with full current scene
6. Both now exchange SCENE_UPDATE on every onChange
7. When last guest leaves → hub sends COLLAB_ENDED → owner downgrades to HTTP solo
```

The WebSocket is a **stateless pipe**. The Go server never interprets the canvas JSON — it just shuffles raw byte slices between connections. No CRDT merging, no element reconciliation, no state to recover after a crash. The server dies, restarts, clients reconnect, owner sends SCENE_UPDATE, everyone resyncs. **Zero data loss.**

The `lastElements` cache in the hub is purely a convenience for `SCENE_INIT` on connect. It's loaded from SQLite and disposable. SQLite is the source of truth — the hub is just a hot cache.

---

## How It Actually Works

### Auth Flow

1. User clicks "Sign in with Google."
2. Firebase Auth Web SDK opens a popup and the user authenticates.
3. Client sends the ID token to `POST /auth/login`.
4. Server verifies the token with the Firebase Admin SDK, then sets an http-only session cookie (`14 * 24h` expiry, `SameSite=Strict`).
5. `lib.Middleware` verifies the cookie on every request and injects the user's `uid` into `context.Context`.

No JWT parsing on the client. No `localStorage` tokens. No refresh token rotation. Login and logout both use HTMX `HX-Redirect` headers.

### Canvas Persistence

**Load:** When Excalidraw mounts, `excalidrawAPI` callback fires. The client fetches `GET /api/draw/{id}/data`, gets the scene JSON, and calls `api.updateScene()`. The server sanitizes `appState.collaborators` to an empty array before responding — Excalidraw crashes if that field is not an array.

**Save (solo):** 3-second dirty-bit interval + `sendBeacon` on tab close. Maximum data loss: one tick. Browser crash or power loss is not covered; that's a contract you accept with any autosave system.

**Save (collaboration):** HTTP save is disabled. Each client broadcasts `SCENE_UPDATE` over WebSocket. The owner's overlapping 3-second save still fires as a safety net (the `_collabMode` guard was removed — simplicity won over optimization).

### Wire Protocol

| Type | Direction | Payload | Purpose |
|---|---|---|---|
| `SCENE_INIT` | Server → Client | `{ elements: [...] }` | Full scene on WS connect |
| `SCENE_UPDATE` | Bidirectional | `{ payload: { elements: [...] } }` | Full scene sync |
| `MOUSE_LOCATION` | Bidirectional | `{ payload: { pointer: {x,y}, username } }` | Live cursor positions |
| `COLLAB_ENDED` | Server → Client | `{}` | Last collaborator left |

No `SCENE_DELTA`. We tried that. The client-side delta reconstruction was brittle and buggy. Full scene updates at ~50 KB per frame are fine when there are 2-3 collaborators. The OOM problem only manifests at 50+ concurrent broadcasters — a scenario that doesn't happen in practice with the lazy socket (most users are solo).

### SQLite

Single file, WAL mode, 5-second busy timeout, foreign keys on, `MaxOpenConns(1)`.

```sql
-- Core drawing record
CREATE TABLE drawings (
    id         TEXT PRIMARY KEY,
    owner_id   TEXT NOT NULL,
    title      TEXT NOT NULL DEFAULT 'Untitled',
    content    TEXT,                      -- full Excalidraw scene JSON
    share_slug TEXT UNIQUE,               -- nullable; set on first share
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Thumbnails live in a separate table so list queries don't
-- drag up to 100 KB of base64 PNG per row.
CREATE TABLE drawing_thumbnails (
    drawing_id TEXT PRIMARY KEY REFERENCES drawings(id) ON DELETE CASCADE,
    data       TEXT NOT NULL,             -- base64-encoded PNG, ≤100 KB
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

**WAL checkpoint:** A background goroutine runs `PRAGMA wal_checkpoint(PASSIVE)` every 5 minutes. Without this, the WAL file accumulates indefinitely between server restarts.

**Data persistence across deploys:** The database lives in a `shared/` directory outside the release tree, symlinked into each release. Deploys never touch the database.

### Excalidraw Bundle

Excalidraw is bundled at build time:

```bash
npx esbuild app/assets/excalidraw/entry.js \
  --bundle \
  --outfile=app/assets/public/excalidraw.bundle.js \
  --minify \
  --format=iife \
  --global-name=ExcalidrawBundle
```

The entry point exposes `Excalidraw`, `React`, `ReactDOM`, and `exportToBlob` as globals on `window.ExcalidrawBundle`. The output is ~8 MB. It lives in the binary via `//go:embed`.

### Sharing

`POST /api/draw/{id}/share` generates a 16-byte random hex slug, stores it on the drawing row, and returns it. The share dialog renders a link (`/shared/{slug}`). Anyone with the URL can view the drawing — no auth required. If `allow_public_edits` is enabled, anyone with the link can also draw.

### Thumbnails

After every successful save, `exportToBlob` renders a PNG thumbnail of the current canvas. If the blob is under 100 KB, it gets base64-encoded and stored in `drawing_thumbnails`. The dashboard renders these in a grid.

### Tailwind v4 + `.templ` Files

Tailwind v4's `@source` directive doesn't scan `.templ` files by default. Responsive variants like `sm:hidden`, `md:flex`, `lg:grid-cols-3` silently disappear from the output — no warnings, no errors, just broken layouts. The fix is `tools/generate_css/main.go`:

1. Scans all `.templ` files under `app/`
2. Extracts classes matching responsive/variant patterns
3. Writes `app/_entry.css` with `@source inline("...")` declarations
4. Compiles via standalone Tailwind binary → embedded in the binary

### CSS Cache Busting

CSS is served at `/globals.css` with `Cache-Control: public, max-age=31536000, immutable` and an `ETag` of the SHA256 hash. Template links include `?v=<hash>`. When the CSS changes, the hash changes, the URL changes, the CDN fetches a new copy.

### Excalidraw Theming

A brutalist override stylesheet remaps Excalidraw's internal CSS variables to Catppuccin tokens: `border-radius: 0` everywhere, hard offset shadows, 2px borders. The theme toggle syncs with Excalidraw via `api.updateScene({ appState: { theme } })`. A `MutationObserver` on `document.documentElement` propagates class changes to the canvas.

---

## The Load Testing Saga

### Round 1: HTTP Baseline — 1,500 VUs, 94 MB

k6 test against the live server, simulating solo HTTP dirty-bit saves:

| Metric | Value |
|---|---|
| Max VUs | 1,500 |
| Total Requests | 39,756 |
| Error Rate | **0.00%** |
| p95 latency | 3.23s (SQLite single-writer queueing) |
| Go Heap Peak | 15.04 MB |
| OS Memory Peak | 94.1 MB |
| CPU Peak | ~72.1% of one core |

The Go binary handled nearly 40,000 authenticated read/write operations from 1,500 concurrent users without dropping a single request or triggering `database is locked`. The bottleneck: the Linux OOM killer at 128 MB.

**Lesson:** HTTP-only solo mode is essentially free. The server acts as a dumb write-ahead log.

### Round 2: Full SCENE_UPDATE — 50 VUs, OOM at 128 MB

Added WebSocket collaboration with full SCENE_UPDATE on every `onChange`. k6 test at 50 VUs in one room:

| Metric | Value |
|---|---|
| ws_errors | 82 (0.68/s) |
| Sessions | 68 (18 reconnects) |
| data_sent | 52 MB |
| **MemoryPeak** | **134 MB — CRASHED** |
| **OOMKills** | **1** |

The hub's broadcast loop writing ~50 KB frames to 50 connections filled TCP buffers faster than slow clients drained them. A single `bcast DROP` cascade turned into a chain reaction: one dropped client triggers another `write FAIL`, which triggers more drops, which spawns reconnect goroutines, which spikes memory past 128 MB. systemd kills the process.

**Lesson:** Full SCENE_UPDATE at scale is a slow OOM pump. The write-buffer amplification (1 send × 50 writes) is the killer.

### Round 3: SCENE_DELTA — 50 VUs, 95.8 MB, Survived

Replaced SCENE_UPDATE with SCENE_DELTA (~1 KB per frame). Added client-side delta reconstruction. Every 10th delta sends a full SCENE_UPDATE to keep the hub's cache fresh. k6 at 50 VUs:

| Metric | Value |
|---|---|
| ws_errors | **3 (0.025/s)** ✓ |
| Sessions | **50 (exactly, zero reconnects)** |
| data_sent | **7.6 MB** |
| data_received | 474 MB |
| ws_msgs_received | 233,124 (1,942/s) |
| **MemoryPeak** | **95.8 MB** |
| **OOMKills** | **0** |

| Metric | Full SCENE_UPDATE | SCENE_DELTA |
|---|---|---|
| MemoryPeak | 134 MB (crashed) | **95.8 MB** |
| ws_errors | 82 (0.68/s) | **3 (0.025/s)** ✓ |
| data_sent | 52 MB | **7.6 MB** |
| Sessions | 68 (18 reconnects) | **50 (0 reconnects)** |

The delta approach reduced per-frame size by 50× and eliminated the write-buffer cascade. Every VU connected exactly once and stayed.

**Lesson:** SCENE_DELTA works at scale. But the client-side delta reconstruction (computeDelta → handleRemoteDelta) was fragile. Phantom diffs, stale `_lastSentElements`, race conditions between SCENE_INIT and local state. Not worth the complexity for the typical 2-3 person collaboration session.

### Round 4: Hybrid Lazy Socket — The Current Architecture

The bottleneck analysis showed two distinct ceilings:

| Mode | Ceiling | Cost per user |
|---|---|---|
| Solo (HTTP only) | **1,500+ VUs** | ~60 KB (one POST every 3s) |
| Collaboration (WS) | **~150-200 VUs** | ~1.6 MB (goroutine + buffers + broadcast) |

The Hybrid Lazy Socket combines both: default to HTTP-only (1,500+ VU ceiling), upgrade to WebSocket only when a collaborator is detected via `GET /api/draw/{id}/collab-status` polling. When the last collaborator leaves, downgrade back to HTTP solo.

The delta reconstruction was removed. The current wire protocol sends full SCENE_UPDATE frames (like Round 2) but only when actively collaborating. This is simpler, more reliable, and the bandwidth only matters during collaboration — which is the minority case.

**Final bottleneck:** The per-connection WS cost (~1.6 MB each) means ~150-200 concurrent collaborators before hitting the 128 MB ceiling. In practice, you'll never hit this because most users are solo.

---

## Route Map

| Method | Path | Handler | Auth | Response |
|---|---|---|---|---|
| `GET` | `/` | `app.PageHandler` | No | Templ (landing) |
| `GET` | `/drawings` | `dashboard.PageHandler` | Yes | Templ |
| `POST` | `/draw/new` | `dashboard.NewHandler` | Yes | Redirect |
| `GET` | `/draw/{id}` | `canvas.PageHandler` | Yes | Templ |
| `GET` | `/profile` | `profile.PageHandler` | Yes | Templ |
| `GET` | `/api/draw/{id}/data` | `api.DataHandler` | Yes | JSON |
| `POST` | `/api/draw/{id}/save` | `api.SaveHandler` | Yes | JSON |
| `POST` | `/api/draw/{id}/share` | `api.ShareHandler` | Yes | JSON |
| `PUT` | `/api/draw/{id}/rename` | `api.RenameHandler` | Yes | JSON |
| `POST` | `/api/draw/{id}/thumbnail` | `api.ThumbnailHandler` | Yes | JSON |
| `DELETE` | `/api/draw/{id}` | `api.DeleteHandler` | Yes | JSON |
| `GET` | `/shared/{slug}` | `canvas.SharedPageHandler` | No | Templ |
| `GET` | `/api/shared/{slug}/data` | `api.SharedDataHandler` | No | JSON |
| `GET` | `/api/draw/{id}/ws` | `api.OwnerWSHandler` | Yes | WS upgrade |
| `GET` | `/api/shared/{slug}/ws` | `api.GuestWSHandler` | No | WS upgrade |
| `GET` | `/api/draw/{id}/collab-status` | `api.CollabStatusHandler` | Yes | JSON (`{ online: N }`) |
| `GET` | `/api/ws/stats` | `api.WsStatsHandler` | No | Plain-text |
| `POST` | `/auth/login` | `lib.LoginHandler` | No | HX-Redirect |
| `POST` | `/auth/logout` | `lib.LogoutHandler` | No | HX-Redirect |
| `GET` | `/auth/user` | `lib.UserHandler` | No | HTML fragment |

---

## Project Structure

```
main.go                        # Route registration, static serving, server startup
app/
  lib/
    auth.go                    # Firebase Admin SDK init, session cookie helpers
    auth_handlers.go           # Login / logout / user fragment handlers
    db.go                      # SQLite init, WAL mode, schema migration, checkpoint goroutine
    middleware.go              # Session verification, RequireAuth wrapper
  api/
    draw.go                    # Data, Save, Share, Rename, Thumbnail, Delete handlers
    shared.go                  # Public shared-drawing data handler (no auth)
    hub.go                     # WS hub: Client/Room structs, connect/disconnect, broadcast, message dispatch
    ws.go                      # WS upgrade handlers (OwnerWS, GuestWS, CollabStatus), read/write pump loops
  canvas/
    page.go                    # Canvas page handler
    page.templ                 # Excalidraw editor: title bar, bridge JS, dirty-bit autosave + lazy socket
    shared.go                  # Shared canvas page handler
    shared.templ               # Read-only Excalidraw view with WS
  dashboard/
    page.go                    # Dashboard handler + NewDrawing handler
    page.templ                 # Drawing grid, empty state, new drawing button
  profile/
    page.go                    # Profile handler
    page.templ                 # User info display
  components/
    navigation.templ           # Desktop nav + hamburger menu
    logo.templ                 # Icon-only on mobile, text on sm:+
    drawing_card.templ         # Thumbnail card with rename / delete
    footer.templ               # GOTTH badge + copyright
    empty_state.templ          # "No drawings yet" CTA
  layout.templ                 # Root HTML shell (fonts, Firebase SDK, HTMX, PWA meta)
  canvas_layout.templ          # Minimal shell for canvas pages
  page.templ                   # Landing page (hero, feature grid, CTA)
  globals.css                  # Catppuccin tokens, design system, Tailwind directives
  _entry.css                   # Generated: @source inline() + globals.css (do not hand-edit)
  _responsive.css              # Manually curated responsive overrides
  assets/
    excalidraw/
      entry.js                 # Excalidraw esbuild entry point
      package.json             # Pinned: excalidraw 0.18.1, react 18.3.1
    public/
      excalidraw.bundle.js     # Built by `make bundle` (~8 MB, embedded in binary)
      excalidraw.css           # Excalidraw's own CSS (embedded)
      logo.svg                 # App logo
      manifest.json            # PWA manifest
    assets.go                  # //go:embed directives + CSSHash (SHA256)
tools/
  generate_css/main.go         # Scan .templ → extract responsive classes → _entry.css
load_tests/
  k6_ws_test.js                # k6 WebSocket load test script
  PROFILES.md                  # pprof analysis and comparison
  SCENE_DELTA-PLAN.md          # SCENE_DELTA implementation plan (archived)
  POSTMORTEM-2026-07-10.md     # OOM crash analysis (archived)
  REPORT-2026-07-11.md         # SCENE_DELTA load test report (archived)
```

---

## Design System

**Catppuccin Brutalist.** Sharp corners, hard offset shadows, Catppuccin palette.

- **Fonts:** Bungee (headings), Space Mono (body/UI/mono)
- **Borders:** 2px solid. Always.
- **Shadows:** Hard offset — `2px 2px`, `4px 4px`, `6px 6px`, `8px 8px`. No blur. No spread.
- **Radius:** Zero. No exceptions.
- **Buttons:** Translate on active (`translate-x-0.5 translate-y-0.5`), shadow collapses to zero.
- **Grid:** Subtle colored grid lines (pink/blue in light mode, mauve/lavender in dark).
- **Dark mode:** Defaults to system preference. Toggle persists to `localStorage`.

### Color Tokens

| Token | Latte (Light) | Mocha (Dark) | Usage |
|---|---|---|---|
| `--bg` | `#eff1f5` | `#1e1e2e` | Page background |
| `--fg` | `#4c4f69` | `#cdd6f4` | Body text |
| `--fg-muted` | `#8c8fa1` | `#6c7086` | Secondary text |
| `--bg-subtle` | `#e6e9ef` | `#181825` | Card backgrounds |
| `--border` | `#4c4f69` | `#cdd6f4` | All borders |
| `--accent` | `#8839ef` | `#cba6f7` | Primary actions, focus rings |
| `--mauve` | `#8839ef` | `#cba6f7` | Secondary accent |
| `--pink` | `#ea76cb` | `#f5c2e7` | Feature cards, hover states |
| `--peach` | `#fe640b` | `#fab387` | Accent borders |
| `--teal` | `#179299` | `#94e2d5` | Feature cards |
| `--blue` | `#1e66f5` | `#89b4fa` | Grid lines, links |
| `--lavender` | `#7287fd` | `#b4befe` | Grid lines, secondary |

---

## Deployment

Ships as a single binary to a bare-metal VPS via atomic symlink swap. Zero downtime.

```bash
bash deploy.sh
```

### What `deploy.sh` Does

1. **Local build:** `make css && make templ && CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build`
2. **rsync:** Binary + Firebase service account JSON + server-side `Makefile.server` → timestamped release dir on the server
3. **Atomic swap:** `ln -nfs` points `current/` → new release directory
4. **Symlink DB:** `canvas.db` in `shared/` is symlinked into each release — database never touched by a deploy
5. **Restart:** `systemctl restart udin-canvas`
6. **Cleanup:** Old releases archived to `.tar.xz`, keeping the last 5

Binary is ~61 MB (Excalidraw bundle embedded). Deploy takes ~50 seconds.

### Server Setup

| Concern | Solution |
|---|---|
| Reverse proxy | Caddy → app port |
| Process manager | systemd unit with `MemoryMax=128M` |
| Database | Shared SQLite file, persists via symlinks |
| CDN | Cloudflare (aggressive caching; purge on CSS change) |
| Peak memory | ~16 MB solo (HTTP), ~96 MB for 50 WS collaborators |

### Environment Variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `PORT` | No | `3000` | HTTP listen port |
| `SQLITE_DB_PATH` | No | `./canvas.db` | Path to SQLite database file |

Firebase service account: auto-detected by scanning for `*-firebase-adminsdk-*.json` in the working directory.

---

## The Suffering Log

Things that went wrong, in roughly chronological order.

1. **Firestore → SQLite** — Moved from a managed service to a single file. Rewrote every query handler. Worth it.

2. **Login/logout routing** — The logout form had `hx-boost="false"` which silently swallowed the `HX-Redirect` header. Removed it. Fixed.

3. **Excalidraw canvas font** — `body { font-family: 'Space Mono' }` in `globals.css` leaked into Excalidraw's canvas text input. Added a reset in `excalidraw-brutalist.css` targeting `.excalidraw .text-editor`. Partially fixed.

4. **Tailwind v4 responsive classes silently disappearing** — `sm:hidden`, `md:flex`, `lg:grid-cols-3` were all missing from CSS output. No warnings. No errors. Just broken layouts. Built a Go tool to scan `.templ` files and inject them via `@source inline()`.

5. **Cloudflare caching stale CSS** — `Cache-Control: no-cache` was being overwritten by Cloudflare. Switched to content-hashed URLs with `immutable`. Purge on deploy anyway.

6. **Auth bar OOB swap** — `UserHandler` returns HTML for both `#auth-bar` (desktop) and `#auth-bar-mobile` (mobile) with `hx-swap-oob="true"`. Required restructuring the handler to return both elements in a single response.

7. **Autosave race on tab close** — The `setTimeout` debounce reset on every `onChange` event. User closes tab within the 2-second window → final `fetch` never fires → stale data on reopen. Replaced with dirty-bit + `setInterval` + `sendBeacon` on `beforeunload`.

8. **`canvas.db-wal` grew to 4.2 MB** — SQLite's auto-checkpoint threshold is 1000 pages. Without explicit checkpointing, the WAL accumulates between server restarts. Fixed with `PRAGMA wal_checkpoint(PASSIVE)` on a 5-minute ticker.

9. **WebSocket `socket.readyState` is undefined in k6** — k6 v2.1.0's Socket object does not expose `readyState`; the guard `if (socket.readyState === 1)` silently blocked all SCENE_UPDATE sends during the first WS load test. Root cause of 0 SCENE_UPDATE in the first run. Removed the guard entirely.

10. **WebSocket OOM at 128 MB with full SCENE_UPDATE** — The hub's broadcast loop writing ~50 KB SCENE_UPDATE frames to 50 connections filled TCP buffers faster than slow clients drained them, cascading into OOM. The broadcast fan-out (1 send → 50 writes) amplified a single slow client into a chain reaction: DROP → write FAIL → reconnect → more goroutines → memory spike → systemd kill. Switched to SCENE_DELTA (~1 KB per frame), which eliminated the buffer cascade.

11. **SCENE_DELTA frontend missing `_sceneElements` update** — `handleRemoteScene` updated Excalidraw via `api.updateScene` but didn't sync `_sceneElements`. The next `computeDelta` computed against stale elements, sending phantom diffs of the entire scene. Fixed by syncing `_sceneElements` after every remote update.

12. **SCENE_DELTA client-side reconstruction was fundamentally fragile** — The delta approach worked at scale (50 VUs, 95.8 MB, zero reconnects), but the client code was a nest of race conditions. `computeDelta` vs `_lastSentElements`, `handleRemoteDelta` vs `_sceneElements`, SCENE_INIT overwriting local state during handshake. The complexity wasn't worth it for the 2-3 person collaboration session that is 99% of real usage. Reverted to full SCENE_UPDATE, backed by the lazy socket to keep solo users on the HTTP-only path.

---

## Getting Started

**Prerequisites:** Go 1.22+, Node.js (for the esbuild bundle step), `gcc` (for CGO/SQLite).

```bash
make setup   # installs templ, air, standalone tailwind binary; runs npm install
make dev     # css watch + air live-reload server on :3000
```

Or bare minimum, if the Excalidraw bundle and CSS already exist:

```bash
go run .
```

Open [http://localhost:3000](http://localhost:3000). Sign in with Google. Create a drawing. It saves automatically.

> **CGO note:** `mattn/go-sqlite3` requires `CGO_ENABLED=1`. On macOS it works out of the box. On Linux, `gcc` must be installed. The deploy script cross-compiles with `CGO_ENABLED=1 GOOS=linux GOARCH=amd64`.

### Make Targets

| Target | What it does |
|---|---|
| `make setup` | Install `templ`, `air`, download standalone Tailwind binary, run `npm install`, generate templ files |
| `make dev` | Compile CSS, start Tailwind watcher + Air live-reload server |
| `make templ` | Re-generate `*_templ.go` from `.templ` files |
| `make css` | Run `generate-css` then compile Tailwind → `app/assets/globals.css.output` |
| `make generate-css` | Scan `.templ` files, extract responsive classes, write `app/_entry.css` |
| `make bundle` | esbuild Excalidraw entry → `app/assets/public/excalidraw.bundle.js` |
| `make build` | Full production build: css + templ + bundle + `go build` |

---

## Why Not Just Use Excalidraw's Built-In Save?

Excalidraw has a "save to disk" button. That works fine for personal use. It doesn't work for "I want to open this diagram on a different machine without emailing myself a JSON file."

SQLite is simpler than any managed database. No indexes to manage, no billing surprises, no document size limits. A single `canvas.db` file. Backed up with `cp`.

---

## The Accidental React

We started this project specifically to **avoid React**. HTMX's server-rendered fragments were supposed to handle everything. And they did — for auth, navigation, the dashboard, profile pages, every "normal" web thing.

But a canvas isn't a normal web thing.

Excalidraw ships as a React component. You can't render it with HTMX. You can't sprinkle it with `hx-trigger`. You have to mount a React root, hand it props, and wire up the `excalidrawAPI` callback. The inline `<script>` block in `page.templ` grew from 10 lines of glue code to 200+ lines managing WebSocket state, dirty-bit autosave, collaborator detection, and scene reconciliation.

We tried to be clever. We built a delta protocol to save bandwidth. We wrote `computeDelta`, `handleRemoteDelta`, `_lastSentElements`, `_deltaCount`. We debugged phantom diffs, stale caches, and race conditions between SCENE_INIT and local state. It worked on the load tester. It broke in the browser.

In the end, we deleted all of it. The current code sends the full scene on every change — the same approach we started with, before the OOM crash convinced us we needed something smarter. The difference is the lazy socket: solo users never open a WebSocket, so the full-scene broadcast only affects the 2-3 people actually collaborating. The OOM problem at 50 broadcasters is academic when 49 of those users don't have a socket open.

**We started not wanting to do React, we ended up doing React, in its intended way.** The React root renders Excalidraw. The inline script handles the bridge logic. HTMX handles everything else. Each tool does what it's good at.

---

## License

**WTFPL — Do What the Fuck You Want To Public License.**

Copy the code. Sell it. Put your name on it. Light your server on fire with it. I don't care. The author is not responsible for anything that happens as a result of using this software, including but not limited to: database corruption, emotional damage from debugging WebSocket cascades, or your boss asking why the architecture diagram looks like a plate of spaghetti.

This is free software. Go nuts.

*Excalidraw is separately licensed under MIT — see NOTICE.*
