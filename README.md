<p align="center">
  <img src="app/assets/public/logo.svg" width="96" height="96" alt="Udin's Canvas" style="border: 2px solid var(--border); padding: 8px;" />
</p>

<h1 align="center">Udin's Canvas</h1>

<p align="center">
  <strong>Live: <a href="https://canvas.x1nx3r.dev" target="_blank">canvas.x1nx3r.dev</a></strong>
</p>

An Excalidraw wrapper. Because I wanted to draw diagrams without signing up
for a SaaS product that emails me every Tuesday about "premium canvas
textures." You know the ones. "Unlock the felt-tip pen for $9/mo." I just
want to draw a box with an arrow in it. I don't need a subscription for that.

This is **Udin's Canvas** — a Go + Templ + HTMX + Tailwind app that wraps
Excalidraw, saves drawings to SQLite, and lets you share them with a link.
No latency. No login wall (well, a Google login wall, but you already have
a Google account). No "upgrade to pro to export as PNG."

It compiles to a single binary (~59MB, mostly the 8MB Excalidraw bundle).
The database is a single `canvas.db` file. I deployed it on a $5 VPS and it
uses 16MB of RAM. That's less than most people's browser tabs.

## Stack

| Layer | Choice | Reason |
|---|---|---|
| Language | Go | Compiles to a binary. No runtime. No `node_modules`. |
| Templates | Templ | Type-safe HTML that doesn't make you want to cry. |
| CSS | Tailwind v4 (standalone) | Utility classes without 400MB of PostCSS plugins. |
| Interactivity | HTMX | Server-rendered HTML fragments. No virtual DOM. |
| Auth | Firebase Auth Web SDK | Google sign-in. ID tokens. Server-verified session cookies. |
| Database | SQLite (`mattn/go-sqlite3`) | Single file. WAL mode. Zero config. CGO required. |
| Canvas | Excalidraw 0.18.1 (bundled) | 8MB of someone else's hard work. I just render it. |

## Getting Started

```bash
# Prerequisites: Go 1.22+, Node (for the excalidraw bundle), CGO (for SQLite)
make setup     # installs templ, tailwind, downloads deps
make dev       # live-reloading dev server on :3000
```

Or just:

```bash
go run .
```

Open [http://localhost:3000](http://localhost:3000). You'll see a landing
page. Click "Start Drawing." Sign in with Google. Draw a box. It saves
automatically because I didn't want you to lose your masterpiece.

**Note:** Requires `CGO_ENABLED=1` because of `mattn/go-sqlite3`. This is
the only dependency that needs CGO. On macOS it just works. On Linux you
might need `gcc` installed (it's usually there already). On the server,
`deploy.sh` sets `CGO_ENABLED=1 GOOS=linux GOARCH=amd64` for cross-compilation.

## How It Works

### Auth Flow

1. User clicks "Sign in with Google."
2. Firebase Auth Web SDK opens a popup, user authenticates.
3. Client sends the ID token to `POST /auth/login`.
4. Server verifies the token with the Firebase Admin SDK and creates an
   http-only session cookie (`14 * 24 * time.Hour` expiry, `SameSite=Strict`).
5. Middleware verifies the cookie on every request and injects the user's
   `uid` into the request context.

That's it. No JWT parsing on the client. No `localStorage` tokens. No
"refresh token rotation" blog post you'll read at 2AM and immediately forget.

### Canvas Save/Load

Excalidraw fires an `onChange` callback on every user action. The client
debounces this with a 2-second timer. When the timer fires, it sends the
entire scene (elements + appState) to `POST /api/draw/{id}/save`. The
server writes it to SQLite as a JSON blob in the `content` column.

Loading is the reverse: `GET /api/draw/{id}/data` reads from SQLite,
sanitizes the `appState.collaborators` field (because Excalidraw crashes
if that's not an array — ask me how I know), and returns the scene.

### SQLite

The database is a single `canvas.db` file with WAL mode, busy timeout of
5 seconds, and foreign keys enabled. The schema:

```sql
CREATE TABLE drawings (
    id TEXT PRIMARY KEY,
    owner_id TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT 'Untitled',
    content TEXT,
    share_slug TEXT UNIQUE,
    thumbnail TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_drawings_owner ON drawings(owner_id);
CREATE INDEX idx_drawings_share_slug ON drawings(share_slug);
```

`content` stores the full Excalidraw scene as JSON. `thumbnail` stores a
base64-encoded PNG (under 100KB). `share_slug` is nullable — only set when
the drawing is shared. The app uses `MaxOpenConns(1)` because SQLite doesn't
handle concurrent writers well, and WAL mode + busy timeout handles the rest.

On the server, the database lives in `/var/www/udin-canvas/shared/canvas.db`
and is symlinked into each release directory. This means deploys never
touch the database — it persists across atomic symlink swaps.

### Excalidraw Bundle

Excalidraw is bundled at build time via esbuild:

```bash
npx esbuild app/assets/excalidraw/entry.js \
  --bundle \
  --outfile=app/assets/public/excalidraw.bundle.js \
  --minify \
  --format=iife \
  --global-name=ExcalidrawBundle
```

The entry point exports `Excalidraw`, `React`, `ReactDOM`, and
`exportToBlob` as globals. The output is ~8MB. It lives in the binary via
`//go:embed`. The CSS is a separate file (also embedded) that gets linked
in the page head.

Versions are pinned to exact (`@excalidraw/excalidraw: 0.18.1`,
`react: 18.3.1`, `react-dom: 18.3.1`) because Excalidraw's CSS classes
change between versions, and our brutalist override stylesheet depends on
specific selectors.

### Sharing

Clicking "Share" hits `POST /api/draw/{id}/share`, which generates a
16-byte random hex slug, stores it on the drawing, and returns it. The
share dialog shows a read-only URL (`/shared/{slug}`). Anyone with that
URL can view the drawing — no auth required.

The shared page uses Excalidraw in `viewModeEnabled: true`. It loads the
scene from `GET /api/shared/{slug}/data`, which does a public query
(no auth middleware) by matching the `share_slug` column.

### Thumbnails

After every save, the client calls `exportToBlob` (exported from the
Excalidraw bundle) to generate a PNG thumbnail. If the blob is under
100KB, it gets base64-encoded and stored in the `thumbnail` column. The
dashboard shows these thumbnails in a grid. If there's no thumbnail, you
get a placeholder with the drawing title.

The 100KB limit is arbitrary. I picked it because it felt right. If your
drawing produces a thumbnail larger than 100KB, congratulations, you've
made something detailed enough that a 100KB thumbnail doesn't do it justice.

### Excalidraw Theming

Excalidraw's default CSS has rounded corners, soft shadows, and a color
scheme that doesn't match anything. I wrote an override stylesheet
(`excalidraw-brutalist.css`) that maps Excalidraw's internal CSS variables
to our design tokens, sets `border-radius: 0` everywhere, replaces soft
shadows with hard offset shadows (`4px 4px 0px`), and applies 2px borders
to everything that stands still long enough.

The theme toggle syncs with Excalidraw's internal state. When you flip
from Latte to Mocha, the canvas follows without needing a page reload.
This took longer than I'd like to admit.

## Project Structure

```
main.go                          # Routes, middleware, static serving
app/
  lib/                           # Shared infrastructure
    auth.go                      # Firebase Admin SDK init (auto-detects service account)
    middleware.go                # Session cookie verification, RequireAuth, GetUserUID
    auth_handlers.go             # Login/logout/user endpoints (unified nav button styling)
    db.go                        # SQLite init, WAL mode, schema migration
  api/                           # API handlers (no HTML rendering)
    draw.go                      # Data, Save, Share, Rename, Thumbnail, Delete handlers
    shared.go                    # Public shared data handler (no auth)
  canvas/                        # Canvas pages (Excalidraw editor)
    page.go + .templ             # Canvas editor with merged title bar
    shared.go + .templ           # Read-only shared view
  dashboard/
    page.go + .templ             # Drawing list + new drawing (content-only, uses Layout)
  profile/
    page.go + .templ             # User profile with drawing grid (content-only, uses Layout)
  components/
    navigation.templ             # Nav bar with logo, links, theme toggle (autoHide param)
    drawing_card.templ           # Card with thumbnail, rename, delete
    logo.templ                   # SVG logo component
    footer.templ                 # Footer
    empty_state.templ            # Empty state for dashboard
  layout.templ                   # Root HTML shell (HTML, head, nav, footer, Firebase SDK)
  canvas_layout.templ            # Minimal HTML shell for Excalidraw pages
  page.templ                     # Landing page (hero, features, CTA)
  page.go                        # Landing handler (redirects authenticated to /drawings)
  globals.css                    # Design tokens, Tailwind config, base styles
  assets/
    excalidraw/                  # Excalidraw source (entry.js, package.json)
    public/                      # Static files (logo, excalidraw bundle/css)
    assets.go                    # go:embed directives
```

### Layout Unification

All pages use one of two layout templates:

- **`Layout`** — Full HTML shell with nav, footer, Firebase SDK, HTMX. Used
  by landing, dashboard, and profile pages. Page templates are content-only
  components wrapped in `@Layout(...)`.

- **`CanvasLayout`** — Minimal HTML shell with just the head (Excalidraw CSS,
  theme init). Used by canvas and shared pages which need full-screen layouts
  without nav/footer.

This means 2 layout templates cover 5 pages, instead of 5 standalone HTML
shells with duplicated `<head>` blocks.

### Auth Bar

The nav auth bar (`#auth-bar`) is loaded via HTMX on every page. The server
returns a consistent `<div class="flex items-center gap-2">` with buttons
that all share the same brutalist class set:

```
px-3 py-1.5 border-2 border-[var(--border)] text-xs font-bold uppercase
tracking-wider cursor-pointer hover:bg-[var(--bg-subtle)] transition-all
active:translate-x-0.5 active:translate-y-0.5 active:shadow-none
shadow-[2px_2px_0px_0px_var(--border)]
```

This is defined once in `auth_handlers.go` as `navBtnClass` and used
across all auth states (Sign In, Logout, logged-in profile bar).

## Deployment

The app deploys as a single binary to a bare-metal VPS via atomic symlink swap.

### The Flow

```bash
bash deploy.sh
```

1. **Local build:** `make css && make templ && CGO_ENABLED=1 go build` → single binary
2. **rsync:** Binary + Firebase JSON + Makefile.server → timestamped release dir
3. **Atomic swap:** `ln -nfs` points `current` → new release
4. **Symlink DB:** SQLite files in `shared/` are symlinked into the new release
5. **Restart:** `systemd restart udin-canvas`
6. **Clean up:** Old releases are compressed to `.tar.xz` archives (keep 5)

Zero downtime. The binary is 59MB, the DB is a single file, and the whole
deploy takes about 30 seconds including the compression of old releases.

### Server Setup

- **Reverse proxy:** Caddy → app port
- **Service:** systemd unit with memory limit
- **Database:** shared SQLite file (persists across deploys via symlinks)
- **Peak memory:** ~16MB

### Environment Variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `PORT` | No | `3000` | HTTP listen port |
| `SQLITE_DB_PATH` | No | `./canvas.db` | Path to SQLite database file |

The Firebase service account JSON is auto-detected by scanning for
`-firebase-adminsdk-*.json` in the working directory. If you have exactly
one such file, it gets picked up. If you have zero or more than one, the
app yells at you.

## Load Testing

Ran k6 load tests against the live server. Results at 500 VUs:

| Metric | Value |
|---|---|
| Error rate | 10.76% (all Cloudflare connection resets, 0% application errors) |
| p95 latency | 739ms (Cloudflare overhead, not server) |
| CRUD checks | 100% pass (create, save, load, rename, share, delete) |
| Share bug | Fixed: was returning 100% errors due to NULL `share_slug` scan |

The 10% error rate is entirely Cloudflare dropping connections during the
ramp-up, not the server rejecting requests. The Go binary handles 500
concurrent users with zero application-level errors.

## Why Not Just Use Excalidraw's Built-In Save?

Excalidraw has a "save to disk" button and a "load from disk" button. That
works great for personal use. It doesn't work great for "I want to draw a
diagram on my work computer and open it on my laptop without emailing
myself a JSON file."

SQLite is simpler than Firestore. No indexes to manage, no billing surprises,
no "document size limit." A single `canvas.db` file with WAL mode handles
everything I need. And it's backed up with a `cp` command.

## The Excalidraw CSS Situation

Excalidraw ships a minified CSS file that's about 145KB on one line. It
gets embedded in the Go binary and served as a static file. There's also
a brutalist override CSS that fixes the aesthetic clash between our sharp
gothic theme and Excalidraw's rounded-corner default.

If Excalidraw updates their CSS classes between versions, the overrides
will break. I've pinned the version to `0.18.1` to delay this as long as
possible. When it happens, I'll spend an hour updating selectors and
muttering about CSS specificity. That's a future problem. Future me is
very understanding.

## Design System

"Goth-Brutalist" — sharp corners, hard offset shadows, dark palette.

- **Fonts:** Cinzel (headings), Inter (body), Fira Code (mono)
- **Borders:** 2px solid, always
- **Shadows:** Hard offset (`4px 4px 0px`), no blur
- **Colors:** Dark backgrounds, crimson/lavender accents, high contrast
- **Buttons:** Translate on active (`translate-x-0.5 translate-y-0.5`), shadow disappears
- **Radius:** Zero. Everywhere. No exceptions.
