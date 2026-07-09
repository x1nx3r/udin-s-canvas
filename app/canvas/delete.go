package canvas

import (
	"net/http"

	"gotth/app/auth"
)

func DeleteHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	uid := auth.GetUserUID(r.Context())

	doc, err := auth.Firestore.Collection("drawings").Doc(id).Get(r.Context())
	if err != nil {
		http.NotFound(w, r)
		return
	}
	data := doc.Data()
	ownerId, _ := data["ownerId"].(string)
	if ownerId != uid {
		http.NotFound(w, r)
		return
	}

	// Delete shares associated with this drawing
	shareSlug, _ := data["shareSlug"].(string)
	if shareSlug != "" {
		auth.Firestore.Collection("shares").Doc(shareSlug).Delete(r.Context())
	}

	_, err = auth.Firestore.Collection("drawings").Doc(id).Delete(r.Context())
	if err != nil {
		http.Error(w, "delete failed", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}
