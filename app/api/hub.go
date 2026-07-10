package api

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"gotth/app/lib"
)

type Client struct {
	Conn *websocket.Conn
	Send chan []byte
}

type Room struct {
	mu           sync.Mutex
	clients      map[*Client]bool
	key          string
	drawingID    string
	createdAt    time.Time
	msgCount     int64
	lastElements []json.RawMessage
}

func newRoom(key string) *Room {
	drawingID := ""
	if len(key) > 5 && key[:5] == "draw:" {
		drawingID = key[5:]
	}
	r := &Room{
		clients:   make(map[*Client]bool),
		key:       key,
		drawingID: drawingID,
		createdAt: time.Now(),
	}
	r.loadFromDB()
	return r
}

func (r *Room) loadFromDB() {
	if r.drawingID == "" {
		return
	}
	var content string
	err := lib.DB.QueryRow("SELECT content FROM drawings WHERE id = ?", r.drawingID).Scan(&content)
	if err != nil || content == "" {
		return
	}
	var parsed struct {
		Elements []json.RawMessage `json:"elements"`
	}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		log.Printf("[hub] loadFromDB parse error for %s: %v", r.drawingID, err)
		return
	}
	if parsed.Elements != nil {
		r.lastElements = parsed.Elements
	}
}

func (r *Room) add(client *Client) {
	r.mu.Lock()
	r.clients[client] = true
	r.mu.Unlock()
}

func (r *Room) remove(client *Client) {
	r.mu.Lock()
	if _, ok := r.clients[client]; ok {
		delete(r.clients, client)
		close(client.Send)
	}
	r.mu.Unlock()
}

func (r *Room) sendSceneInit(client *Client) {
	r.mu.Lock()
	elements := r.lastElements
	r.mu.Unlock()
	msg, _ := json.Marshal(map[string]any{
		"type": "SCENE_INIT",
		"payload": map[string]any{
			"elements": elements,
		},
	})
	select {
	case client.Send <- msg:
	default:
		log.Printf("[hub] sendSceneInit DROP key=%s remote=%s buffer full", r.key, client.Conn.RemoteAddr())
		r.mu.Lock()
		close(client.Send)
		delete(r.clients, client)
		r.mu.Unlock()
	}
}

func (r *Room) handleMessage(sender *Client, msg []byte) {
	var incoming struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(msg, &incoming); err != nil {
		log.Printf("[hub] bad msg from %s: %v", sender.Conn.RemoteAddr(), err)
		return
	}
	switch incoming.Type {
	case "SCENE_UPDATE":
		var scene struct {
			Payload struct {
				Elements []json.RawMessage `json:"elements"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(msg, &scene); err != nil {
			log.Printf("[hub] SCENE_UPDATE parse error: %v", err)
			return
		}
		r.mu.Lock()
		r.lastElements = scene.Payload.Elements
		r.mu.Unlock()
		r.broadcastAllBut(sender, msg)
	case "MOUSE_LOCATION":
		r.broadcastAllBut(sender, msg)
	default:
		r.broadcastAllBut(sender, msg)
	}
}

func (r *Room) broadcastAllBut(sender *Client, msg []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgCount++
	count := 0
	start := time.Now()
	for client := range r.clients {
		if client == sender {
			continue
		}
		select {
		case client.Send <- msg:
			count++
		default:
			log.Printf("[hub] bcast DROP key=%s remote=%s buffer full", r.key, client.Conn.RemoteAddr())
			close(client.Send)
			delete(r.clients, client)
		}
	}
	_ = start
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
		_ = r
		delete(hub.rooms, key)
	}
}

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
