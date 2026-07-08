package docs

import (
	"bytes"
	"net/http"
	"strings"
	"gotth/app/assets"
	"github.com/yuin/goldmark"
)

func PageHandler(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	slug := strings.TrimPrefix(path, "/docs/")
	if slug == "/docs" || slug == "" || slug == "docs" {
		slug = "getting-started"
	}

	content, err := assets.Docs.ReadFile("docs/" + slug + ".md")
	if err != nil {
		http.NotFound(w, r)
		return
	}

	var buf bytes.Buffer
	if err := goldmark.Convert(content, &buf); err != nil {
		http.Error(w, "failed to render markdown", http.StatusInternalServerError)
		return
	}

	words := strings.Split(slug, "-")
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	title := strings.Join(words, " ")

	Page(title, buf.String(), "/docs/"+slug).Render(r.Context(), w)
}
