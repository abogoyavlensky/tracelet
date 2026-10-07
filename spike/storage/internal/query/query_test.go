package query_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abogoyavlensky/tracelet/spike/storage/internal/gen"
	"github.com/abogoyavlensky/tracelet/spike/storage/internal/query"
	"github.com/abogoyavlensky/tracelet/spike/storage/internal/telemetry"
	"github.com/abogoyavlensky/tracelet/spike/storage/internal/testenv"
)

var (
	h0 = testenv.Start
	h1 = h0.Add(time.Hour)
	h2 = h0.Add(2 * time.Hour)
	h3 = h0.Add(3 * time.Hour)
)

// setup ingests three hours and flushes hour 0, so data spans hot and cold.
func setup(t *testing.T) (query.Reader, []telemetry.Batch) {
	t.Helper()
	e := testenv.New(t)
	_, batches := e.IngestBatches(t, gen.NewGenerator(gen.Small, 1, 0), 3*time.Hour)
	stats, err := e.Flusher.FlushDue(t.Context(), h2)
	require.NoError(t, err)
	require.NotEmpty(t, stats)
	return query.Reader{DB: e.Store.DB(), Manifest: e.Manifest, DataDir: e.DataDir}, batches
}

func TestCountBySeverity(t *testing.T) {
	t.Parallel()
	r, batches := setup(t)

	want := map[[2]string]int64{}
	for _, b := range batches {
		for _, l := range b.Logs {
			want[[2]string{l.Service, l.SeverityText}]++
		}
	}

	got, err := r.CountBySeverity(t.Context(), h0, h3)
	require.NoError(t, err)
	gotMap := map[[2]string]int64{}
	for _, c := range got {
		gotMap[[2]string{c.Service, c.Severity}] = c.Count
	}
	assert.Equal(t, want, gotMap)
}

func TestTraceByIDAcrossHotAndCold(t *testing.T) {
	t.Parallel()
	e := testenv.New(t)
	ctx := t.Context()
	counts := testenv.Counts{}

	traceID := "feedfacefeedfacefeedfacefeedface"
	span := func(id, parent string, ts time.Time) telemetry.Span {
		return telemetry.Span{
			TS: ts, EndTS: ts.Add(time.Millisecond), Project: "default", Service: "svc-01",
			TraceID: traceID, SpanID: id, ParentSpanID: parent, Name: "op", DurationNS: int64(time.Millisecond),
		}
	}
	e.Write(t, telemetry.Batch{Spans: []telemetry.Span{
		span("00000000000000a1", "", h0.Add(59*time.Minute+59*time.Second)),
		span("00000000000000a2", "00000000000000a1", h1.Add(time.Second)),
	}}, counts)
	e.Ingest(t, gen.NewGenerator(gen.Small, 2, 0), 3*time.Hour)
	_, err := e.Flusher.FlushDue(ctx, h2)
	require.NoError(t, err)
	var hot int
	require.NoError(t, e.Store.DB().QueryRow("SELECT count(*) FROM spans WHERE trace_id = ?", traceID).Scan(&hot))
	require.Equal(t, 1, hot, "the hour-0 span is cold, the hour-1 span still hot")

	r := query.Reader{DB: e.Store.DB(), Manifest: e.Manifest, DataDir: e.DataDir}
	spans, err := r.TraceByID(ctx, traceID, h0, h3)
	require.NoError(t, err)
	require.Len(t, spans, 2)
	assert.Equal(t, "00000000000000a1", spans[0].SpanID)
	assert.Equal(t, "00000000000000a2", spans[1].SpanID)
}

func TestErrorLogsPage(t *testing.T) {
	t.Parallel()
	r, batches := setup(t)

	// Pick the service and route with the most error logs.
	tally := map[[2]string]int{}
	for _, b := range batches {
		for _, l := range b.Logs {
			if l.SeverityText != "ERROR" {
				continue
			}
			var attrs map[string]any
			require.NoError(t, json.Unmarshal([]byte(l.Attributes), &attrs))
			tally[[2]string{l.Service, attrs["http.route"].(string)}]++
		}
	}
	var key [2]string
	for k, n := range tally {
		if n > tally[key] {
			key = k
		}
	}
	require.Greater(t, tally[key], query.ErrorLogsPageSize, "enough errors to fill a page")

	logs, err := r.ErrorLogsPage(t.Context(), key[0], key[1], h0, h3)
	require.NoError(t, err)
	require.Len(t, logs, query.ErrorLogsPageSize)
	for i, l := range logs {
		assert.Equal(t, "ERROR", l.Severity)
		assert.Equal(t, key[0], l.Service)
		var attrs map[string]any
		require.NoError(t, json.Unmarshal([]byte(l.Attributes), &attrs))
		assert.Equal(t, key[1], attrs["http.route"])
		if i > 0 {
			assert.False(t, l.TS.After(logs[i-1].TS), "newest first")
		}
	}
}

func TestSourceWithoutFilesIsHotOnly(t *testing.T) {
	t.Parallel()
	q, args := query.Source("logs", h0, h1, nil)
	assert.NotContains(t, q, "read_parquet")
	assert.Len(t, args, 2)

	q, args = query.Source("logs", h0, h1, []string{"/data/it's.parquet"})
	assert.Contains(t, q, "read_parquet(['/data/it''s.parquet']")
	assert.Len(t, args, 4)
}
