package api

import (
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"gotth/app/lib"
)

const (
	// Maximum size of an incoming WebSocket frame. 512KB is generous for
	// Excalidraw element patches; a 50MB malicious payload is dead on arrival.
	maxReadBytes = 512 * 1024

	// How often the server pings idle clients.
	pingInterval = 30 * time.Second

	// If no pong is received within this window, the connection is considered dead.
	// Must be > pingInterval to avoid false positives.
	pongDeadline = 60 * time.Second
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// Allow same origin only in production; the Caddy reverse proxy sits in front.
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// OwnerWSHandler handles authenticated WebSocket connections from the drawing owner.
// Route: GET /api/draw/{id}/ws
func OwnerWSHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !checkOwnership(r, id) {
		http.NotFound(w, r)
		return
	}
	serveWS(w, r, id)
}

// GuestWSHandler handles anonymous WebSocket connections from users with a share link.
// Guests are only admitted if allow_public_edits is true for the drawing.
// Route: GET /api/shared/{slug}/ws
func GuestWSHandler(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")

	var allowPublicEdits int
	err := lib.DB.QueryRowContext(r.Context(),
		`SELECT allow_public_edits FROM drawings WHERE share_slug = ?`, slug,
	).Scan(&allowPublicEdits)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if allowPublicEdits == 0 {
		// Room is in view-only mode; no write socket needed.
		http.NotFound(w, r)
		return
	}

	serveWS(w, r, slug)
}

// serveWS upgrades the connection, registers it in the hub, and starts the read loop.
func serveWS(w http.ResponseWriter, r *http.Request, roomKey string) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade [%s]: %v", roomKey, err)
		return
	}

	// OOM guard: reject frames larger than 512KB.
	conn.SetReadLimit(maxReadBytes)

	// Laptop-lid / silent TCP drop guard: set an initial read deadline.
	// The pong handler below resets it on every successful pong.
	conn.SetReadDeadline(time.Now().Add(pongDeadline))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(pongDeadline))
		return nil
	})

	room := getOrCreateRoom(roomKey)
	room.add(conn)

	// Ping goroutine: keep the connection alive and detect silent drops.
	go func() {
		ticker := time.NewTicker(pingInterval)
		defer ticker.Stop()
		for range ticker.C {
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}()

	// Read loop: broadcast every incoming message to all other room members.
	defer func() {
		conn.Close()
		room.remove(conn)
		deleteRoomIfEmpty(roomKey)
	}()

	for {
		msgType, msg, err := conn.ReadMessage()
		if err != nil {
			// Normal close or deadline exceeded — exit cleanly.
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("ws read [%s]: %v", roomKey, err)
			}
			return
		}
		room.broadcast(conn, msgType, msg)
	}
}
