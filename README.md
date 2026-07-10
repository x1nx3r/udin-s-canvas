<p align="center">
  <img src="app/assets/public/logo.svg" width="96" height="96" alt="IMPHISE" />
</p>

<h1 align="center">Ingin Menjadi Programmer Handal Namun Enggan Subscribe Excalidraw</h1>

<p align="center">
  <strong>Live: <a href="https://canvas.x1nx3r.dev" target="_blank">canvas.x1nx3r.dev</a></strong>
</p>

Draw boxes. Connect them. Call it architecture.

An Excalidraw wrapper that actually works. No SaaS emails, no subscription to unlock the felt-tip pen, no "upgrade to pro to export as PNG." Just draw.

**IMPHISE** is a Go + Templ + HTMX + Tailwind v4 app that wraps Excalidraw, persists to SQLite, shares via read-only links, and ships as a single binary to a $5 VPS. Peak memory: ~16 MB.

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
| Dev server | Air | Live reload on Go file changes. |

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

> **CGO note:** `mattn/go-sqlite3` requires `CGO_ENABLED=1`. On macOS it works out of the box. On Linux, `gcc` must be installed (usually already there). The deploy script cross-compiles with `CGO_ENABLED=1 GOOS=linux GOARCH=amd64`.

---

## Make Targets

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

## How It Works

### Auth Flow

1. User clicks "Sign in with Google."
2. Firebase Auth Web SDK opens a popup and the user authenticates.
3. Client sends the ID token to `POST /auth/login`.
4. Server verifies the token with the Firebase Admin SDK, then sets an http-only session cookie (`14 * 24h` expiry, `SameSite=Strict`).
5. `lib.Middleware` verifies the cookie on every request and injects the user's `uid` into `context.Context`.

No JWT parsing on the client. No `localStorage` tokens. No refresh token rotation.

Login and logout both use HTMX `HX-Redirect` headers — the form `POST` triggers a full page navigation without any client-side JS routing.

---

### Canvas: Save & Load

**Load:** When Excalidraw mounts, `excalidrawAPI` callback fires. The client fetches `GET /api/draw/{id}/data`, gets the scene JSON, and calls `api.updateScene()`. The server sanitizes `appState.collaborators` to an empty array before responding — Excalidraw crashes if that field is not an array.

**Autosave — dirty-bit pattern:**

```
onChange fires (every element move, keypress, etc.)
  → _scene = { elements, appState }   // O(1), no timers created
  → _dirty = true
```

```
setInterval(flushIfDirty, 3000)
  → if !_dirty: no-op
  → _dirty = false   // claim before async — prevents double-send
  → snapshot = _scene
  → fetch POST /api/draw/{id}/save  { keepalive: true }
  → on success: generateThumbnail()
  → on failure: _dirty = true        // restore; next tick retries
```

```
beforeunload
  → if !_dirty: no-op (interval already flushed)
  → navigator.sendBeacon('/api/draw/{id}/save', JSON blob)
  → browser delivers this even after the page unloads
```

The `saveDrawing()` button calls `flushIfDirty()` directly — one code path for all three triggers. Maximum accepted data loss: ~3 seconds (one interval tick). Browser crash or power loss is not covered; that's a contract you accept with any autosave system.

**Why not the old `setTimeout` debounce?**
The previous implementation reset a 2-second timer on every `onChange` event. If the user drew continuously and closed the tab within that window, the final `fetch` never fired and the user saw stale data on the next open. The dirty-bit pattern eliminates the timer storm entirely and delegates the flush to the interval + beacon.

---

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
CREATE INDEX idx_drawings_owner      ON drawings(owner_id);
CREATE INDEX idx_drawings_share_slug ON drawings(share_slug);

-- Thumbnails live in a separate table so list queries don't
-- drag up to 100 KB of base64 PNG per row.
CREATE TABLE drawing_thumbnails (
    drawing_id TEXT PRIMARY KEY REFERENCES drawings(id) ON DELETE CASCADE,
    data       TEXT NOT NULL,             -- base64-encoded PNG, ≤100 KB
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

**WAL checkpoint:** A background goroutine runs `PRAGMA wal_checkpoint(PASSIVE)` every 5 minutes. PASSIVE mode never blocks readers or writers — it just reclaims WAL pages when no readers are holding a snapshot. Without this, the WAL file accumulates indefinitely between server restarts.

**Data persistence across deploys:** The database lives in `/var/www/udin-canvas/shared/canvas.db` and is symlinked into each release directory. Deploys never touch the database.

---

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

The entry point exposes `Excalidraw`, `React`, `ReactDOM`, and `exportToBlob` as globals on `window.ExcalidrawBundle`. The output is ~8 MB. It lives in the binary via `//go:embed`. The CSS is a separate embedded file linked in the page head.

Versions are pinned to exact (`@excalidraw/excalidraw: 0.18.1`, `react: 18.3.1`, `react-dom: 18.3.1`) because Excalidraw's internal CSS class names shift between versions and our override stylesheet depends on specific selectors.

---

### Sharing

`POST /api/draw/{id}/share` generates a 16-byte random hex slug, stores it on the drawing row, and returns it. The share dialog renders a read-only link (`/shared/{slug}`). Anyone with the URL can view the drawing — no auth required.

The shared page renders Excalidraw with `viewModeEnabled: true`. Scene data is served by `GET /api/shared/{slug}/data`, which is a public endpoint (no auth middleware) that queries by `share_slug`.

---

### Thumbnails

After every successful save, `exportToBlob` renders a PNG thumbnail of the current canvas. If the blob is under 100 KB, it gets base64-encoded and stored in `drawing_thumbnails`. The dashboard renders these in a grid. Drawings without a thumbnail get a placeholder with the drawing title.

Thumbnails are kept in a separate table specifically so `SELECT * FROM drawings` on the dashboard doesn't load up to 100 KB of PNG data per row.

---

### Tailwind v4 + `.templ` Files

Tailwind v4's `@source` directive doesn't scan `.templ` files by default. Responsive variants like `sm:hidden`, `md:flex`, `lg:grid-cols-3` silently disappear from the output — no warnings, no errors, just broken layouts.

The fix is `tools/generate_css/main.go`:

1. Scans all `.templ` files under `app/`
2. Extracts classes matching responsive/variant patterns (`sm:`, `md:`, `lg:`, `xl:`, `dark:`, etc.)
3. Writes `app/_entry.css` with `@source inline("...")` declarations containing every extracted class
4. Appends `globals.css` contents

`make css` runs this pipeline → standalone Tailwind binary compiles the result → output embedded in the binary.

---

### CSS Cache Busting

The compiled CSS is embedded in the binary and served at `/globals.css` with:
- `Cache-Control: public, max-age=31536000, immutable`
- `ETag: "<sha256-of-css-content>"` computed at startup

Template links include a `?v=<hash>` query parameter. When the CSS changes, the hash changes, the URL changes, the CDN fetches a new copy. No manual Cloudflare cache purge required (in theory — in practice, purge anyway).

---

### Excalidraw Theming

A brutalist override stylesheet (`excalidraw-brutalist.css`) remaps Excalidraw's internal CSS variables to our design tokens: `border-radius: 0` everywhere, hard offset shadows (`4px 4px 0px`), 2px borders, Catppuccin palette.

The theme toggle syncs with Excalidraw's internal theme state via `api.updateScene({ appState: { theme } })`. A `MutationObserver` on `document.documentElement` watches for class changes (added by `toggleTheme()`) and propagates them to the canvas — so the canvas follows the rest of the UI without a page reload.

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
  canvas/
    page.go                    # Canvas page handler
    page.templ                 # Excalidraw editor: title bar, bridge JS, dirty-bit autosave
    shared.go                  # Shared canvas page handler
    shared.templ               # Read-only Excalidraw view
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
4. **Symlink DB:** `canvas.db` (and WAL files) in `shared/` are symlinked into the new release — database is never touched by a deploy
5. **Restart:** `systemctl restart udin-canvas`
6. **Cleanup:** Old releases archived to `.tar.xz`, keeping the last 5

Binary is ~61 MB (Excalidraw bundle embedded). Deploy takes ~50 seconds.

### Server Setup

| Concern | Solution |
|---|---|
| Reverse proxy | Caddy → app port |
| Process manager | systemd unit with memory limit |
| Database | Shared SQLite file, persists via symlinks |
| CDN | Cloudflare (aggressive caching; purge on CSS change) |
| Peak memory | ~16 MB |

### Environment Variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `PORT` | No | `3000` | HTTP listen port |
| `SQLITE_DB_PATH` | No | `./canvas.db` | Path to SQLite database file |

Firebase service account: auto-detected by scanning for `*-firebase-adminsdk-*.json` in the working directory.

---

## Load Testing

k6 load test against the live server at 500 VUs:

| Metric | Value |
|---|---|
| Error rate | 10.76% (all Cloudflare connection resets; 0% application errors) |
| p95 latency | 739ms (Cloudflare overhead, not server) |
| CRUD pass rate | 100% (create, save, load, rename, share, delete) |

The Go binary handles 500 concurrent users with zero application-level errors. The 10% error rate is Cloudflare dropping connections during ramp-up.

---

## The Suffering Log

Things that went wrong, in roughly chronological order.

1. **Firestore → SQLite** — Moved from a managed service to a single file. Rewrote every query handler. Worth it.

2. **Login/logout routing** — The logout form had `hx-boost="false"` which silently swallowed the `HX-Redirect` header. Removed it. Fixed.

3. **Excalidraw canvas font** — `body { font-family: 'Space Mono' }` in `globals.css` leaked into Excalidraw's canvas text input. Added a reset in `excalidraw-brutalist.css` targeting `.excalidraw .text-editor`. Partially fixed.

4. **Tailwind v4 responsive classes silently disappearing** — `sm:hidden`, `md:flex`, `lg:grid-cols-3` were all missing from CSS output. No warnings. No errors. Just broken layouts. Built a Go tool to scan `.templ` files and inject them via `@source inline()`.

5. **Cloudflare caching stale CSS** — `Cache-Control: no-cache` was being overwritten by Cloudflare. Switched to content-hashed URLs with `immutable`. Purge on deploy anyway.

6. **Auth bar OOB swap** — `UserHandler` returns HTML for both `#auth-bar` (desktop) and `#auth-bar-mobile` (mobile) with `hx-swap-oob="true"`. Required restructuring the handler to return both elements in a single response.

7. **Autosave race on tab close** — The `setTimeout` debounce reset on every `onChange` event. User closes tab within the 2-second window → final `fetch` never fires → stale data on reopen. Replaced with dirty-bit + `setInterval` + `sendBeacon` on `beforeunload`. WAL checkpoint goroutine added to prevent the WAL file from ballooning.

8. **`canvas.db-wal` grew to 4.2 MB** — SQLite's auto-checkpoint threshold is 1000 pages. Without explicit checkpointing, the WAL accumulates between server restarts. Fixed with `PRAGMA wal_checkpoint(PASSIVE)` on a 5-minute ticker.

---

## Why Not Just Use Excalidraw's Built-In Save?

Excalidraw has a "save to disk" button. That works fine for personal use. It doesn't work for "I want to open this diagram on a different machine without emailing myself a JSON file."

SQLite is simpler than any managed database. No indexes to manage, no billing surprises, no document size limits. A single `canvas.db` file. Backed up with `cp`.

---

## License

Made with spite and too many hours debugging CSS that should have worked.
