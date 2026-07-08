package app

import (
	"net/http"
)

func PageHandler(w http.ResponseWriter, r *http.Request) {
	Layout("Canvas — Excalidraw", "/").Render(r.Context(), w)
}
