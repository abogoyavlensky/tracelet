package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/abogoyavlensky/tracelet/internal/query"
)

func (h *Handler) listMetrics(w http.ResponseWriter, r *http.Request) {
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
	p := query.MetricsParams{
		ProjectID: projectID, Service: q.Get("service"), Environment: q.Get("environment"),
		Query: q.Get("q"), Range: rng,
	}
	h.runQuery(w, r, func(ctx context.Context, snap *query.Snapshot) (any, error) {
		return query.ListMetrics(ctx, snap, p)
	})
}

func (h *Handler) metricAttributes(w http.ResponseWriter, r *http.Request) {
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
	p := query.MetricAttributesParams{
		ProjectID: projectID, Name: q.Get("name"), Service: q.Get("service"),
		Environment: q.Get("environment"), Range: rng,
	}
	h.runQuery(w, r, func(ctx context.Context, snap *query.Snapshot) (any, error) {
		return query.ListMetricAttributes(ctx, snap, p)
	})
}

func (h *Handler) queryMetric(w http.ResponseWriter, r *http.Request) {
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
	attrs, err := parseAttrs(q)
	if err != nil {
		h.queryError(w, r, err)
		return
	}
	p := query.MetricQueryParams{
		ProjectID: projectID, Name: q.Get("name"), Service: q.Get("service"), Environment: q.Get("environment"),
		Attrs: attrs, GroupBy: q.Get("group_by"), Aggregate: q.Get("aggregate"), Range: rng,
	}
	if s := q.Get("step"); s != "" {
		d, err := time.ParseDuration(s)
		if err != nil || d <= 0 {
			writeError(w, http.StatusBadRequest, "step must be a positive duration such as 30s or 5m")
			return
		}
		p.Step = d
	}

	h.runQuery(w, r, func(ctx context.Context, snap *query.Snapshot) (any, error) {
		res, err := query.MetricQuery(ctx, snap, p)
		if errors.Is(err, query.ErrMetricNotFound) {
			return nil, notFoundError{"metric " + p.Name + " has no points between " + rng.From.Format(time.RFC3339Nano) +
				" and " + rng.To.Format(time.RFC3339Nano)}
		}
		return res, err
	})
}

// parseAttrs reads the repeatable attr parameter, key:value.
func parseAttrs(q url.Values) ([]query.Attr, error) {
	var attrs []query.Attr
	for _, s := range q["attr"] {
		a, err := query.ParseAttr(s)
		if err != nil {
			return nil, err
		}
		attrs = append(attrs, a)
	}
	return attrs, nil
}
