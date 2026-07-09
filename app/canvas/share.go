package canvas

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"gotth/app/auth"
	"gotth/app/db"
)

func generateSlug() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func ShareHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	uid := auth.GetUserUID(r.Context())

	var ownerId string
	var existingSlug sql.NullString
	err := db.DB.QueryRowContext(r.Context(), "SELECT owner_id, share_slug FROM drawings WHERE id = ?", id).Scan(&ownerId, &existingSlug)
	if err != nil || ownerId != uid {
		http.NotFound(w, r)
		return
	}

	if existingSlug.Valid && existingSlug.String != "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"slug": existingSlug.String})
		return
	}

	slug, err := generateSlug()
	if err != nil {
		http.Error(w, "slug generation failed", http.StatusInternalServerError)
		return
	}

	_, err = db.DB.ExecContext(r.Context(),
		"UPDATE drawings SET share_slug = ?, updated_at = ? WHERE id = ?",
		slug, time.Now(), id)
	if err != nil {
		http.Error(w, "share creation failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"slug": slug})
}

func SharedDataHandler(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")

	var content string
	err := db.DB.QueryRowContext(r.Context(), "SELECT content FROM drawings WHERE share_slug = ?", slug).Scan(&content)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	sanitizedSceneData(w, content)
}

func sanitizedSceneData(w http.ResponseWriter, raw string) {
	sanitized := sanitizeSceneJSON(raw)
	w.Header().Set("Content-Type", "application/json")
	w.Write(sanitized)
}

func sanitizeSceneJSON(raw string) []byte {
	if raw == "" {
		raw = `{"elements":[],"appState":{}}`
	}

	var parsed struct {
		Elements json.RawMessage `json:"elements"`
		AppState map[string]any  `json:"appState"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return []byte(`{"elements":[],"appState":{}}`)
	}

	if parsed.AppState != nil {
		parsed.AppState["collaborators"] = []any{}
	}

	sanitized, _ := json.Marshal(parsed)
	return sanitized
}
