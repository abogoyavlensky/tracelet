package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/abogoyavlensky/tracelet/internal/query"
)

func (h *Handler) searchSpans(w http.ResponseWriter, r *http.Request) {
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
	p := query.SpansParams{
		ProjectID:   projectID,
		Service:     q.Get("service"),
		Environment: q.Get("environment"),
		Name:        q.Get("name"),
		TraceID:     q.Get("trace_id"),
		Attrs:       sp.attrs,
		Range:       sp.rng,
		Limit:       sp.limit,
		Cursor:      sp.cursor,
	}
	code, set, err := query.ParseStatus(q.Get("status"))
	if err != nil {
		h.queryError(w, r, err)
		return
	}
	if set {
		p.Status = &code
	}
	if s := q.Get("min_duration"); s != "" {
		d, err := time.ParseDuration(s)
		if err != nil || d <= 0 {
			writeError(w, http.StatusBadRequest, "min_duration must be a positive duration such as 500ms or 2s")
			return
		}
		p.MinDurationNS = d.Nanoseconds()
	}
	if s := q.Get("root"); s != "" {
		root, err := strconv.ParseBool(s)
		if err != nil {
			writeError(w, http.StatusBadRequest, "root must be true or false")
			return
		}
		p.Root = root
	}

	h.runQuery(w, r, func(ctx context.Context, snap *query.Snapshot) (any, error) {
		return query.SpansSearch(ctx, snap, p)
	})
}

func (h *Handler) getTrace(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	projectID, ok := h.resolveProject(w, r, q.Get("project"))
	if !ok {
		return
	}
	traceID, err := query.ParseTraceID(r.PathValue("trace_id"))
	if err != nil {
		h.queryError(w, r, err)
		return
	}
	rng, err := h.parseRange(q, query.DefaultTraceLast)
	if err != nil {
		h.queryError(w, r, err)
		return
	}

	h.runQuery(w, r, func(ctx context.Context, snap *query.Snapshot) (any, error) {
		tr, err := query.TraceGet(ctx, snap, query.TraceParams{ProjectID: projectID, TraceID: traceID, Range: rng})
		if errors.Is(err, query.ErrTraceNotFound) {
			return nil, notFoundError{"trace " + traceID + " not found between " + rng.From.Format(time.RFC3339Nano) +
				" and " + rng.To.Format(time.RFC3339Nano) + "; widen the range with from/to or last"}
		}
		return tr, err
	})
}

// notFoundError is a query result that is a 404 with a message for the client.
type notFoundError struct{ msg string }

func (e notFoundError) Error() string { return e.msg }
