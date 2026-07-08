package main

import (
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"gotth/app"
	"gotth/app/assets"
	"gotth/app/auth"
	"gotth/app/docs"
	_ "github.com/a-h/templ"
)

func main() {
	auth.Init()

	mux := http.NewServeMux()

	publicFS, err := fs.Sub(assets.Public, "public")
	if err != nil {
		log.Fatal(err)
	}
	fileServer := http.FileServer(http.FS(publicFS))

	mux.Handle("GET /globals.css", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		w.Write(assets.CSS)
	}))

	mux.HandleFunc("GET /{file}", func(w http.ResponseWriter, r *http.Request) {
		fileName := r.PathValue("file")
		if file, err := publicFS.Open(fileName); err == nil {
			file.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
	})

	// Auth
	mux.HandleFunc("POST /auth/login", auth.LoginHandler)
	mux.HandleFunc("POST /auth/logout", auth.LogoutHandler)
	mux.HandleFunc("GET /auth/user", auth.UserHandler)

	// Dashboard
	mux.HandleFunc("GET /{$}", auth.RequireAuth(app.PageHandler))

	// Docs
	mux.HandleFunc("GET /docs", docs.PageHandler)
	mux.HandleFunc("GET /docs/{slug}", docs.PageHandler)

	wrapped := auth.Middleware(mux)

	port := os.Getenv("PORT")
	if port == "" {
		port = "3001"
	}
	addr := ":" + port
	fmt.Printf("Canvas running at http://localhost%s\n", addr)
	log.Fatal(http.ListenAndServe(addr, wrapped))
}
