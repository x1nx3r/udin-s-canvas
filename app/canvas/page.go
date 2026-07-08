package canvas

import (
	"log"
	"net/http"
	"gotth/app/auth"
)

func PageHandler(w http.ResponseWriter, r *http.Request) {
	uid := auth.GetUserUID(r.Context())
	if uid == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	id := r.PathValue("id")

	doc, err := auth.Firestore.Collection("drawings").Doc(id).Get(r.Context())
	if err != nil {
		log.Printf("load drawing %s: %v", id, err)
		http.NotFound(w, r)
		return
	}

	ownerId, _ := doc.Data()["ownerId"].(string)
	if ownerId != uid {
		http.NotFound(w, r)
		return
	}

	title, _ := doc.Data()["title"].(string)

	CanvasPage(title, id).Render(r.Context(), w)
}
