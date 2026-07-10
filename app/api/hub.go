package api

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Client struct {
	Conn *websocket.Conn
	Send chan []byte
}

// Room holds all active WebSocket clients for a single drawing session.
type Room struct {
	mu        sync.Mutex
	clients   map[*Client]bool
	key       string
	createdAt time.Time
	msgCount  int64
}

func newRoom(key string) *Room {
	r := &Room{
		clients:   make(map[*Client]bool),
		key:       key,
		createdAt: time.Now(),
	}
	log.Printf("[hub] room OPEN  key=%s", key)
	return r
}

func (r *Room) add(client *Client) {
	r.mu.Lock()
	r.clients[client] = true
	n := len(r.clients)
	r.mu.Unlock()
	log.Printf("[hub] conn  JOIN  key=%s peers=%d remote=%s", r.key, n, client.Conn.RemoteAddr())
}

func (r *Room) remove(client *Client) {
	r.mu.Lock()
	if _, ok := r.clients[client]; ok {
		delete(r.clients, client)
		close(client.Send)
	}
	n := len(r.clients)
	r.mu.Unlock()
	log.Printf("[hub] conn  LEFT  key=%s peers=%d remote=%s", r.key, n, client.Conn.RemoteAddr())
}

// broadcast queues msg to every client in the room except the sender.
// Slow clients whose channels are full will be disconnected to prevent blocking.
func (r *Room) broadcast(sender *Client, msg []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.msgCount++
	count := 0
	skipped := 0
	start := time.Now()

	for client := range r.clients {
		if client == sender {
			skipped++
			continue
		}
		select {
		case client.Send <- msg:
			count++
		default:
			// Client's send buffer is full; disconnect slow client.
			log.Printf("[hub] bcast DROP  key=%s remote=%s buffer full", r.key, client.Conn.RemoteAddr())
			close(client.Send)
			delete(r.clients, client)
		}
	}

	total := time.Since(start)
	log.Printf("[hub] bcast DONE  key=%s msg#=%d bytes=%d sent=%d skip=%d total=%s",
		r.key, r.msgCount, len(msg), count, skipped, total)
}

func (r *Room) snapshot() (int, int64, time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.clients), r.msgCount, time.Since(r.createdAt)
}

func (r *Room) empty() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.clients) == 0
}

// hub is the global registry of active rooms, keyed by "draw:"+drawingID.
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
	r := newRoom(key)
	hub.rooms[key] = r
	return r
}

func deleteRoomIfEmpty(key string) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if r, ok := hub.rooms[key]; ok && r.empty() {
		log.Printf("[hub] room CLOSE key=%s msgs=%d age=%s", key, r.msgCount, time.Since(r.createdAt))
		delete(hub.rooms, key)
	}
}

// HubStats returns a human-readable snapshot of all active rooms.
func HubStats() string {
	hub.mu.RLock()
	defer hub.mu.RUnlock()
	if len(hub.rooms) == 0 {
		return "no active rooms\n"
	}
	out := fmt.Sprintf("active rooms: %d\n", len(hub.rooms))
	for key, r := range hub.rooms {
		peers, msgs, age := r.snapshot()
		out += fmt.Sprintf("  %-36s  peers=%-3d msgs=%-6d age=%s\n", key, peers, msgs, age.Round(time.Second))
	}
	return out
}
