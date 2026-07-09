package canvas

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"

	"gotth/app/auth"
	"gotth/app/db"
)

type sceneData struct {
	Elements json.RawMessage `json:"elements"`
	AppState json.RawMessage `json:"appState"`
}

func loadDrawing(r *http.Request, id string) (string, bool) {
	uid := auth.GetUserUID(r.Context())
	if uid == "" {
		return "", false
	}

	var ownerId string
	err := db.DB.QueryRowContext(r.Context(), "SELECT owner_id FROM drawings WHERE id = ?", id).Scan(&ownerId)
	if err != nil {
		return "", false
	}
	if ownerId != uid {
		return "", false
	}

	return ownerId, true
}

func DataHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	uid := auth.GetUserUID(r.Context())
	if uid == "" {
		http.NotFound(w, r)
		return
	}

	var ownerId, content string
	err := db.DB.QueryRowContext(r.Context(), "SELECT owner_id, content FROM drawings WHERE id = ?", id).Scan(&ownerId, &content)
	if err != nil || ownerId != uid {
		http.NotFound(w, r)
		return
	}

	sanitizedSceneData(w, content)
}

func SaveHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	uid := auth.GetUserUID(r.Context())
	if uid == "" {
		http.NotFound(w, r)
		return
	}

	var ownerId string
	err := db.DB.QueryRowContext(r.Context(), "SELECT owner_id FROM drawings WHERE id = ?", id).Scan(&ownerId)
	if err != nil || ownerId != uid {
		http.NotFound(w, r)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	var sd sceneData
	if err := json.Unmarshal(body, &sd); err != nil {
		http.Error(w, "invalid scene data", http.StatusBadRequest)
		return
	}

	_, err = db.DB.ExecContext(r.Context(),
		"UPDATE drawings SET content = ?, updated_at = ? WHERE id = ?",
		string(body), time.Now(), id)
	if err != nil {
		log.Printf("save drawing %s: %v", id, err)
		http.Error(w, "save failed", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}
