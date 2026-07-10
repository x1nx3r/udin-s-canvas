package api

import (
	"sync"

	"github.com/gorilla/websocket"
)

// Room holds all active WebSocket connections for a single drawing session.
// The key is the connection pointer; the value is unused (map as a set).
type Room struct {
	mu      sync.Mutex
	clients map[*websocket.Conn]bool
}

func newRoom() *Room {
	return &Room{clients: make(map[*websocket.Conn]bool)}
}

func (r *Room) add(conn *websocket.Conn) {
	r.mu.Lock()
	r.clients[conn] = true
	r.mu.Unlock()
}

func (r *Room) remove(conn *websocket.Conn) {
	r.mu.Lock()
	delete(r.clients, conn)
	r.mu.Unlock()
}

func (r *Room) broadcast(sender *websocket.Conn, msgType int, msg []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for conn := range r.clients {
		if conn == sender {
			continue
		}
		// Non-blocking: if the write fails the connection is dead.
		// The readLoop for that connection will clean it up.
		conn.WriteMessage(msgType, msg)
	}
}

func (r *Room) empty() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.clients) == 0
}

// hub is the global registry of active rooms, keyed by drawing ID or share slug.
var hub = struct {
	mu    sync.RWMutex
	rooms map[string]*Room
}{
	rooms: make(map[string]*Room),
}

func getOrCreateRoom(key string) *Room {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if r, ok := hub.rooms[key]; ok {
		return r
	}
	r := newRoom()
	hub.rooms[key] = r
	return r
}

func deleteRoomIfEmpty(key string) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if r, ok := hub.rooms[key]; ok && r.empty() {
		delete(hub.rooms, key)
	}
}
