package canvas

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"gotth/app/auth"
	"gotth/app/db"
)

func ThumbnailHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	uid := auth.GetUserUID(r.Context())

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

	_, err = db.DB.ExecContext(r.Context(),
		"UPDATE drawings SET thumbnail = ?, updated_at = ? WHERE id = ?",
		string(body), time.Now(), id)
	if err != nil {
		http.Error(w, "save failed", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
