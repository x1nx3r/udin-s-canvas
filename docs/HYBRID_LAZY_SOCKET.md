# Hybrid "Lazy Socket" Architecture Plan

## The Problem

Currently the draw page connects WebSocket unconditionally on load — even when the user is working solo. Each WS connection costs ~1.6 MB (goroutine + buffers + hub state) and locks the owner into the 150-200 VU ceiling, even though 99% of sessions have zero collaborators.

The pre-collaboration HTTP baseline proved 1,500+ VU is possible at ~94 MB when the server stays **stateless** (no WS goroutines, no hub rooms, no broadcast loops). The goal is to restore that ceiling for the common case while keeping real-time sync available on demand.

---

## The Design: Two Modes, One Trigger

```
                  ┌─────────────────────────┐
                  │   Page Load              │
                  │   WS = disconnected      │
                  │   HTTP save = ON         │
                  └─────────┬───────────────┘
                            │
                    ┌───────┴───────┐
                    │               │
              ┌─────┴─────┐   ┌────┴─────┐
              │  SOLO     │   │ COLLAB   │
              │  MODE     │   │ MODE     │
              │           │   │          │
              │ HTTP save │   │ WS ON    │
              │ every 3s  │   │ HTTP OFF │
              │ No WS     │   │ SCENE_   │
              │ stateless │   │ DELTA    │
              └───────────┘   └──────────┘
```

### Solo Mode (default, 1,500+ VU ceiling)
- `onChange` sets `_dirty = true`
- `setInterval(flushIfDirty, 3000)` saves via `POST /api/draw/{id}/save`
- `beforeunload` fires `sendBeacon` — no data loss beyond one tick
- **No WebSocket connection.** No goroutine, no hub entry, no broadcast buffers.
- Total server cost per user: ~60 KB (one HTTP handler spin-up every 3s, then GC'd instantly)

### Collaboration Mode (upgrade, 150+ VU ceiling)
- WS is connected. HTTP save loop is disabled.
- `onChange` sends `SCENE_DELTA` / `SCENE_UPDATE` over WS.
- Server's hub broadcasts to all other room members.
- The owner's local dirty-bit save is suppressed — the guest's overlapping save is the fallback persistence.

### The Trigger: How the Owner Knows to Upgrade

#### Option A: Polling (recommended — simplest)
```
Owner's page polls:  GET /api/draw/{id}/collab-status
Response:            { "online": 0 | 1 }

Interval:            every 5 seconds
```
- No server-side state per owner (no SSE goroutines)
- Worst-case latency to detect a collaborator: ~5 seconds (acceptable for a shared link open)
- When `online` flips 0→1: `connectWS()` → disable HTTP save → send initial `SCENE_UPDATE`
- When `online` flips 1→0: `disconnectWS()` → re-enable HTTP save

#### Option B: Server-Sent Events (lower latency, more complexity)
```
Owner opens:        GET /api/draw/{id}/events
Server pushes:      event: collab_joined / event: collab_left
```
- Instant notification (no polling delay)
- But: persistent HTTP connection per owner = goroutine + TCP socket (same cost as a WS connection)
- **Defeats the purpose** — the owner is back to paying for a persistent connection even in solo mode

**Decision: Polling.** SSE's instant notification isn't worth the cost of a persistent connection.

---

## Server Changes

### 1. `GET /api/draw/{id}/collab-status`
New public-authenticated endpoint (requires session cookie, returns `{ online: N }`).

```
1. Hub mutex RLock
2. If hub.rooms["draw:{id}"] exists:
     room.mu.Lock()
     online = len(room.clients)
     room.mu.Unlock()
3. Else: online = 0
4. Hub mutex RUnlock
5. Return {"online": online}
```

**Lock safety:** Both locks are acquired as read-only (RWMutex). This prevents concurrent map read/write panics if a room is being created or destroyed at the exact moment the poll arrives. The `hub.mu` guards the `rooms` map; the `room.mu` guards the `clients` map within a room. Both use `RLock()`/`Lock()` respectively — never a write lock, so polls never block WS connect/disconnect.

The endpoint is **O(1)** — just a map lookup. No goroutines spawned, no DB queries.

### 2. Hub Room Lifecycle (no change needed)

The hub already handles everything correctly:
- Room created on first WS connect (owner or guest)
- `lastElements` loaded from DB on room creation
- Room destroyed when last client disconnects
- SCENE_DELTA forwarded; SCENE_UPDATE stored in `lastElements`

### 3. Owner WS Handler (`PUT /api/draw/{id}/ws`)

When the owner finally connects WS (because they detected a collaborator):
- Hub sends `SCENE_INIT` to owner (existing behavior)
- Owner must immediately send `SCENE_UPDATE` so `room.lastElements` is fresh
  (otherwise the guest has stale elements from DB load)

---

## Client Changes

### 1. Draw Page (`page.templ`)

#### Initial state
```js
var _ws = null;
var _collabMode = false;
var _collabPollInterval = null;
```

#### On page load
```js
// Start in solo mode — no WS
// Dirty-bit HTTP save interval already runs (existing code)

// Start polling for collaborators
_collabPollInterval = setInterval(checkCollabStatus, 5000);
```

#### Poll handler
```js
async function checkCollabStatus() {
    if (_collabMode) return; // already in collab mode

    try {
        const res = await fetch(`/api/draw/${_drawingId}/collab-status`);
        const data = await res.json();
        if (data.online > 0) {
            upgradeToCollab();
        }
    } catch (e) {
        // Network error — retry next interval
    }
}
```

#### Upgrade to collaboration mode
```js
function upgradeToCollab() {
    _collabMode = true;
    clearInterval(_collabPollInterval);

    // Disable HTTP saves (stop claiming dirty bit)
    //   The save interval still runs, but flushIfDirty should no-op

    // Connect WS
    connectWS();

    // On WS open: send full SCENE_UPDATE to sync room
    //   (hub's SCENE_INIT may be stale; our local state is authoritative)
}
```

#### Downgrade to solo mode
```js
function downgradeToSolo() {
    _ws.close();
    _ws = null;
    _collabMode = false;

    // Resume polling
    _collabPollInterval = setInterval(checkCollabStatus, 5000);

    // Re-enable HTTP saves
    //   Next flushIfDirty tick will persist latest state
}
```

This fires when:
- WS `onclose` fires with no reconnection (last collaborator left)
- OR a dedicated `COLLAB_ENDED` message arrives from the hub

#### `flushIfDirty` modification
```js
function flushIfDirty() {
    if (_collabMode) return; // WS handles saving via SCENE_UPDATE → hub lastElements
    if (!_dirty) return;
    // ... existing HTTP save logic
}
```

#### `beforeunload` — always fire, regardless of mode
```js
window.addEventListener('beforeunload', () => {
    if (_collabMode) {
        // Flush via WS if connected, or HTTP as fallback
        if (_ws && _ws.readyState === WebSocket.OPEN) {
            _ws.send(JSON.stringify({ type: "SCENE_UPDATE", payload: { elements: _sceneElements } }));
        }
    }
    // Existing sendBeacon always fires
});
```

### 2. Shared Page (`shared.templ`) — no change needed

Guests always connect WS immediately (they're already on the collaboration path). No lazy socket for guests.

---

## State Sync on Upgrade (The Critical Detail)

When the owner upgrades from solo → collab, their local `_sceneElements` may differ from what's in the DB (up to 3 seconds of unsaved changes).

**The sequence:**

```
Time  Owner                              Guest (already on WS)
────  ─────                              ─────────────────────
T0    Opens page, loads scene from DB    Opens shared page, loads scene from DB
      HTTP save every 3s                 Connects WS immediately
      No WS                              Receives SCENE_INIT (elements from DB)
      _sceneElements = fresh             _sceneElements = fresh from DB

T1    Draws a box                        Sees blank canvas (owner not on WS)
      _dirty = true                      Waits for owner to appear
      _sceneElements has the box
      HTTP save hasn't fired yet

T2    Poll detects collaborator
      Connects WS ─────────────────────► Hub sends SCENE_INIT (stale — no box)
                                         Guest sees stale scene? No!

T3    Owner WS onopen
      → sends SCENE_UPDATE
        with current _sceneElements ───► Hub stores in lastElements
                                         Broadcasts to guest
                                         Guest's onmessage fires updateScene
                                         Guest sees the box! ✓
```

**Key requirement:** The owner MUST send `SCENE_UPDATE` immediately on WS open, before any `onChange` fires. This ensures:
1. Hub's `lastElements` is updated to the owner's current state
2. Guest receives the latest scene
3. Future deltas are computed against the correct baseline

Implementation in `connectWS()`:
```js
function connectWS() {
    _ws = new WebSocket(`wss://.../api/draw/${_drawingId}/ws`);

    _ws.onopen = () => {
        // Sync our local state to the room immediately
        _ws.send(JSON.stringify({
            type: "SCENE_UPDATE",
            payload: { elements: _sceneElements }
        }));
    };

    _ws.onmessage = (event) => {
        const msg = JSON.parse(event.data);
        switch (msg.type) {
            case "SCENE_INIT":
                // Ignore — we already have the full scene
                // (it's stale compared to our local state)
                break;
            case "SCENE_UPDATE":
            case "SCENE_DELTA":
                // Apply remote changes
                handleRemoteUpdate(msg);
                break;
            case "MOUSE_LOCATION":
                handleCursor(msg);
                break;
        }
    };

    _ws.onclose = () => {
        // If the room is now empty (no collaborators), downgrade
        // Wait a moment then check
        setTimeout(checkCollabStatusAfterClose, 2000);
    };
}
```

---

## Downgrade (Collab → Solo)

When the last guest disconnects:

### Option A: Hub sends `COLLAB_ENDED` message
- In `Room.remove()`, after deleting the client, if `len(r.clients) == 1` (only owner left), send a special WS message to the owner: `{ type: "COLLAB_ENDED" }`
- Owner's `onmessage` handler calls `downgradeToSolo()`

### Option B: Owner detects via poll fallback
- After WS close, owner re-checks collab-status after 2s
- If `online == 0`, downgrade
- If `online > 0`, reconnect WS

**Recommendation: Both.** Hub sends `COLLAB_ENDED` for instant downgrade. Poll as fallback (e.g., if the owner's WS drops due to network but collaborators are still present, reconnecting handles it).

### Hub changes for `COLLAB_ENDED`:
```go
func (r *Room) remove(client *Client) {
    r.mu.Lock()
    delete(r.clients, client)
    remaining := len(r.clients)
    r.mu.Unlock()

    close(client.Send)

    // If only the owner remains, tell them collab ended
    if remaining == 1 {
        for c := range r.clients {
            select {
            case c.Send <- collabEndedMsg:
            default:
            }
        }
    }
}
```

---

## Edge Cases

### Same user, two tabs of the same drawing

If the owner opens the same drawing in two tabs:
- Both tabs poll independently. Tab A detects Tab B's WS connection as a "collaborator".
- This is **correct behavior**: the user is collaborating with themselves. Both tabs receive updates.
- When one tab closes, the remaining tab sees `online: 0` and downgrades. Correct.

### Rapid join/leave (guest opens link for 2 seconds)

- Owner upgrades to WS on poll (worst case 5s latency).
- Guest leaves before owner even connects WS.
- Owner connects WS to an empty room → `SCENE_UPDATE` updates `lastElements` → owner's `onclose` fires → polls sees `online: 0` → downgrades.
- No data loss. The SCENE_UPDATE updated the hub's memory for future joiners.
- **Optimization:** Add a debounce to prevent upgrade if a guest joins and leaves within the poll window.

### Guest connects before owner upgrades

The guest's WS handler:
1. Room doesn't exist → `getOrCreateRoom` creates it, `loadFromDB()` gets current DB state
2. `sendSceneInit(guest)` sends DB state
3. Guest sees stale scene

This is correct because:
- Guest loaded the same stale data from HTTP `GET /api/shared/{slug}/data`
- When owner upgrades (up to 5s later), their SCENE_UPDATE brings guest up to date
- Guest misses at most 5 seconds of drawing — the owner's 3s HTTP saves would have persisted most of it

### Owner opens shared link in another tab (same browser)

- The shared tab is a guest WS connection (no auth needed for shared pages)
- The owner's draw tab detects the guest via poll and upgrades
- Both tabs now have WS connections to the same room
- This works correctly — the user sees their own changes echoed back

### Deploy during solo mode

- No WS to drop. The HTTP save fires, hits the old server (Caddy buffers), or fails.
- On failure, `_dirty = true` — next tick retries.
- After deploy completes, next save succeeds. Zero disruption. Same as current behavior.

### Deploy during collaboration

- WS drops. Both owner and guest reconnect.
- Owner's reconnect fires SCENE_UPDATE → guest gets current state.
- Both `beforeunload` beacons fire as safety net.
- New process's hub loads state from DB. Same resilience as current architecture.

---

## File Change Summary

| File | Change |
|---|---|
| `app/api/hub.go` | Add `collabEndedMsg` constant; send `COLLAB_ENDED` in `remove()` when 1 client remains |
| `app/api/ws.go` | Add `CollabStatusHandler` for `GET /api/draw/{id}/collab-status` |
| `app/api/draw.go` | Register new route in `main.go` |
| `main.go` | Register `GET /api/draw/{id}/collab-status` with auth middleware |
| `app/canvas/page.templ` | Lazy WS init, poll loop, upgrade/downgrade functions, `_collabMode` guard in `flushIfDirty`, immediate SCENE_UPDATE on WS open |
| `app/canvas/shared.templ` | No changes (guests always connect WS) |

---

## Implementation Order

| Step | What | Why first |
|---|---|---|
| 1 | Add `collab-status` endpoint | Needed by the client; standalone change, testable via curl |
| 2 | Add `COLLAB_ENDED` to hub | Small hub change, doesn't break existing WS flow |
| 3 | Refactor `page.templ` JS | The big change — extract WS connect into its own function, add poll loop, guard flushIfDirty |
| 4 | Test solo mode | Open drawing, draw, close. Verify HTTP saves work without WS. |
| 5 | Test collab upgrade | Open drawing, open shared link in another tab. Verify owner upgrades within 5s, guest receives scene. |
| 6 | Test collab downgrade | Close guest tab. Verify owner downgrades within 2s, HTTP saves resume. |
| 7 | Load test | k6 at 1,500 VUs with solo-mode pages. Verify memory stays at ~94 MB (matching pre-collab baseline). |
