package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/abogoyavlensky/tracelet/internal/project"
	"github.com/abogoyavlensky/tracelet/internal/query"
)

// Snapshots opens the pinned read snapshot every query runs through.
type Snapshots interface {
	Open(ctx context.Context) (*query.Snapshot, error)
}

// queryTimeout bounds a query request, including the wait for a query slot.
const queryTimeout = 30 * time.Second

func (h *Handler) searchLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	projectID, ok := h.resolveProject(w, r, q.Get("project"))
	if !ok {
		return
	}

	p := query.LogsParams{
		ProjectID:   projectID,
		Service:     q.Get("service"),
		Environment: q.Get("environment"),
		Query:       q.Get("q"),
		TraceID:     q.Get("trace_id"),
		Cursor:      q.Get("cursor"),
	}
	if p.Cursor != "" && (q.Has("from") || q.Has("to") || q.Has("last")) {
		writeError(w, http.StatusBadRequest, "cursor cannot be combined with from, to, or last")
		return
	}
	if p.Cursor == "" {
		rng, err := h.parseRange(q)
		if err != nil {
			h.queryError(w, r, err)
			return
		}
		p.Range = rng
	}
	if level := q.Get("level"); level != "" {
		n, err := query.ParseLevel(level)
		if err != nil {
			h.queryError(w, r, err)
			return
		}
		p.MinSeverity = n
	}
	if limit := q.Get("limit"); limit != "" {
		n, err := strconv.Atoi(limit)
		if err != nil {
			writeError(w, http.StatusBadRequest, "limit must be a number")
			return
		}
		p.Limit = n
	}
	for _, s := range q["attr"] {
		a, err := query.ParseAttr(s)
		if err != nil {
			h.queryError(w, r, err)
			return
		}
		p.Attrs = append(p.Attrs, a)
	}

	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()
	snap, err := h.queries.Open(ctx)
	if err != nil {
		h.queryError(w, r, err)
		return
	}
	defer snap.Close()

	page, err := query.LogsSearch(ctx, snap, p)
	if err != nil {
		h.queryError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

type servicesResponse struct {
	Items []query.ServiceCount `json:"items"`
	Range query.Range          `json:"range"`
}

func (h *Handler) services(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	projectID, ok := h.resolveProject(w, r, q.Get("project"))
	if !ok {
		return
	}
	rng, err := h.parseRange(q)
	if err != nil {
		h.queryError(w, r, err)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()
	snap, err := h.queries.Open(ctx)
	if err != nil {
		h.queryError(w, r, err)
		return
	}
	defer snap.Close()

	items, err := query.Services(ctx, snap, projectID, rng)
	if err != nil {
		h.queryError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, servicesResponse{Items: items, Range: rng})
}

// resolveProject returns the project ID a read request is about. A
// project-scoped token fixes it, and naming another project is forbidden; an
// all-projects token must name one.
func (h *Handler) resolveProject(w http.ResponseWriter, r *http.Request, slug string) (string, bool) {
	tok := tokenFrom(r.Context())
	if tok.ProjectID != "" {
		if slug != "" && slug != tok.ProjectSlug {
			writeError(w, http.StatusForbidden, "token is limited to project "+tok.ProjectSlug)
			return "", false
		}
		return string(tok.ProjectID), true
	}
	if slug == "" {
		writeError(w, http.StatusBadRequest, "project is required")
		return "", false
	}
	p, err := h.projects.FindBySlug(r.Context(), slug)
	if errors.Is(err, project.ErrNotFound) {
		writeError(w, http.StatusNotFound, "project "+slug+" not found")
		return "", false
	}
	if err != nil {
		h.internalError(w, r, err)
		return "", false
	}
	return string(p.ID), true
}

// parseRange reads from and to (RFC 3339) or last (a Go duration).
func (h *Handler) parseRange(q url.Values) (query.Range, error) {
	var from, to time.Time
	var last time.Duration
	var err error
	if s := q.Get("from"); s != "" {
		if from, err = time.Parse(time.RFC3339Nano, s); err != nil {
			return query.Range{}, &query.ValidationError{Message: "from must be an RFC 3339 time"}
		}
	}
	if s := q.Get("to"); s != "" {
		if to, err = time.Parse(time.RFC3339Nano, s); err != nil {
			return query.Range{}, &query.ValidationError{Message: "to must be an RFC 3339 time"}
		}
	}
	if s := q.Get("last"); s != "" {
		if last, err = time.ParseDuration(s); err != nil || last <= 0 {
			return query.Range{}, &query.ValidationError{Message: "last must be a positive duration such as 30m or 2h"}
		}
	}
	return query.ResolveRange(from, to, last, h.now())
}

// queryError maps query failures: bad parameters are 400, running out of
// time or query slots is 503, anything else is logged and 500.
func (h *Handler) queryError(w http.ResponseWriter, r *http.Request, err error) {
	if verr, ok := errors.AsType[*query.ValidationError](err); ok {
		writeError(w, http.StatusBadRequest, verr.Message)
		return
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusServiceUnavailable, "query did not finish in time")
		return
	}
	h.internalError(w, r, err)
}
