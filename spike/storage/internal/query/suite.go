package query

import (
	"context"
	"time"
)

// Metric names the suite queries; they match the generator's.
const (
	metricDuration = "http.server.request.duration"
	metricJobs     = "app.jobs.processed"
)

// SuiteParams fixes the inputs of the measured queries.
type SuiteParams struct {
	Now     time.Time
	TraceID string
	Service string
	Route   string
}

// Named is one measured query.
type Named struct {
	Name string
	Run  func(ctx context.Context, r Reader) (any, error)
}

// Suite returns the six measured queries from the design, with windows
// relative to p.Now.
func Suite(p SuiteParams) []Named {
	day := 24 * time.Hour
	week := 7 * day
	return []Named{
		{"count_by_severity", func(ctx context.Context, r Reader) (any, error) {
			return r.CountBySeverity(ctx, p.Now.Add(-day), p.Now)
		}},
		{"request_rate", func(ctx context.Context, r Reader) (any, error) {
			return r.RequestRate(ctx, metricDuration, p.Now.Add(-6*time.Hour), p.Now, time.Minute, DefaultInterval)
		}},
		{"p95_latency", func(ctx context.Context, r Reader) (any, error) {
			return r.HistogramQuantile(ctx, metricDuration, 0.95, p.Now.Add(-time.Hour), p.Now, DefaultInterval)
		}},
		{"jobs_rate", func(ctx context.Context, r Reader) (any, error) {
			return r.CounterRate(ctx, metricJobs, p.Now.Add(-day), p.Now, time.Minute, DefaultInterval)
		}},
		{"trace_by_id", func(ctx context.Context, r Reader) (any, error) {
			return r.TraceByID(ctx, p.TraceID, p.Now.Add(-week), p.Now)
		}},
		{"error_logs_page", func(ctx context.Context, r Reader) (any, error) {
			return r.ErrorLogsPage(ctx, p.Service, p.Route, p.Now.Add(-week), p.Now)
		}},
	}
}
