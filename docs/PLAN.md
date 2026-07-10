# Excalidraw Wrapper — Plan

## Pages

| Route | Page | Auth | Description |
|-------|------|------|-------------|
| `/` | Landing | No | Hero + "Sign in with Google" CTA |
| `/drawings` | Dashboard | Yes | Grid of user's drawings, "New Drawing" button |
| `/draw/{id}` | Canvas | Yes | Excalidraw editor with auto-save |
| `/shared/{slug}` | Shared | No | Read-only Excalidraw view |
| `/profile` | Profile | Yes | Display name, photo, account management |
| `/docs/*` | Docs | No | Already exists |

## Components

### Shared (in `app/components/`)
- **Navigation** — existing, updated: auth bar, "Drawings" link, "Profile" link when signed in
- **Footer** — existing, unchanged
- **DrawingCard** — thumbnail, title, last modified, share status, delete button
- **ShareDialog** — inline share link with copy button, revoke toggle
- **AutoSaveBadge** — "Saving..." / "Saved" / "Error" indicator (HTMX-polled)
- **UserMenu** — avatar + dropdown with Profile link + Sign out
- **EmptyState** — "No drawings yet" illustration + CTA

### Auth (in `app/auth/`)
- **AuthBar** — rendered by `GET /auth/user`: shows "Sign in" button or user info

### Dashboard (in `app/dashboard/`)
- **DashboardPage** — grid layout with DrawingCards
- **NewDrawingButton** — creates drawing, redirects to canvas

### Canvas (in `app/canvas/`)
- **CanvasPage** — Templ shell + Excalidraw mount div + inline bridge JS
- **Toolbar** — minimal HTMX toolbar (save, share, back to dashboard)

### Shared (in `app/shared/`)
- **SharedPage** — read-only Excalidraw (no editing tools)

### Profile (in `app/profile/`)
- **ProfilePage** — display name, email, photo, sign-out button
- **ProfileForm** — edit display name (saves to Firestore user doc)

## Theming

Keep the existing brutalist design language from the boilerplate:

### CSS Variable System (already in `globals.css`)

```css
:root {
  --bg: #fff;
  --fg: #1a1a24;
  --fg-muted: #6b6b80;
  --bg-subtle: #f5f5f7;
  --border: #1a1a24;
  --accent: #6c5ce7;
  --red: #e74c3c;
  --green: #00b894;
}
.dark {
  --bg: #0d0d14;
  --fg: #e8e8ed;
  --fg-muted: #8b8b9e;
  --bg-subtle: #14141f;
  --border: #2a2a3a;
  --accent: #7c6cf0;
  --red: #e74c3c;
  --green: #00dba0;
}
```

### Design rules (inherited)
- 2px borders everywhere, no border-radius
- Solid shadows with `translate` on hover/active (brutalist)
- `font-black` uppercase tracking-wider headings
- Mono font for technical labels (`Fira Code`)
- Inter for body, Cinzel for display (already imported)

### Dark mode
- Defaults to system preference via `prefers-color-scheme`
- Toggle in nav saves to localStorage
- View transitions on toggle via `document.startViewTransition()`

## Directory Structure

```
main.go
app/
  auth/
    firebase.go         — Admin SDK init
    middleware.go        — session cookie → context
    handlers.go          — login/logout/user handlers
  dashboard/
    page.go             — GET /drawings handler
    page.templ          — DashboardPage component
  canvas/
    page.go             — GET /draw/{id} handler
    page.templ          — CanvasPage component
    data.go             — save/load JSON handlers
    share.go            — share link CRUD
  shared/
    page.go             — GET /shared/{slug} handler
    page.templ          — SharedPage component
  profile/
    page.go             — GET /profile handler
    page.templ          — ProfilePage component
  components/
    navigation.templ     — updated with auth-aware links
    footer.templ         — unchanged
    drawing_card.templ   — DrawingCard component
    share_dialog.templ   — ShareDialog component
    auto_save.templ      — AutoSaveBadge component
    empty_state.templ    — EmptyState component
  assets/
    bridge.js            — Excalidraw ↔ Go adapter
```

## Data Flow (per page)

```
Landing (/)          → no fetch, static hero + sign-in button
Dashboard (/drawings) → hx-get="/api/drawings" → server queries Firestore → renders DrawingCards
Canvas (/draw/{id})  → page load: fetch /api/draw/{id}/data → Excalidraw.importScene()
                       onChange → debounce → fetch POST /api/draw/{id}/save
                       HTMX: share button → POST /api/draw/{id}/share → renders ShareDialog
Profile (/profile)   → hx-get="/api/profile" → renders form, POST to update
Shared (/shared/{x}) → page load: fetch /api/shared/{x}/data → Excalidraw.importScene() (read-only)
```

## Route Map (final)

| Method | Path | Handler | Auth | Resp |
|--------|------|---------|------|------|
| GET | `/` | dashboard.PageHandler (landing) | No | Templ |
| GET | `/drawings` | dashboard.PageHandler (dashboard) | Yes | Templ |
| GET | `/draw/{id}` | canvas.PageHandler | Yes | Templ |
| POST | `/draw/new` | dashboard.NewHandler | Yes | Redirect |
| POST | `/api/draw/{id}/save` | canvas.SaveHandler | Yes | JSON |
| GET | `/api/draw/{id}/data` | canvas.DataHandler | Yes | JSON |
| POST | `/api/draw/{id}/share` | canvas.ShareHandler | Yes | Templ |
| DELETE | `/api/draw/{id}/share` | canvas.ShareHandler | Yes | Templ |
| GET | `/shared/{slug}` | shared.PageHandler | No | Templ |
| GET | `/api/shared/{slug}/data` | shared.DataHandler | No | JSON |
| GET | `/profile` | profile.PageHandler | Yes | Templ |
| POST | `/api/profile` | profile.UpdateHandler | Yes | Templ/redirect |
| POST | `/auth/login` | auth.LoginHandler | No | HX-Redirect |
| POST | `/auth/logout` | auth.LogoutHandler | No | HX-Redirect |
| GET | `/auth/user` | auth.UserHandler | No | HTML frag |
| GET | `/docs` / `/docs/{slug}` | docs.PageHandler | No | Templ |

## Implementation Order

### Phase 1 ✅ — Auth Foundation (done)
### Phase 2 — Dashboard + Drawing CRUD
### Phase 3 — Canvas Editor + bridge.js
### Phase 4 — Sharing + Read-only
### Phase 5 — Profile Page
### Phase 6 — Polish (delete, auto-save indicator, rate limiting)
