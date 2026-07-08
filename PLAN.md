# Excalidraw Wrapper — Implementation Plan

## Overview

Transform the GOTTH boilerplate into an Excalidraw drawing app with Firestore persistence,
Firebase Auth, and shareable links. The architecture is **hybrid**: the canvas is a JS island
(Excalidraw runs client-side), everything surrounding it (dashboard, nav, auth, sharing) uses
HTMX with server-rendered Templ fragments.

---

## Directory Structure

```
main.go
app/
  auth/
    middleware.go          — Firebase session cookie verification (wraps http.Handler)
    handlers.go            — POST /auth/login, POST /auth/logout
    firebase.go            — Admin SDK init (Firestore + Auth clients)
  dashboard/
    page.go                — GET / — list user's drawings
    page.templ             — dashboard page
    drawing_card.templ     — reusable card in the list
  canvas/
    page.go                — GET /draw/{id} — serve editor page (hybrid)
    page.templ             — Templ layout + Excalidraw mount div + inline JS bridge
    data.go                — GET /draw/{id}/data (JSON), POST /draw/{id}/save
    share.go               — POST /draw/{id}/share, DELETE /draw/{id}/share
  shared/
    page.go                — GET /shared/{slug} — read-only view
    page.templ
  components/
    navigation.templ       — updated: user avatar, login/logout
    share_dialog.templ     — share link display/copy
  assets/
    bridge.js              — Excalidraw ↔ Go adapter (embedded via //go:embed)
```

---

## Routes

| Method | Path                  | Handler             | Auth | Response Type    |
|--------|-----------------------|---------------------|------|------------------|
| GET    | `/`                   | dashboard.PageHandler | Yes | HTML (Templ)    |
| GET    | `/draw/{id}`          | canvas.PageHandler  | Yes  | HTML (Templ)    |
| POST   | `/draw/new`           | dashboard.NewHandler | Yes | Redirect         |
| POST   | `/draw/{id}/save`     | canvas.SaveHandler  | Yes  | JSON / HTMX frag |
| GET    | `/draw/{id}/data`     | canvas.DataHandler  | Yes  | JSON             |
| POST   | `/draw/{id}/share`    | canvas.ShareHandler | Yes  | HTML (Templ)     |
| DELETE | `/draw/{id}/share`    | canvas.ShareHandler | Yes  | HTML (Templ)     |
| GET    | `/shared/{slug}`      | shared.PageHandler  | No   | HTML (Templ)     |
| POST   | `/auth/login`         | auth.LoginHandler   | No   | HTMX redirect    |
| POST   | `/auth/logout`        | auth.LogoutHandler  | No   | HTMX redirect    |

---

## Firestore Schema

### `drawings/{drawingId}`

```json
{
  "ownerId":    "string",
  "title":      "string",
  "createdAt":  "Timestamp",
  "updatedAt":  "Timestamp",
  "sceneData":  "string (JSON: { elements: [], appState: {} })",
  "shareSlug":  "string | null"
}
```

### `shared/{shareSlug}`

```json
{
  "drawingId":  "string",
  "createdAt":  "Timestamp"
}
```

Note: `sceneData` is stored as a single JSON string to stay well under Firestore's 1MB
document limit. Excalidraw scenes with thousands of elements can be large; storing as a
string avoids index overhead on fields we never query.

---

## Auth Flow (Firebase Auth)

1. **Client** initializes Firebase Auth SDK and shows a "Sign in with Google" button.
2. On click, Firebase popup returns an ID token.
3. Client sends the ID token to `POST /auth/login` via HTMX.
4. **Server**: `auth.LoginHandler` verifies the token via Firebase Admin SDK, creates a
   session cookie (http-only, same-site=strict), returns an HTMX redirect to `/`.
5. **Server**: `auth.Middleware` reads the cookie on every subsequent request, verifies it
   with Admin SDK, injects `uid` into `r.Context()`.
6. **Logout**: `POST /auth/logout` clears the session cookie.

---

## JavaScript Surface Area

Three JS sources on the canvas page:

| Source | Purpose |
|--------|---------|
| Firebase Auth SDK | sign-in popup, ID token management |
| Excalidraw (npm/CDN) | the canvas editor |
| `bridge.js` (embedded) | glues Excalidraw events to Go endpoints |

The bridge.js responsibilities:
- Initialize Excalidraw instance in the mount div
- On page load: `fetch(GET /draw/{id}/data)` → `excalidrawAPI.importScene(data)`
- On `excalidrawAPI.onChange`: debounce, serialize scene, `fetch(POST /draw/{id}/save, {body: JSON.stringify(sceneData)})`
- Listen for HTMX-triggered custom events from share dialog

---

## Implementation Phases

### Phase 1 — Foundation
- [ ] `app/auth/firebase.go` — load credentials, init Firestore + Auth clients
- [ ] `app/auth/middleware.go` — cookie read/verify, context injection
- [ ] `app/auth/handlers.go` — login/logout endpoints
- [ ] Update `main.go` — register middleware + auth routes
- [ ] Update `app/components/navigation.templ` — show user avatar / login button

### Phase 2 — Drawing CRUD
- [ ] `app/dashboard/page.go` + `page.templ` — list drawings from Firestore
- [ ] `app/dashboard/new.go` — create drawing, redirect to `/draw/{id}`
- [ ] `app/canvas/data.go` — save/load JSON handlers
- [ ] `app/canvas/page.go` + `page.templ` — serve Excalidraw with embedded bridge

### Phase 3 — Sharing
- [ ] `app/canvas/share.go` — generate/revoke share slugs
- [ ] `app/components/share_dialog.templ` — share link UI
- [ ] `app/shared/page.go` — read-only view (no toolbar, prevent edits)
- [ ] `app/assets/bridge.js` — wire Excalidraw events to save/load

### Phase 4 — Polish
- [ ] Auto-save indicator (HTMX-polled or SSE)
- [ ] Delete drawing
- [ ] Last-opened sorting
- [ ] Rate-limit share link generation
