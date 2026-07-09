package canvas

import (
	"encoding/json"
	"net/http"
	"time"

	"gotth/app/auth"
	"gotth/app/db"
)

func RenameHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	uid := auth.GetUserUID(r.Context())

	var ownerId string
	err := db.DB.QueryRowContext(r.Context(), "SELECT owner_id FROM drawings WHERE id = ?", id).Scan(&ownerId)
	if err != nil || ownerId != uid {
		http.NotFound(w, r)
		return
	}

	var body struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Title == "" {
		http.Error(w, "missing or invalid title", http.StatusBadRequest)
		return
	}

	_, err = db.DB.ExecContext(r.Context(),
		"UPDATE drawings SET title = ?, updated_at = ? WHERE id = ?",
		body.Title, time.Now(), id)
	if err != nil {
		http.Error(w, "rename failed", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}
