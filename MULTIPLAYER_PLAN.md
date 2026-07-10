# Multiplayer Architecture Plan (The Stateless Pipe)

## Objective
Implement real-time collaboration for Excalidraw canvases while strictly adhering to the brutalist constraints of the $5 VPS, specifically avoiding Go heap bloat and complex CRDT merging on the backend.

---

## 1. The Core Philosophy: "The Stateless Pipe"
Instead of building a stateful server-authoritative hub that holds massive JSON blobs in RAM and attempts to merge CRDT patches, the Go backend will act exclusively as a **dumb, zero-knowledge router**. 

The server will simply shuffle raw byte slices between WebSocket connections. 

Database persistence will continue to rely entirely on the clients' overlapping 3-second dirty-bit autosaves, leveraging our mathematically proven SQLite `MaxOpenConns(1)` write durability.

---

## 2. Backend Implementation (Go)

### The Hub
We will introduce a minimal WebSocket hub using the standard `x/net/websocket` or `gorilla/websocket`.
```go
type Room struct {
    sync.Mutex
    Clients map[*websocket.Conn]bool
}

var Hub = struct {
    sync.RWMutex
    Rooms map[string]*Room
}{
    Rooms: make(map[string]*Room),
}
```

### The `/ws/{id}` Endpoint
1. Authenticate the user via the existing session cookie.
2. Upgrade the HTTP request to a WebSocket.
3. Add the connection to `Hub.Rooms[id]`.
4. Spin up a single `read` goroutine for the connection.
5. **The Broadcast Loop:** When a message (byte slice) arrives, blindly range over `Hub.Rooms[id].Clients` and write the byte slice to everyone except the sender.
6. **Cleanup:** On connection close, remove the client from the room map. If the room is empty, delete the room from the Hub map.

**Total Server Memory Overhead:** The size of a `*websocket.Conn` struct per active user. Zero JSON storage.

---

## 3. Frontend Implementation (`canvas.templ`)

We will inject the WebSocket logic directly into the existing `canvas.templ` inline script to avoid esbuild bloat and allow native Go template variables (like `{{.ID}}`) to set the room address.

### The Network Bridge
```javascript
const ws = new WebSocket(`wss://${window.location.host}/api/draw/{{.ID}}/ws`);

ws.onmessage = (event) => {
    const patch = JSON.parse(event.data);
    
    // IMPORTANT: We flag this as a remote update to prevent loopbacks
    _isRemoteUpdate = true;
    
    // Update the Excalidraw scene
    api.updateScene({ elements: patch.elements }); 
};
```

### Modifying the Dirty-Bit Hook
We hook into Excalidraw's `onChange` event to broadcast our local changes, while carefully ensuring we don't broadcast changes we just received from the network.

```javascript
onChange: (elements, appState) => {
    if (_isRemoteUpdate) {
        // This change was triggered by the WebSocket. 
        // Reset the flag and abort. Do NOT broadcast. Do NOT set dirty.
        _isRemoteUpdate = false;
        return;
    }

    _scene = { elements, appState };
    _dirty = true; // Flag for the 3-second SQLite autosave
    
    // Broadcast the change to the dumb pipe
    if (ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ elements }));
    }
}
```

---

## 4. The Data Flow in Action

Imagine 5 users in a room: **User A** is drawing a box. Users B, C, D, and E are watching.

1. **User A** moves their mouse.
2. User A's browser triggers `onChange`. `_dirty` becomes `true`.
3. User A's browser sends the JSON patch over the WebSocket.
4. The Go server receives the bytes and instantly echoes them to B, C, D, and E.
5. Users B, C, D, and E receive the `onmessage` event. `_isRemoteUpdate` is set to `true`.
6. Their Excalidraw instances re-render the new box.
7. Their `onChange` handlers fire, but since `_isRemoteUpdate` is true, they immediately abort. **Their `_dirty` flags remain `false`.**

### The 3-Second Flush
When the 3-second `setInterval` fires across all 5 browsers:
- Users B, C, D, and E skip the save because their `_dirty` flag is false.
- **Only User A** executes the `POST /api/draw/:id/save` request to the Go backend.

**The Result:** The Go server successfully facilitated a 5-player real-time collaboration session, but it only had to process a single database write every 3 seconds, and it held precisely 0 bytes of canvas JSON in its own memory.

Even if all 5 users draw simultaneously (triggering 5 redundant writes every 3 seconds), our load test proved that SQLite can swallow 450 writes a second without dropping a single packet.

---

## 5. Deployment Durability

Because the Go server is completely stateless regarding the canvas data, atomic deployments remain completely safe. 

If `deploy.sh` bounces the systemd service while 5 users are drawing:
1. The WebSocket connections drop.
2. All 5 browsers execute their `beforeunload` or native WebSocket reconnect logic.
3. The latest canvas state is naturally preserved via the `sendBeacon` POST request (which fires asynchronously on connection loss/page unload).
4. When the new Go binary boots 1 second later, the clients reconnect, read the latest state from SQLite, and resume drawing. Zero data loss.

---

## 6. The UX Strategy (How Users Interact With It)

The current system has a `POST /api/draw/{id}/share` endpoint that generates a random `share_slug` and serves a static, read-only view at `/shared/{slug}`. We can evolve this into a massive feature with very little friction.

### The "Live Presentation" Mode (Default)
By default, the `/shared/{slug}` link remains **read-only** (`viewModeEnabled: true` in Excalidraw).
- However, the shared page *does* connect to the WebSocket as a passive listener.
- **The UX:** When the owner is drawing in their private `/draw/{id}` page, their changes are broadcasted over the WebSocket. Anyone watching the public `/shared/{slug}` link sees the canvas updating in real-time. It becomes an instant, frictionless live presentation tool. 

### The "Live Collaboration" Mode (Opt-In)
We add a simple toggle to the owner's Share menu: **"Allow anyone with the link to edit"**.
- This flips a boolean `allow_public_edits` on the `drawings` SQLite row.
- When `allow_public_edits` is true, the `/shared/{slug}` page mounts Excalidraw with `viewModeEnabled: false` and allows the WebSocket to send (not just receive) patches.
- **The UX:** It acts exactly like a Google Docs "Anyone with the link can edit" session. No sign-ups required for guests. Pure, chaotic, frictionless collaboration.

### Security & Ownership
- The original creator retains absolute ownership in their dashboard. They are the only one who can rename the file, delete it, or revoke the share link (by rolling a new slug or turning off public edits).
- Because guests don't need to authenticate to join a public edit session, we completely avoid the architectural bloat of building an invite system, email verification, and a relational `drawing_collaborators` permissions table.

This approach perfectly aligns with the project's brutalist philosophy: **Zero friction. Just draw.**
