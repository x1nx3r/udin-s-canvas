# TECH-LOG.md — The Real-Time Collaboration Suffering Log

> *"It compiles? Ship it."* — Every line of this file.

The first volume of suffering lives in the README under "The Suffering Log." This is volume II. It covers the week we decided Excalidraw needed WebSockets and refused to use a managed service.

**Spoiler:** It works now. But we had to destroy a few things to get there.

---

## Table of Suffering

| Act | Title | Pain Level |
|-----|-------|------------|
| 0 | The Starting Line | Low — things were working |
| I | The onChange Trap | Medium — firehose of events |
| II | Protocol Archaeology | High — reading minified JS |
| III | The Canonical Heresy | Very High — hub v1 was wrong |
| IV | The Pure Forwarder | Medium — hub v2 was right |
| V | The Client Rewrite | High — IIFE grew legs |
| VI | The Bundle That Wouldn't Bundle | CRITICAL — `make dev` strikes |
| VII | The Revelation | Medium — npm had it all along |
| VIII | The Autosave Decoupling | Medium — WAL was ballooning |
| IX | The Font Red Herring | Low — 404s that didn't matter |
| X | The Debug Print Holocaust | Low — but cathartic |

---

## Act 0: The Starting Line

Before real-time collaboration, the app worked like this:

- Open `/draw/{id}` → Excalidraw mounts → fetches scene from `GET /api/draw/{id}/data` → `api.updateScene()`
- Edit something → `onChange` fires → `_scene = snapshot`, `_dirty = true`
- Every 3 seconds → `flushIfDirty()` → `POST /api/draw/{id}/save`
- Close tab → `sendBeacon` with the last snapshot
- Share → `POST /api/draw/{id}/share` → returns a slug → `/shared/{slug}` renders Excalidraw in `viewModeEnabled: true`

No WebSockets. No real-time. No cursors. Just a drawing app with SQLite persistence. It was peaceful. We should have stayed there.

The route structure was already in place for WebSocket upgrades:

```go
// main.go
mux.Handle("GET /api/draw/{id}/ws", lib.RequireAuth(api.OwnerWSHandler))
mux.HandleFunc("GET /api/shared/{slug}/ws", api.GuestWSHandler)
```

They were stubs. Empty handlers. Placeholders for "future work." The future arrived.

---

## Act I: The onChange Trap

### The Autosave That Cried Wolf

The existing `onChange` callback looked like this:

```javascript
onChange: function(elements, appState) {
    _sceneElements = elements;
    _scene = { elements: elements, appState: appState };
    _dirty = true;
}
```

Every. Single. Interaction. Every pixel of every drag. Every character of every text edit. Every selection change. `onChange` does not discriminate. It fires if you look at the canvas wrong.

The autosave handled this fine because `_dirty = true` is O(1) and the 3-second interval batches the noise. But WebSocket broadcasting is not free. Sending 8 MB of elements 60 times per second is a performance crime.

### Version-Gated Broadcasting

We introduced `_lastSceneVersion`:

```javascript
var _lastSceneVersion = -1;

onChange: function(elements, appState) {
    _sceneElements = elements;
    var version = getSceneVersion(elements);
    if (version > _lastSceneVersion) {
        _lastSceneVersion = version;
        _ws.send(JSON.stringify({ type: 'SCENE_UPDATE', payload: { elements: elements } }));
    }
}
```

`getSceneVersion()` computes a hash over all element versions. If no element versions changed, the hash stays the same. No broadcast. This filtered out selection changes, viewport pans, and accidental mouse clicks that don't modify elements.

It filtered out nothing during active drawing. Every stroke changes element versions. Every text edit bumps the version. Every move, resize, rotate — all of them change versions. The version gate only filters out the noise between actual edits. When someone is actively drawing, every single `onChange` still triggers a broadcast.

**Lesson learned:** Excalidraw's `onChange` fires on every render. There is no delta. There is no debounce built-in. If you want to rate-limit broadcasts, you do it yourself. We didn't add rate limiting. The hub just drops clients whose buffers overflow. It's not elegant. It's a pressure valve.

### The Protocol Hunger Games

We now had two problems:
1. We're broadcasting elements, but the receiving client needs to *reconcile* them with its local state
2. We need a handshake for late-joining clients (SCENE_INIT)

We went looking for the upstream Excalidraw collaboration protocol. We found it in the source code. We should have stayed in the source code.

---

## Act II: Protocol Archaeology

### Cloning the Known Universe

```bash
git clone git@github.com:excalidraw/excalidraw
cd excalidraw
```

This is a monorepo. The collaboration protocol lives in `packages/excalidraw/`. It's TypeScript. It's well-written. It's not documented because it's internal.

The protocol uses message types:

| Type | Direction | Purpose |
|------|-----------|---------|
| `SCENE_INIT` | Server → Client | Full element array sent on connection |
| `SCENE_UPDATE` | Client ↔ Client | Element changes broadcast to peers |
| `MOUSE_LOCATION` | Client ↔ Client | Cursor position + username for multiplayer cursors |

The upstream implementation uses a `SocketedDataChannel` interface abstracted over Firebase Realtime DB. We don't use Firebase. We use Go + gorilla/websocket. The abstraction layer is "write our own."

### The Functions We Needed

We found these utility functions in the source:

| Function | Source | Purpose |
|----------|--------|---------|
| `getSceneVersion()` | `element/index.ts` | Compute a hash over element versions |
| `restoreElements()` | `data/restore.ts` | Repair element data after deserialization |
| `reconcileElements()` | `data/reconcile.ts` | Merge remote elements into local state, preserving local edits |
| `CaptureUpdateAction` | `store.ts` | Enum to control undo history capture |
| `bumpVersion()` | `element/mutateElement.ts` | Increment a single element's version |

`reconcileElements()` is the star of the show. It takes `(localElements, remoteElements, appState)` and returns a merged array. The merge logic:
- New elements from remote (not in local) → added
- Elements in both → if remote version > local version, use remote; if local version > remote version (local has unsaved edits), keep local
- Deleted elements → removed if remote confirms deletion
- Elements currently being edited locally → kept local (this is where `appState` is needed)

This function is why the server can't merge. It needs `appState` to know which elements are being edited. Only the client has that.

### The Custom Bundle Rodeo

The bundle we use (`excalidraw.bundle.js`) is an IIFE that assigns to `window.ExcalidrawBundle`. The upstream default IIFE only exports the `Excalidraw` React component and `exportToBlob`. Our `entry.js` imported from the npm package and re-exported a subset.

But we didn't start with the npm package. We started by pointing esbuild at the cloned repo's TypeScript source:

```bash
npx esbuild packages/excalidraw/index.ts \
  --bundle --minify --format=iife --global-name=ExcalidrawBundle \
  --define:process.env.NODE_ENV=\"production\" \
  --outfile=/tmp/excalidraw-custom/excalidraw.bundle.js
```

This produced a bundle with the utility functions mangled to short names:

```
getSceneVersion → aae
reconcileElements → PS
restoreElements → DB
CaptureUpdateAction → aQ
```

We verified this with `rg`:

```bash
$ rg "getSceneVersion:aae" excalidraw.bundle.js
# found it
$ rg "reconcileElements:PS" excalidraw.bundle.js
# found it
```

We had a custom bundle that exported everything we needed. We copied it to `assets/public/`. We loaded the page. `getSceneVersion` was `undefined`.

We checked the file on disk. It had the exports. We checked the server response. It had the exports. We cleared the browser cache. Still broken. We hard-refreshed. Still broken. We incinerated the service worker. Still broken.

Then we ran `make dev` and realized.

---

## Act III: The Canonical Heresy

### Hub v1: The Server Is the Source of Truth

The first hub implementation was a server-authoritative canonical scene model. The server stored the one true element array. Every incoming SCENE_UPDATE was merged into canonical. The merged result was broadcast to all peers.

```go
type Room struct {
    mu                sync.Mutex
    clients           map[*Client]bool
    canonicalElements []json.RawMessage
}
```

The `handleMessage` flow:

1. Parse SCENE_UPDATE → extract elements
2. Lock the mutex
3. Merge incoming elements into `canonicalElements`:
   - For each incoming element, find matching element in canonical by ID
   - If remote version > canonical version, replace
   - If not in canonical, append
   - Elements in canonical but not in incoming → keep canonical (they weren't deleted)
4. Store merged result as the new canonical
5. Broadcast to all peers
6. Save to database
7. Unlock

This is broken for reasons that became apparent over the next 2 hours of debugging.

### Why the Canonical Hub Cannot Work

**Reason 1: The server doesn't have `appState`.**

`reconcileElements()` takes three arguments: `(localElements, remoteElements, appState)`. The third argument is critical — it tells the function which element is currently being edited so it doesn't get overwritten by a remote update. The server doesn't have `appState`. The server doesn't even know what React is. The server is a Go binary that thinks JSX is a file extension.

Without `appState`, the server's merge is equivalent to last-writer-wins at the element level. If client A is editing a text element and client B moves a box, the server's merge might or might not work depending on timing. If both clients edit the same element simultaneously, one edit silently vanishes.

**Reason 2: The race condition.**

Client A sends SCENE_UPDATE (elements at version 5).
Client B sends SCENE_UPDATE (elements at version 6).
They arrive at the server in the same `ReadMessage` tick.

The server processes A's message first → merges into canonical → version 5 is now canonical.
The server processes B's message next → merges into canonical → version 6 elements replace version 5 elements.

Result: A's changes are in canonical, B's changes are in canonical. But B's merge was based on a stale snapshot of A's elements. If both clients modified the same element, the result is undefined. The server doesn't know which one to prefer.

This race exists in the upstream Excalidraw Collab too, but it's mitigated by Firebase Realtime DB's per-field atomicity and the fact that the upstream Collab runs the reconciliation on the client, not the server.

**Reason 3: The database write storm.**

Every SCENE_UPDATE triggered a `REPLACE INTO drawings(content)`. This meant:
- `SELECT` the current row
- `UPDATE` the content column
- SQLite WAL grows with every write
- The checkpoint goroutine can't keep up
- 4.2 MB WAL file in 10 minutes of testing

We were writing the full 8 MB element array to SQLite on every mouse move. This is not sustainable.

### The Debate: Who Owns the Truth?

We spent an embarrassing amount of time debating this question. The options:

| Model | Server Role | Client Role | Merge Location |
|-------|-----------|-------------|----------------|
| Canonical (v1) | Owns truth | Sends full scenes | Server |
| Client-authoritative (v2) | Pipe | Owns truth | Client |
| Hybrid (v2.5) | Caches last scene | Owns truth | Client, server caches for SCENE_INIT |

The hybrid model is what we actually shipped: the server caches `lastElements` (the most recent SCENE_UPDATE payload it saw), sends it as SCENE_INIT on new connections, and otherwise just broadcasts. Clients own their own state and run reconciliation locally.

This is the simplest model. It's also the only one that works without server-side React.

---

## Act IV: The Pure Forwarder

### Hub v2: The Server Is a Pipe

```go
func (r *Room) handleMessage(sender *Client, msg []byte) {
    var incoming struct { Type string `json:"type"` }
    if err := json.Unmarshal(msg, &incoming); err != nil {
        return // bad message, drop it
    }
    switch incoming.Type {
    case "SCENE_UPDATE":
        r.cacheElements(msg)
        r.broadcastAllBut(sender, msg)
    case "MOUSE_LOCATION":
        r.broadcastAllBut(sender, msg)
    default:
        r.broadcastAllBut(sender, msg)
    }
}
```

`cacheElements` extracts the element array from the SCENE_UPDATE and stores it in `lastElements`. That's it. No merge. No reconciliation. No database write. Just cache the latest elements for SCENE_INIT and forward the message.

`broadcastAllBut` iterates the client map and writes to each client's send channel. If a client's buffer is full (256 messages deep), the client gets disconnected — buffer full means the client is too slow to keep up, and keeping them around would stale the room.

The complete hub in production:
- ~200 lines including room management, client tracking, scene init, broadcast, stats
- No database writes
- No merge logic
- No state machine
- 2 mutex locks per message (one for cache, one for broadcast)

### The SCENE_INIT Handshake

When a new client connects, the server immediately sends SCENE_INIT:

```go
func (r *Room) sendSceneInit(client *Client) {
    r.mu.Lock()
    elements := r.lastElements
    r.mu.Unlock()
    msg, _ := json.Marshal(map[string]any{
        "type": "SCENE_INIT",
        "payload": map[string]any{ "elements": elements },
    })
    select {
    case client.Send <- msg:
    default:
        // Buffer full before we even started — disconnect
        close(client.Send)
        delete(r.clients, client)
    }
}
```

This is server-pushed. The client doesn't send a "gimme the scene" message. The server sends it immediately after the WebSocket upgrade succeeds. The client receives it in the same `onmessage` handler as regular SCENE_UPDATE messages. Same code path. Same reconciliation pipeline.

The select-with-default pattern prevents a slow client from blocking the server. If the send channel buffer is full (256 messages queued before the client has read a single one), the server disconnects the client immediately. This is harsh but correct — if the client can't keep up with SCENE_INIT (one message), it certainly can't handle the firehose of updates.

### The Client-Side Reconciliation Pipeline

```javascript
function handleRemoteScene(remoteElements) {
    var existing = _sceneElements.length > 0
        ? _sceneElements
        : window.excalidrawAPI.getSceneElements();

    var Bundle = window.ExcalidrawBundle;
    var restored = Bundle.restoreElements(remoteElements, existing);
    var reconciled = Bundle.reconcileElements(existing, restored, appState);
    reconciled = Bundle.bumpElementVersions(reconciled, existing);
    _lastSceneVersion = Bundle.getSceneVersion(reconciled);
    window.excalidrawAPI.updateScene({
        elements: reconciled,
        captureUpdate: Bundle.CaptureUpdateAction.NEVER
    });
}
```

The pipeline:
1. **`restoreElements`** — Fixes any deserialization issues. Excalidraw elements have required fields (`type`, `id`, `version`, `isDeleted`, etc.) that must survive JSON round-trips. If a field is missing, `restore` fills in defaults.
2. **`reconcileElements`** — Merges remote elements into local state. New elements from remote? Added. Local elements not in remote? Kept. Both versions of the same element? Higher version wins, unless the local copy is being edited (checked against `appState`).
3. **`bumpElementVersions`** — If the merged result has elements with the same version as existing local elements, bump them so the local client registers them as changes.
4. **`CaptureUpdateAction.NEVER`** — Don't add this scene change to the undo stack. The undo stack is for local edits only. Remote updates should not be undoable (the user didn't make them).

This runs for both SCENE_INIT and SCENE_UPDATE. Same function. Same behavior. The client doesn't care if the elements came from a new connection or a remote edit.

---

## Act V: The Client Rewrite

### The IIFE That Ate Manila

The page.templ inline script started at ~50 lines. It grew to ~150. The shared.templ script grew to ~140. Together they contain:

- **WebSocket lifecycle management** — connect, reconnect (exponential backoff via `setTimeout`), error handling, close handling
- **Scene reconciliation** — The full handleRemoteScene pipeline
- **Local edit detection** — Version-gated broadcasting via `getSceneVersion`
- **Collaborator cursor tracking** — `MOUSE_LOCATION` messages decoded into a `Map<clientId, {pointer, username}>`, fed to `api.updateScene({ collaborators })`
- **Client identity** — `_clientId` (sessionStorage, 36-radix timestamp + random), `_username` (sessionStorage, set via name prompt)
- **Name prompt** — Shared page shows a modal asking for username before enabling edit mode. If `_username` already exists in sessionStorage, the modal is skipped.
- **View-only vs. edit mode** — Conditional on `allowPublicEdits`. If false, `viewModeEnabled: true`, no `onChange` handler registered.
- **Autosave** — The original dirty-bit + setInterval + sendBeacon pattern from the README, coexisting peacefully with the WebSocket code.

The two scripts are nearly identical but not shared. They can't be shared because they're embedded in `.templ` files as inline `<script>` blocks. Templ doesn't support importing one template's JS into another. The only way to share code is to extract it into a separate static JS file. We didn't do that. We copy-pasted and diverged. We are bad people.

### The Shared Page Name Prompt

When `allowPublicEdits` is true, the shared page shows a modal:

```html
<div id="name-prompt">
    <div class="name-card">
        <h2>Join Session</h2>
        <p>Enter your name so others know who's drawing.</p>
        <input id="username-input" type="text" placeholder="Your name" maxlength="32"/>
        <button onclick="joinSession()">Join →</button>
    </div>
</div>
```

The Enter key handler:

```javascript
nameInput.addEventListener('keydown', function(e) {
    if (e.key === 'Enter') { window.joinSession(); }
});
```

`joinSession()` stores the name in `sessionStorage`, hides the modal, and the `onChange` handler starts broadcasting edits. Before joining, the guest is in view-only mode regardless of `allowPublicEdits`.

This is the most over-engineered name prompt in existence. It's ~40 lines of JS + ~40 lines of CSS for what amounts to `prompt("Enter your name")`. But it looks better.

### The Cursor Tracking

```javascript
_ws.onmessage = function(event) {
    var payload = JSON.parse(event.data);
    if (payload.type === 'MOUSE_LOCATION') {
        _collaborators.set(payload.payload.clientId, {
            pointer: payload.payload.pointer,
            username: payload.payload.username || 'Guest'
        });
        window.excalidrawAPI.updateScene({ collaborators: new Map(_collaborators) });
    }
};
```

And on the sending side:

```javascript
onPointerUpdate: function(payload) {
    _ws.send(JSON.stringify({
        type: 'MOUSE_LOCATION',
        payload: {
            clientId: _clientId,
            username: _username,
            pointer: payload.pointer
        }
    }));
}
```

The owner's `clientId` is `"owner-" + drawingId`. The guest's `clientId` is generated once per session and stored in `sessionStorage`. The `onPointerUpdate` fires on every mouse move. We rate-limit nothing. The hub's buffer is the rate limiter. If the buffer overflows, the client gets disconnected. Harsh but effective.

---

## Act VI: The Bundle That Wouldn't Bundle

### The `make dev` Tragedy (Extended Director's Cut)

This section exists because the previous version was too short. The `make dev` overwrite deserves a full treatment because it was the single most time-wasting bug of the entire project.

**The timeline of a single debug cycle:**

1. Clone excalidraw/excalidraw repo
2. Build custom bundle from source: `npx esbuild packages/excalidraw/index.ts ...`
3. Verify exports: `rg "getSceneVersion" excalidraw.bundle.js` → found
4. Copy to project: `sudo cp /tmp/bundle.js app/assets/public/excalidraw.bundle.js`
5. Kill server: `pkill -f "go run"`
6. Restart server: `go run .`
7. Open browser: hard refresh (Cmd+Shift+R)
8. Check console: `getSceneVersion is not a function`
9. Check file on disk: `rg "getSceneVersion" app/assets/public/excalidraw.bundle.js` → found
10. Check server response: `curl http://localhost:3000/static/excalidraw.bundle.js | rg "getSceneVersion"` → found
11. Clear browser cache: Settings → Privacy → Clear cache → "All time"
12. Hard refresh: still broken
13. Open incognito window: still broken
14. Try different browser: still broken
15. Question reality
16. Check if Go embed is caching the old file: read embed docs → `//go:embed` is compile-time, can't update at runtime → must rebuild binary
17. Rebuild binary: `go build -o /tmp/server && /tmp/server`
18. Hard refresh: still broken
19. Check if there's a build cache: `go clean -cache`
20. Rebuild: still broken
21. Notice `make dev` in another terminal tab
22. Check Makefile: `dev: $(TAILWIND_BIN) bundle`
23. Read `bundle` target: `esbuild app/assets/excalidraw/entry.js ...`
24. Read `entry.js`: only exports `Excalidraw`, `exportToBlob`, `React`, `ReactDOM`
25. Close terminal tab. Open new one. Run `make bundle` manually. Restart server.
26. Hard refresh: `getSceneVersion is not a function` — because `make bundle` ran the OLD entry.js

**The actual fix:**

```diff
- import { Excalidraw, exportToBlob } from '@excalidraw/excalidraw';
- export { React, ReactDOM, Excalidraw, exportToBlob };
+ import {
+   Excalidraw, exportToBlob,
+   getSceneVersion, restoreElements,
+   reconcileElements, CaptureUpdateAction,
+ } from '@excalidraw/excalidraw';
+ export { React, ReactDOM, Excalidraw, exportToBlob,
+   getSceneVersion, restoreElements,
+   reconcileElements, CaptureUpdateAction,
+   bumpElementVersions };
```

The fix was a 7-line edit to a file we didn't know was relevant for 5 hours. The `entry.js` file was right there. We'd looked at it. We didn't realize it was the source of the production bundle.

**The `rg` output problem:**

The bundle is minified to a single 8 MB line. Running `rg "getSceneVersion"` on an 8 MB single-line file returns... the entire 8 MB line as the matching context. The terminal froze for 3 seconds and then vomited 8 MB of unreadable JS. We had to use `rg -c` (count mode) or `rg -o` (only match) to get useful output. This delayed debugging because we assumed `rg` found nothing when it was just outputting too much data.

We lost at least 30 minutes to this across the project.

---

## Act VII: The Revelation

### `@excalidraw/excalidraw` Had Everything

The npm package `@excalidraw/excalidraw` at version 0.18.1 exports:

```typescript
// From dist/types/excalidraw/index.d.ts
export { getSceneVersion } from "./element";
export { restoreElements } from "./data/restore";
export { reconcileElements } from "./data/reconcile";
export { CaptureUpdateAction } from "./store";
export { bumpVersion } from "./element/mutateElement";
```

We found this by accident. We were looking at the npm package's `dist/dev/index.js` to confirm the functions existed in the development build (they did), and we happened to open the `.d.ts` file in the same directory. The type declarations listed every export. The production build (`dist/prod/index.js`) includes all of them.

The only function not exported is `bumpElementVersions`. It exists in the source as an internal helper in `data/reconcile.ts` but isn't part of the public API. We had to write it:

```javascript
function bumpElementVersions(elements, existing) {
    var existingMap = new Map();
    for (var i = 0; i < existing.length; i++) {
        existingMap.set(existing[i].id, existing[i]);
    }
    return elements.map(function(el) {
        var existingEl = existingMap.get(el.id);
        if (existingEl && existingEl.version > el.version) {
            return Object.assign({}, el, { version: existingEl.version + 1 });
        }
        return el;
    });
}
```

11 lines. No dependencies. Does what it says: if an existing element has a higher version than the incoming one, bump the incoming one past it. This prevents version conflicts when two clients independently modify the same element.

---

## Act VIII: The Autosave Decoupling

### The Original Sin

The original hub v1 had autosave built in:

```go
// hub.go (v1, lost to history but remembered in therapy)
r.mu.Lock()
r.canonicalElements = merged
r.mu.Unlock()
go r.saveToDB(merged)  // fire-and-forget database write
r.broadcastAllBut(sender, msg)
```

Every SCENE_UPDATE triggered a database write. The client was also autosaving via the dirty-bit pattern. The result: each edit was written to SQLite twice — once by the hub and once by the client. The WAL file grew unbounded. The checkpoint goroutine couldn't keep up.

### The Fix: One Writer

The client's dirty-bit autosave is the canonical persistence mechanism. It's been battle-tested since the README's "Suffering Log" was written. The hub doesn't need to save to the database at all.

```go
// hub.go — the save function was deleted. It no longer exists.
```

The server only reads from the database in `DataHandler` (the initial scene load). The server writes to the database in `SaveHandler` (HTTP POST from the autosave). The WebSocket hub is excluded from persistence entirely.

The decoupling works because:
- The client autosaves independently of WebSocket state
- The hub only needs `lastElements` for SCENE_INIT, which it caches from the latest SCENE_UPDATE
- The database is never accessed by the WebSocket goroutines
- No WAL contention between multiple writers

### The `loadFromDB` Race

When a room is created, the hub loads the last saved scene from the database:

```go
func (r *Room) loadFromDB() {
    // SELECT content FROM drawings WHERE id = ?
    // Parse JSON
    // Set r.lastElements = parsed.Elements
}
```

This runs once, synchronously, when the room is created. If the database returns stale data (because the client autosave hasn't flushed yet), `lastElements` is empty and the SCENE_INIT sends an empty canvas. The client then loads the actual data from the HTTP `DataHandler` response (which happens in parallel via `excalidrawAPI` → `fetch /api/draw/{id}/data`), so the client eventually shows the correct scene.

But there's a window where the newly connected peer briefly sees an empty canvas before the HTTP response arrives. This window is about 50ms. Nobody has reported this as a bug. We're not fixing it because the fix would require the hub to wait for the HTTP response, which couples the WebSocket and HTTP paths and introduces more complexity than 50ms of visual emptiness justifies.

---

## Act IX: The Font Red Herring

### The 404s That Didn't Matter

When we loaded the page and opened the browser console, we saw:

```
GET /static/fonts/Assistant/Assistant-Regular.woff2 404 (Not Found)
GET /static/fonts/Assistant/Assistant-Medium.woff2 404 (Not Found)
GET /static/fonts/Assistant/Assistant-SemiBold.woff2 404 (Not Found)
GET /static/fonts/Assistant/Assistant-Bold.woff2 404 (Not Found)
```

The CSS Font Loading API reported these as `status=2147746065`, which is the error code for `FontFaceLoadStatus.ERROR` in the CSS Font Loading specification. It's not a Windows error. We looked it up three times because it looked like a Windows error.

The fonts are referenced by Excalidraw's internal CSS (`excalidraw.css`). The CSS is served from `/static/excalidraw.css`. The font paths in the CSS are relative to the CSS file's location — `/static/fonts/Assistant/Assistant-Regular.woff2` — but the actual font files in our `public/` directory are flat with hashed names like `Xiaolai-Regular-<hash>.woff2`.

The solution is to check what fonts Excalidraw needs and place them at the expected paths. We manually downloaded the Assistant fonts and placed them in `public/`. This didn't fix the `getSceneVersion` error. We left them there. Later we deleted them because they were noise.

The font 404s still exist in the deployed version. The app uses fallback fonts. Nobody has complained. The CSS Font Loading API continues to silently fail in the background, logging errors to a console nobody reads in production.

---

## Act X: The Debug Print Holocaust

### Server-Side Log Massacre

The original `ws.go` had 12 `log.Printf` calls:

```
[ws]  OWNER  upgrade  room=%s remote=%s
[ws]  GUEST  upgrade  room=%s slug=%s remote=%s
[ws]  upgrade FAIL  room=%s remote=%s err=%v
[ws]  upgrade OK    room=%s remote=%s elapsed=%s
[ws]  pong   room=%s remote=%s
[ws]  DISCONN room=%s remote=%s session=%s
[ws]  read  ERR  room=%s remote=%s err=%v
[ws]  read  CLOSE room=%s remote=%s reason=%v
[ws]  read  MSG  room=%s remote=%s bytes=%d readWait=%s
[ws]  write FAIL room=%s remote=%s err=%v
[ws]  ping   room=%s remote=%s
[ws]  ping  FAIL room=%s remote=%s err=%v
```

The original `hub.go` had 11 `log.Printf` calls:

```
[hub] room OPEN  key=%s elements=%d
[hub] loadFromDB parse error for %s: %v
[hub] conn  JOIN  key=%s peers=%d remote=%s
[hub] conn  LEFT  key=%s peers=%d remote=%s
[hub] sendSceneInit DROP key=%s remote=%s buffer full
[hub] bad msg from %s: %v
[hub] SCENE_UPDATE parse error: %v
[hub] bcast DROP key=%s remote=%s buffer full
[hub] bcast DONE key=%s msg#=%d bytes=%d sent=%d elapsed=%s
[hub] room CLOSE key=%s msgs=%d age=%s
```

Every WebSocket event was logged. Every message byte counted. Every broadcast timed. The server produced more log output in 30 seconds of collaboration than the rest of the app generates in a week.

The cleanup removed:
- All diagnostic `[ws]` and `[hub]` tracing (upgrade OK, MSG, ping, pong, OPEN, JOIN, LEFT, DONE, CLOSE)
- All timing instrumentation (elapsed, readWait, session duration)
- The `connectedAt` variable in ws.go (only used for the disconnect log)
- The `n` variables in hub.go (only used for peer count logs)
- The `start` variable in hub.go `broadcastAllBut` (only used for bcast DONE timing)

Kept:
- Error logs: parse errors, upgrade failures, write failures, ping failures, buffer full drops
- Unexpected close errors (abnormal closure detection)

The client side lost 4 `console.log` calls:
- `[ws]  RECV %s  elements=%d` in both page.templ and shared.templ
- `[ws]  SEND SCENE_UPDATE  v=%d els=%d` in both page.templ and shared.templ

`console.error` calls for actual errors were kept.

---

## The Final File Count

| File | Lines Added | Lines Removed | Net |
|------|------------|---------------|-----|
| `app/api/hub.go` | 211 | 0 | +211 (new file) |
| `app/api/ws.go` | 179 | ~60 | ~+119 (rewritten) |
| `app/canvas/page.templ` | ~350 | ~30 | ~+320 (rewritten) |
| `app/canvas/shared.templ` | ~243 | ~20 | ~+223 (new file, replaced old) |
| `app/assets/excalidraw/entry.js` | 32 | 5 | +27 |
| `app/assets/public/excalidraw.bundle.js` | 0 | 0 | 8 MB, regenerated |
| `app/canvas/page_templ.go` | ~10 | ~10 | 0 (regenerated) |
| `app/canvas/shared_templ.go` | ~10 | ~10 | 0 (regenerated) |
| `ANALYSIS.md` | ~100 | 0 | 0 (deleted) |
| `excalidraw/` | ~50k (cloned) | 0 | 0 (deleted) |
| `TECH-LOG.md` | ~600 | 0 | +600 (this file) |

Files created and deleted during the journey:

| Path | Lifespan | Cause of Death |
|------|----------|----------------|
| `/tmp/excalidraw-custom/excalidraw.bundle.js` | ~12 hours | Replaced by npm package build |
| `excalidraw/` (upstream repo clone) | ~24 hours | rm -rf |
| `ANALYSIS.md` | ~2 hours | rm (internal notes escaping containment) |
| `app/assets/public/Assistant-*.woff2` | ~1 hour | rm (red herring) |
| `app/assets/public/CascadiaCode-Regular-*.woff2` | ~1 hour | rm (wrong font set) |

---

## The Technical Debt (Updated)

- **The font 404s.** Assistant, Cascadia Code, Nunito, and others are requested by the Excalidraw CSS but not present at the expected paths. The app falls back silently. The console collects errors.
- **The IIFE scripts.** Two copies of the same JS logic diverging independently. Not shared. Not testable. Not maintainable.
- **No WebSocket rate limiting.** The hub's only backpressure mechanism is the send channel buffer filling up and disconnecting the slow client. There's no per-client message coalescing, no tick-based batching, no adaptive throttling.
- **`bumpElementVersions` is hand-written.** It works, but if the upstream package ever exports it, we should switch. We won't know because we don't read changelogs.
- **The SCENE_INIT race.** Newly connecting peers see an empty canvas for ~50ms before the HTTP scene data loads. Fixing this requires coupling the WS and HTTP paths, which we're not doing.

---

## The Timeline (Comprehensive)

| When | What | How We Felt |
|------|------|-------------|
| Evening, Day 1 | Decided to add real-time collaboration | Excited |
| Night, Day 1 | Implemented onChange-based broadcasting | Productive |
| Late night, Day 1 | Discovered we need element reconciliation | Confused |
| Very late, Day 1 | Cloned excalidraw repo, found the protocol | Hopeful |
| Early morning, Day 2 | Built custom bundle with all exports | Victorious |
| Morning, Day 2 | Tested in browser: `getSceneVersion is not a function` | Frustrated |
| Late morning, Day 2 | Blamed browser cache, hard reset | Confident (wrong) |
| Afternoon, Day 2 | Blamed Go embed.FS cache | Sure (wrong) |
| Late afternoon, Day 2 | Rebuilt binary, cleared go cache | Determined (wrong) |
| Evening, Day 2 | Noticed font 404s, chased that rabbit | Distracted |
| Night, Day 2 | Manually added Assistant fonts | Productive (wrong target) |
| Late night, Day 2 | Fonts still 404, getSceneVersion still undefined | Desperate |
| Middle of night, Day 2 | Realized `make dev` runs `make bundle` which overwrites our custom bundle | Eureka |
| Next morning, Day 3 | Fixed entry.js to re-export from npm package | Relieved |
| Morning, Day 3 | Everything suddenly works | Elated |
| Afternoon, Day 3 | Implemented hub v1 (canonical merge) | Overconfident |
| Evening, Day 3 | Realized canonical merge can't work without appState | Defeated |
| Night, Day 3 | Debated architecture: client-authoritative vs. hybrid | Philosophical |
| Late night, Day 3 | Implemented hub v2 (pure forwarder) | Decisive |
| Very late, Day 3 | Rewrote page.templ bridge JS | Efficient |
| Early morning, Day 4 | Implemented shared.templ guest page | Productive |
| Morning, Day 4 | Added name prompt, cursor tracking, reconnection | Feature-creeping |
| Afternoon, Day 4 | Tested collaboration between two browsers | Nervous |
| Late afternoon, Day 4 | Two browsers, same canvas, real-time | Euphoric |
| Evening, Day 4 | Discovered WAL file at 4.2 MB | Concerned |
| Night, Day 4 | Decoupled autosave from hub | Judicious |
| Late night, Day 4 | Cleaned up debug prints | Cathartic |
| Next day, Day 5 | Wrote this log | Meta-suffering |

---

## Epilogue

The collaboration feature works. Two browsers on the same canvas. Real-time element sync. Cursor positions. Name tags. Autosave. Reconnection. It's deployed at [canvas.x1nx3r.dev](https://canvas.x1nx3r.dev).

The hub is ~200 lines of Go. The client bridge is ~150 lines of inline JS. The bundle is the same 8 MB IIFE it always was, just with a few more exports. The font 404s are still there. Nobody has noticed.

The moral of the story: read the Makefile before you blame the embed cache.

---

*"The best code is the code you don't write. The second best is the code you write after deleting the first version."* — Still someone on Hacker News, probably.
