// Package httpapi is the HTTP boundary of the management and query API.
// Handlers decode requests, call services, and encode results; business
// decisions do not live here.
package httpapi

import (
	"encoding/json"
	"net/http"
)

// Info describes the running server to API clients.
type Info struct {
	Version string
}

// Handler holds the dependencies the API handlers need.
type Handler struct {
	info Info
}

// NewHandler returns the API's http.Handler with every route registered.
// All routes live under /api/.
func NewHandler(info Info) http.Handler {
	h := &Handler{info: info}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", h.health)
	mux.HandleFunc("/api/", h.notFound)
	return mux
}

type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok", Version: h.info.Version})
}

func (h *Handler) notFound(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotFound, "not found")
}

type errorResponse struct {
	Error string `json:"error"`
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
