// Package httpapi is the HTTP boundary of the management and query API and
// the OTLP receiver. Handlers decode requests, call services, and encode
// results; business decisions do not live here.
package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/abogoyavlensky/tracelet/internal/project"
)

// Info describes the running server to API clients.
type Info struct {
	Version string
}

// Deps is everything the handlers call.
type Deps struct {
	Info     Info
	Auth     Authenticator
	Projects Projects
	Ingester Ingester
	Queries  Snapshots
	// Now is the clock that default and relative search ranges end at.
	Now func() time.Time
	// DiskPressure reports whether free disk is under the floor, in which
	// case ingestion is refused.
	DiskPressure func() bool
	Logger       *slog.Logger
}

// Handler holds the dependencies the API handlers need.
type Handler struct {
	info         Info
	auth         Authenticator
	projects     Projects
	ingester     Ingester
	queries      Snapshots
	now          func() time.Time
	diskPressure func() bool
	logger       *slog.Logger
}

// NewHandler returns the API's http.Handler with every route registered.
// The management API lives under /api/; OTLP lives under /v1/.
func NewHandler(deps Deps) http.Handler {
	h := &Handler{
		info: deps.Info, auth: deps.Auth, projects: deps.Projects, ingester: deps.Ingester,
		queries: deps.Queries, now: deps.Now, diskPressure: deps.DiskPressure, logger: deps.Logger,
	}
	if h.now == nil {
		h.now = time.Now
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", h.health)
	mux.HandleFunc("GET /api/v1/projects", h.requireScope(project.ScopeAdmin, h.listProjects))
	mux.HandleFunc("POST /api/v1/projects", h.requireScope(project.ScopeAdmin, h.createProject))
	mux.HandleFunc("GET /api/v1/tokens", h.requireScope(project.ScopeAdmin, h.listTokens))
	mux.HandleFunc("POST /api/v1/tokens", h.requireScope(project.ScopeAdmin, h.createToken))
	mux.HandleFunc("DELETE /api/v1/tokens/{id}", h.requireScope(project.ScopeAdmin, h.revokeToken))
	mux.HandleFunc("GET /api/v1/logs", h.requireScope(project.ScopeRead, h.searchLogs))
	mux.HandleFunc("GET /api/v1/services", h.requireScope(project.ScopeRead, h.services))
	mux.HandleFunc("POST /v1/logs", h.requireScope(project.ScopeIngest, h.otlpLogs))
	mux.HandleFunc("/api/", h.notFound)
	mux.HandleFunc("/v1/", h.notFound)
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

// serviceError maps a service error to its status. Unexpected errors are
// logged here, once, and the client sees a generic message.
func (h *Handler) serviceError(w http.ResponseWriter, r *http.Request, err error) {
	if verr, ok := errors.AsType[*project.ValidationError](err); ok {
		writeError(w, http.StatusBadRequest, verr.Message)
		return
	}
	switch {
	case errors.Is(err, project.ErrSlugTaken):
		writeError(w, http.StatusConflict, "project slug already taken")
	case errors.Is(err, project.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	default:
		h.internalError(w, r, err)
	}
}

func (h *Handler) internalError(w http.ResponseWriter, r *http.Request, err error) {
	h.logger.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}

// maxJSONBody bounds management API request bodies.
const maxJSONBody = 1 << 20

// decodeJSON decodes the request body into v, answering 400 or 413 itself
// and returning false when it cannot.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody))
	dec.DisallowUnknownFields()
	err := dec.Decode(v)
	if err == nil && dec.Decode(&struct{}{}) != io.EOF {
		err = errors.New("unexpected data after the JSON object")
	}
	if err == nil {
		return true
	}
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		return false
	}
	writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
	return false
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
