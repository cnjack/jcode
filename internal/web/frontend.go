package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed dist/*
var distFS embed.FS

// frontendFS returns an http.FileSystem rooted at the embedded dist/ directory.
func frontendFS() http.FileSystem {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic("embedded dist/ not found: " + err.Error())
	}
	return http.FS(sub)
}

// spaHandler serves the embedded SPA frontend.
// For known static file extensions it serves the file directly;
// all other paths fall back to index.html for client-side routing.
type spaHandler struct {
	fs http.FileSystem
}

func newSPAHandler() http.Handler {
	return &spaHandler{fs: frontendFS()}
}

func (h *spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "/" {
		path = "/index.html"
	}

	// Try to serve static file first.
	f, err := h.fs.Open(path)
	if err == nil {
		f.Close()
		http.FileServer(h.fs).ServeHTTP(w, r)
		return
	}

	// If the path looks like a static asset (has extension), return 404.
	if lastDot := strings.LastIndex(path, "."); lastDot > strings.LastIndex(path, "/") {
		http.NotFound(w, r)
		return
	}

	// SPA fallback: serve index.html for routing paths.
	r.URL.Path = "/index.html"
	http.FileServer(h.fs).ServeHTTP(w, r)
}
