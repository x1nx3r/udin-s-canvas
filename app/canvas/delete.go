package canvas

import (
	"net/http"

	"gotth/app/auth"
	"gotth/app/db"
)

func DeleteHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	uid := auth.GetUserUID(r.Context())

	var ownerId string
	err := db.DB.QueryRowContext(r.Context(), "SELECT owner_id FROM drawings WHERE id = ?", id).Scan(&ownerId)
	if err != nil || ownerId != uid {
		http.NotFound(w, r)
		return
	}

	_, err = db.DB.ExecContext(r.Context(), "DELETE FROM drawings WHERE id = ?", id)
	if err != nil {
		http.Error(w, "delete failed", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}
