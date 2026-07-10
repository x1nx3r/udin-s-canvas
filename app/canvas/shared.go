package canvas

import (
	"net/http"

	"gotth/app"
	"gotth/app/lib"
)

func SharedPageHandler(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")

	var title string
	var allowPublicEdits int
	err := lib.DB.QueryRowContext(r.Context(),
		"SELECT title, allow_public_edits FROM drawings WHERE share_slug = ?", slug,
	).Scan(&title, &allowPublicEdits)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	meta := app.PageMeta{
		Description: "View \"" + title + "\" — a shared Excalidraw drawing on IMPHISE.",
	}
	SharedPage(title, slug, allowPublicEdits == 1, meta).Render(r.Context(), w)
}
