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
	sp, err := h.parseSearch(q)
	if err != nil {
		h.queryError(w, r, err)
		return
	}
	p := query.LogsParams{
		ProjectID:   projectID,
		Service:     q.Get("service"),
		Environment: q.Get("environment"),
		Query:       q.Get("q"),
		TraceID:     q.Get("trace_id"),
		Attrs:       sp.attrs,
		Range:       sp.rng,
		Limit:       sp.limit,
		Cursor:      sp.cursor,
		Since:       q.Get("since"),
	}
	if level := q.Get("level"); level != "" {
		n, err := query.ParseLevel(level)
		if err != nil {
			h.queryError(w, r, err)
			return
		}
		p.MinSeverity = n
	}

	h.runQuery(w, r, func(ctx context.Context, snap *query.Snapshot) (any, error) {
		return query.LogsSearch(ctx, snap, p)
	})
}

// searchParams are the parameters every search shares.
type searchParams struct {
	cursor string
	rng    query.Range // zero when a cursor positions the search
	limit  int
	attrs  []query.Attr
}

// parseSearch reads cursor, the range (last hour by default), limit, and the
// repeatable attr. A cursor carries its own range, so it excludes from, to,
// and last.
func (h *Handler) parseSearch(q url.Values) (searchParams, error) {
	sp := searchParams{cursor: q.Get("cursor")}
	if sp.cursor != "" && (q.Has("from") || q.Has("to") || q.Has("last")) {
		return sp, &query.ValidationError{Message: "cursor cannot be combined with from, to, or last"}
	}
	if sp.cursor == "" {
		rng, err := h.parseRange(q, query.DefaultLast)
		if err != nil {
			return sp, err
		}
		sp.rng = rng
	}
	if limit := q.Get("limit"); limit != "" {
		n, err := strconv.Atoi(limit)
		if err != nil {
			return sp, &query.ValidationError{Message: "limit must be a number"}
		}
		sp.limit = n
	}
	attrs, err := parseAttrs(q)
	if err != nil {
		return sp, err
	}
	sp.attrs = attrs
	return sp, nil
}

// runQuery opens a snapshot under the query timeout, runs fn on it, and
// answers with its result as JSON or with the mapped error.
func (h *Handler) runQuery(w http.ResponseWriter, r *http.Request, fn func(context.Context, *query.Snapshot) (any, error)) {
	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()
	snap, err := h.queries.Open(ctx)
	if err != nil {
		h.queryError(w, r, err)
		return
	}
	defer snap.Close()

	v, err := fn(ctx, snap)
	if err != nil {
		h.queryError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
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
	rng, err := h.parseRange(q, query.DefaultLast)
	if err != nil {
		h.queryError(w, r, err)
		return
	}

	h.runQuery(w, r, func(ctx context.Context, snap *query.Snapshot) (any, error) {
		items, err := query.Services(ctx, snap, projectID, rng)
		return servicesResponse{Items: items, Range: rng}, err
	})
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

// parseRange reads from and to (RFC 3339) or last (a Go duration); with
// none of them the range is the defaultLast ending now.
func (h *Handler) parseRange(q url.Values, defaultLast time.Duration) (query.Range, error) {
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
	return query.ResolveRange(from, to, last, defaultLast, h.now())
}

// queryError maps query failures: bad parameters are 400, a missing result
// is 404, running out of time or query slots is 503, anything else is logged
// and 500.
func (h *Handler) queryError(w http.ResponseWriter, r *http.Request, err error) {
	if verr, ok := errors.AsType[*query.ValidationError](err); ok {
		writeError(w, http.StatusBadRequest, verr.Message)
		return
	}
	if nerr, ok := errors.AsType[notFoundError](err); ok {
		writeError(w, http.StatusNotFound, nerr.msg)
		return
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusServiceUnavailable, "query did not finish in time")
		return
	}
	h.internalError(w, r, err)
}
