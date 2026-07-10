package api

import (
	"encoding/json"
	"net/http"

	"gotth/app/lib"
)

func SharedDataHandler(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")

	var content string
	var allowPublicEdits int
	err := lib.DB.QueryRowContext(r.Context(),
		"SELECT content, allow_public_edits FROM drawings WHERE share_slug = ?", slug,
	).Scan(&content, &allowPublicEdits)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	scene := sanitizeSceneJSON(content)

	// Embed allow_public_edits into the response so the client knows
	// whether to mount in viewMode or full-edit mode.
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(scene, &parsed); err != nil {
		http.NotFound(w, r)
		return
	}
	parsed["allowPublicEdits"] = json.RawMessage(func() string {
		if allowPublicEdits == 1 {
			return "true"
		}
		return "false"
	}())

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(parsed)
}
