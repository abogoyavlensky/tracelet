// Package web serves the embedded single-page frontend.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// dist is filled by `rite web-build`; without a build it holds only .gitkeep.
//
//go:embed all:dist
var dist embed.FS

// Assets returns the built frontend files.
func Assets() (fs.FS, error) {
	return fs.Sub(dist, "dist")
}

// Handler serves the frontend. Files under /assets/ carry a content hash in
// their name and are cached for a year. Any other path that is not a file
// gets index.html, so the browser router owns every URL outside /api/.
type Handler struct {
	assets fs.FS
	files  http.Handler
	index  []byte
}

// NewHandler returns a Handler over assets. A missing index.html means the
// frontend was not built; the handler then answers 404 instead of failing
// startup, so the API still runs.
func NewHandler(assets fs.FS) *Handler {
	index, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		index = nil
	}
	return &Handler{assets: assets, files: http.FileServerFS(assets), index: index}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.index == nil {
		http.Error(w, "frontend not built", http.StatusNotFound)
		return
	}

	name := strings.TrimPrefix(r.URL.Path, "/")
	if strings.HasPrefix(name, "assets/") {
		if !isFile(h.assets, name) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		h.files.ServeHTTP(w, r)
		return
	}

	if name != "" && isFile(h.assets, name) {
		w.Header().Set("Cache-Control", "no-cache")
		h.files.ServeHTTP(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(h.index)
}

func isFile(fsys fs.FS, name string) bool {
	info, err := fs.Stat(fsys, name)
	return err == nil && !info.IsDir()
}
