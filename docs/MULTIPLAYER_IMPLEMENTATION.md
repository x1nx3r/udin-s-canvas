# Multiplayer Implementation Roadmap

This document outlines the step-by-step phased execution plan to transform IMPHISE into a real-time collaborative whiteboard using the "Stateless Pipe" architecture.

The collaboration feature is behind a **VIP whitelist** — only users on the whitelist can toggle "Live Collaboration" on a drawing. Access is managed via a hidden super-admin panel.

---

## Phase 1: Database & UX Foundation ✅ DONE
*Goal: Prepare the database and the UI to support opt-in collaboration.*

1. **Schema Migration:**
   - ✅ `ALTER TABLE drawings ADD COLUMN allow_public_edits INTEGER NOT NULL DEFAULT 0;`
   - ✅ Update the `Drawing` struct in `app/dashboard/page.go` to include this field.
   - ⬜ `CREATE TABLE feature_whitelist (email TEXT PRIMARY KEY, created_at DATETIME DEFAULT CURRENT_TIMESTAMP);`

2. **Backend Handlers:**
   - ✅ Add endpoint `PUT /api/draw/{id}/public-edit` that toggles `allow_public_edits`.
   - ✅ Update `api.SharedDataHandler` to return the `allow_public_edits` boolean alongside the scene data.

3. **Frontend UI (Share Dialog):**
   - ✅ Add "Live Collaboration: [Off/On]" toggle button inside the Share modal.
   - ✅ Toggle calls `PUT /api/draw/{id}/public-edit` via `fetch` and syncs state on dialog open.

---

## Phase 2: VIP Feature Flag & Super-Admin Panel
*Goal: Gate the collaboration feature behind a SQLite whitelist, managed by a hidden admin panel.*

1. **Schema Addition (`app/lib/db.go`):**
   - Add idempotent migration for `feature_whitelist` table using `CREATE TABLE IF NOT EXISTS`.

2. **Super-Admin Middleware (`app/lib/middleware.go`):**
   - Create `RequireSuperAdmin(next http.Handler) http.Handler`.
   - Extract the verified user from `r.Context()`.
   - If user is nil or `user.Email != "monmega110@gmail.com"`, return `http.StatusNotFound` (404, not 403 — hide the existence of the route).
   - If email matches, pass through to `next`.

3. **Admin Handlers (`app/admin/`):**
   - `GET /admin/vip` → Serves the admin panel page (fetches and renders current whitelist from SQLite).
   - `POST /admin/vip/add` → `INSERT OR IGNORE INTO feature_whitelist (email) VALUES (?)`. Returns updated HTML list item fragment for HTMX `beforeend` swap.
   - `DELETE /admin/vip/remove` → `DELETE FROM feature_whitelist WHERE email = ?`. Returns `200 OK`; HTMX removes the element via `outerHTML` swap.

4. **Admin UI (`app/admin/admin.templ`):**
   - Minimal, brutalist shell — no ceremony, no extra CSS.
   - Add-user form: `<form hx-post="/admin/vip/add" hx-target="#vip-list" hx-swap="beforeend">` with `<input type="email" name="email">`.
   - Whitelist: `<ul id="vip-list">` iterating over current emails.
   - Each entry: `<button hx-delete="/admin/vip/remove?email={email}" hx-target="closest li" hx-swap="outerHTML">Revoke</button>`.

5. **Route Registration (`main.go`):**
   - Register all `/admin/*` routes wrapped in `RequireSuperAdmin`.

6. **API Gatekeeper (`app/api/draw.go`):**
   - In `PublicEditHandler`, before executing the `UPDATE`, query `feature_whitelist` using the current user's email.
   - If not on the whitelist → `http.StatusForbidden` (403).
   - If on whitelist → proceed with the `UPDATE`.

7. **Canvas Page VIP Check (`app/canvas/page.go` & `page.templ`):**
   - In `canvas.PageHandler`, query `feature_whitelist` for the current user's email.
   - Pass `IsVIP bool` down to the `CanvasPage` templ component.
   - In the Share Modal:
     - `IsVIP == true` → render the functional toggle: `"Live Collaboration: [Off/On]"`.
     - `IsVIP == false` → render a disabled hint: `"Live Collaboration: (Locked — Invite Only)"`.

---

## Phase 3: The Go WebSocket Hub (The Stateless Pipe)
*Goal: Build the bare-metal, zero-state routing engine in Go.*

1. **Hub Data Structures (`app/api/hub.go`):**
   - Create a `Room` struct containing a `map[*websocket.Conn]bool` and a `sync.Mutex`.
   - Create a global `Hub` struct containing a `map[string]*Room` (keyed by drawing ID / slug) and a `sync.RWMutex`.

2. **The WebSocket Upgrader (`app/api/ws.go`):**
   - Implement `GET /api/draw/{id}/ws` for authenticated owners.
   - Implement `GET /api/shared/{slug}/ws` for anonymous guests.
   - Both endpoints upgrade the HTTP connection via `gorilla/websocket`.

3. **The Broadcast Loop:**
   - Write a `readLoop` goroutine for each connection.
   - When a byte slice (JSON payload) is received from a client, the server acquires the room's Mutex, iterates over the connections map, and executes `conn.WriteMessage()` to all clients *except* the sender.
   - Ensure `defer conn.Close()` and proper removal from the `Hub` when a connection drops.

4. **Connection Security & Lifecycles (The OOM & Leak Guards):**
   - **The Malicious Payload OOM Fix:** Immediately enforce `conn.SetReadLimit(1024 * 512)` (512 KB). This prevents a malicious user from sending a 50MB string over the socket and instantly triggering the systemd OOM killer.
   - **The Laptop-Lid Memory Leak Fix:** Implement a Ping/Pong heartbeat loop. Set `conn.SetReadDeadline(time.Now().Add(60 * time.Second))`. The server must ping clients every 30 seconds; if a client drops offline silently (e.g. laptop lid closed), the read deadline trips, the loop safely exits, and the dead connection is purged from the Hub's RAM.

---

## Phase 4: The Passive Client (Live Presentation)
*Goal: Enable read-only clients to passively receive updates in real-time.*

1. **WebSocket Initialization (`shared.templ` & `canvas.templ`):**
   - Inject the WebSocket connection script directly into the `.templ` files.
   - Example: `const ws = new WebSocket('wss://' + window.location.host + '/api/shared/{{.Slug}}/ws');`

2. **The EMP Tripwire (Thundering Herd Protection):**
   - Implement the `onclose` and `onerror` handlers with randomized exponential backoff.
   - `setTimeout(() => connect(), 1000 + Math.random() * 2000)` to ensure simultaneous reconnects (e.g., during an atomic deploy) are safely staggered.

3. **Applying Remote Patches:**
   - Implement `ws.onmessage` to parse the incoming JSON payload.
   - Call `api.updateScene({ elements: payload.elements })`.

---

## Phase 5: The Active Client (Live Collaboration)
*Goal: Safely broadcast local changes without causing infinite feedback loops or breaking the dirty-bit autosave.*

1. **Mounting the Editor:**
   - In `shared.templ`, if `allow_public_edits` is true, initialize Excalidraw with `viewModeEnabled: false`.

2. **The Infinite Loop Guard (Element Versioning):**
   - Track the highest `version` or `versionNonce` of elements received from the network.
   - In the Excalidraw `onChange` hook, deeply compare the local elements against the known network state. If the versions haven't incremented, abort the broadcast.

3. **Broadcasting & Dirty-Bit Overlap:**
   - If the `onChange` hook determines the change is genuinely local, execute `ws.send(JSON.stringify({ type: "update", elements }))`.
   - Set `_dirty = true` to arm the local 3-second autosave interval.
   - **Crucial Verification:** Ensure that users receiving `onmessage` updates do *not* have their `_dirty` flags set, preventing them from hammering the SQLite database with redundant saves.

4. **Multiplayer Presence & Identity (Live Cursors):**
   - **The UX (Name Prompt):** When a guest visits the shared link, intercept them with a minimal, brutalist modal asking for their name before mounting the canvas. Store this in `sessionStorage`.
   - **Client Identity:** Generate a random UUID for the current browser session.
   - **Broadcasting:** In `onPointerUpdate`, capture `pointer: { x, y }` and broadcast a `{ type: "pointer", clientId, username, pointer }` payload via the WebSocket.
   - **Rendering:** On the receiving end, use the incoming `clientId` as the key to inject the data into Excalidraw's native state: `api.updateScene({ collaborators: new Map([[payload.clientId, { pointer: payload.pointer, username: payload.username }]]) })`. This renders their cursor with their chosen name gracefully floating next to it.

---

## Phase 6: Hardening & Load Testing
*Goal: Prove the architecture holds under pressure.*

1. **Thundering Herd Simulation:**
   - Connect 100 simulated clients to a single room via `k6`.
   - Bounce the Go server (`systemctl restart udin-canvas`).
   - Monitor Caddy and the Go runtime to ensure the randomized reconnect backoff prevents CPU spikes.

2. **Write Contention Verification:**
   - Simulate 10 users rapidly drawing in the same room.
   - Confirm that the overlapping 3-second dirty-bit autosaves queue perfectly in the SQLite `MaxOpenConns(1)` lock without producing `database is locked` errors.
   - Verify that data integrity is maintained (the last autosave wins).
