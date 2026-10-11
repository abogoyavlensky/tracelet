package duckdb_test

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abogoyavlensky/tracelet/internal/duckdb"
	"github.com/abogoyavlensky/tracelet/internal/telemetry"
)

func sampleBatch(ts time.Time) telemetry.Batch {
	var b telemetry.Batch
	for i := range 3 {
		b.Logs = append(b.Logs, telemetry.Log{
			TS: ts, Project: "default", Service: "svc-01",
			SeverityNumber: 9, SeverityText: "INFO", Body: "hello",
			Attributes: fmt.Sprintf(`{"seq":%d}`, i),
		})
	}
	for i := range 2 {
		b.Spans = append(b.Spans, telemetry.Span{
			TS: ts, EndTS: ts.Add(time.Millisecond), Project: "default", Service: "svc-01",
			// IDs unique per ts, so two sample batches are not duplicates.
			TraceID: fmt.Sprintf("%032x", ts.UnixNano()), SpanID: fmt.Sprintf("%016x", ts.UnixNano()+int64(i)),
			Name: "GET /", Kind: 2, DurationNS: int64(time.Millisecond),
		})
	}
	count := uint64(4)
	value := int64(7)
	b.Points = []telemetry.MetricPoint{
		{
			TS: ts, Project: "default", Service: "svc-01", Name: "app.jobs.processed",
			Type: telemetry.TypeSum, Temporality: telemetry.TemporalityCumulative, Monotonic: true,
			SeriesHash: 1, ValueInt: &value,
		},
		{
			TS: ts, Project: "default", Service: "svc-01", Name: "http.server.request.duration",
			Type: telemetry.TypeHistogram, Temporality: telemetry.TemporalityCumulative,
			SeriesHash: 2, Count: &count, BucketCounts: []uint64{1, 2, 1}, BucketBounds: []float64{10, 50},
		},
	}
	return b
}

func countRows(t *testing.T, s *duckdb.Store, table string) int {
	t.Helper()
	var n int
	require.NoError(t, s.DB().QueryRow("SELECT count(*) FROM "+table).Scan(&n))
	return n
}

func TestWriterWritesBatch(t *testing.T) {
	s := openStore(t)
	w, err := duckdb.NewWriter(t.Context(), s, time.Time{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	stats, err := w.Write(t.Context(), sampleBatch(time.Now().UTC()))
	require.NoError(t, err)

	assert.Equal(t, 7, stats.Rows)
	assert.Zero(t, stats.Duplicates)
	assert.Positive(t, stats.Latency)
	assert.Equal(t, 3, countRows(t, s, "logs"))
	assert.Equal(t, 2, countRows(t, s, "spans"))
	assert.Equal(t, 2, countRows(t, s, "metric_points"))

	var nullEnv, nullValueDouble, nullBuckets bool
	err = s.DB().QueryRow(`SELECT environment IS NULL, value_double IS NULL, bucket_counts IS NULL
		FROM metric_points WHERE series_hash = 1`).Scan(&nullEnv, &nullValueDouble, &nullBuckets)
	require.NoError(t, err)
	assert.True(t, nullEnv)
	assert.True(t, nullValueDouble)
	assert.True(t, nullBuckets)
}

func TestWriterEmptyBatch(t *testing.T) {
	s := openStore(t)
	w, err := duckdb.NewWriter(t.Context(), s, time.Time{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	stats, err := w.Write(t.Context(), telemetry.Batch{})
	require.NoError(t, err)
	assert.Equal(t, 0, stats.Rows)
	assert.Equal(t, 0, countRows(t, s, "logs"))
}

func TestWriterStampsIncrease(t *testing.T) {
	s := openStore(t)
	w, err := duckdb.NewWriter(t.Context(), s, time.Time{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	// Back-to-back writes routinely land in the same microsecond.
	for range 20 {
		_, err := w.Write(t.Context(), telemetry.Batch{Logs: sampleBatch(time.Now().UTC()).Logs[:1]})
		require.NoError(t, err)
	}

	assertIncreasing(t, ingestStamps(t, s, "logs"), 20)
}

func TestWriterStampsEachRow(t *testing.T) {
	s := openStore(t)
	w, err := duckdb.NewWriter(t.Context(), s, time.Time{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	_, err = w.Write(t.Context(), sampleBatch(time.Now().UTC()))
	require.NoError(t, err)
	_, err = w.Write(t.Context(), sampleBatch(time.Now().UTC()))
	require.NoError(t, err)

	// Stamps are distinct within a batch and across batches, and all of one
	// batch's rows precede the next batch's.
	stamps := append(ingestStamps(t, s, "logs"), ingestStamps(t, s, "spans")...)
	stamps = append(stamps, ingestStamps(t, s, "metric_points")...)
	slices.SortFunc(stamps, time.Time.Compare)
	assertIncreasing(t, stamps, 14)
}

func TestWriterSeedsStampFromStore(t *testing.T) {
	dir := t.TempDir()
	s, err := duckdb.Open(dir, duckdb.Limits{MemoryLimit: "64MB", Threads: 1})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	// A stored stamp far in the future stands in for a clock that stepped back.
	future := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	_, err = s.DB().Exec(`INSERT INTO logs (ts, ingest_ts, project, service) VALUES (?, ?, 'p', 'svc')`, future, future)
	require.NoError(t, err)

	w, err := duckdb.NewWriter(t.Context(), s, time.Time{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })
	_, err = w.Write(t.Context(), telemetry.Batch{Logs: sampleBatch(time.Now().UTC()).Logs[:1]})
	require.NoError(t, err)

	stamps := ingestStamps(t, s, "logs")
	require.Len(t, stamps, 2)
	assert.Equal(t, future.Add(time.Microsecond), stamps[1])
}

func TestWriterSeedsStampFromFloor(t *testing.T) {
	s := openStore(t)
	floor := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	w, err := duckdb.NewWriter(t.Context(), s, floor)
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	_, err = w.Write(t.Context(), telemetry.Batch{Logs: sampleBatch(time.Now().UTC()).Logs[:1]})
	require.NoError(t, err)
	assert.Equal(t, []time.Time{floor.Add(time.Microsecond)}, ingestStamps(t, s, "logs"))
}

func ingestStamps(t *testing.T, s *duckdb.Store, table string) []time.Time {
	t.Helper()
	rows, err := s.DB().Query("SELECT ingest_ts FROM " + table + " ORDER BY ingest_ts")
	require.NoError(t, err)
	defer rows.Close()
	var stamps []time.Time
	for rows.Next() {
		var ts time.Time
		require.NoError(t, rows.Scan(&ts))
		stamps = append(stamps, ts.UTC())
	}
	require.NoError(t, rows.Err())
	return stamps
}

func assertIncreasing(t *testing.T, stamps []time.Time, n int) {
	t.Helper()
	require.Len(t, stamps, n)
	for i := 1; i < len(stamps); i++ {
		assert.True(t, stamps[i].After(stamps[i-1]), "stamp %d not after %d", i, i-1)
	}
}

func TestWriterFailedBatchDoesNotLeak(t *testing.T) {
	s := openStore(t)
	w, err := duckdb.NewWriter(t.Context(), s, time.Time{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	bad := sampleBatch(time.Now().UTC())
	bad.Logs[2].TS = time.Date(300000, 1, 1, 0, 0, 0, 0, time.UTC) // out of TIMESTAMP range
	_, err = w.Write(t.Context(), bad)
	require.Error(t, err)

	_, err = w.Write(t.Context(), telemetry.Batch{Logs: sampleBatch(time.Now().UTC()).Logs[:1]})
	require.NoError(t, err)

	assert.Equal(t, 1, countRows(t, s, "logs"))
}

func span(project, trace, id string, ts time.Time) telemetry.Span {
	return telemetry.Span{
		TS: ts, EndTS: ts.Add(time.Millisecond), Project: project, Service: "api",
		TraceID: fmt.Sprintf("%032s", trace), SpanID: fmt.Sprintf("%016s", id),
		Name: "op", DurationNS: int64(time.Millisecond),
	}
}

func newWriter(t *testing.T) (*duckdb.Store, *duckdb.Writer) {
	t.Helper()
	s := openStore(t)
	w, err := duckdb.NewWriter(t.Context(), s, time.Time{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })
	return s, w
}

func spanNames(t *testing.T, s *duckdb.Store) map[string]string {
	t.Helper()
	rows, err := s.DB().Query("SELECT project || '/' || trace_id || '/' || span_id, name FROM spans")
	require.NoError(t, err)
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, name string
		require.NoError(t, rows.Scan(&k, &name))
		out[k] = name
	}
	require.NoError(t, rows.Err())
	return out
}

func TestWriterDropsDuplicateSpans(t *testing.T) {
	s, w := newWriter(t)
	ts := time.Now().UTC().Truncate(time.Microsecond)

	first := []telemetry.Span{span("p1", "1", "1", ts), span("p1", "1", "2", ts), span("p1", "2", "3", ts)}
	stats, err := w.Write(t.Context(), telemetry.Batch{Spans: first})
	require.NoError(t, err)
	assert.Equal(t, 3, stats.Rows)
	assert.Zero(t, stats.Duplicates)

	repeat := span("p1", "2", "5", ts)
	again := repeat
	again.Name = "second copy"
	second := append(append([]telemetry.Span{}, first...), span("p1", "1", "4", ts), repeat, again)
	stats, err = w.Write(t.Context(), telemetry.Batch{Spans: second})
	require.NoError(t, err)
	assert.Equal(t, 2, stats.Rows)
	assert.Equal(t, 4, stats.Duplicates)

	assert.Equal(t, 5, countRows(t, s, "spans"))
	names := spanNames(t, s)
	assert.Equal(t, "op", names["p1/"+repeat.TraceID+"/"+repeat.SpanID], "the first copy is kept")
	assert.Len(t, second, 6, "the caller's batch is not mutated")
}

func TestWriterDedupIgnoresOtherHours(t *testing.T) {
	s, w := newWriter(t)
	ts := time.Now().UTC().Truncate(time.Hour).Add(-3 * time.Hour)

	_, err := w.Write(t.Context(), telemetry.Batch{Spans: []telemetry.Span{span("p1", "1", "1", ts)}})
	require.NoError(t, err)

	// The repeat is two hours later, so the stored copy is only in range
	// because the batch's other span widens it back to ts.
	stats, err := w.Write(t.Context(), telemetry.Batch{Spans: []telemetry.Span{
		span("p1", "1", "1", ts.Add(2*time.Hour)),
		span("p1", "1", "2", ts),
	}})
	require.NoError(t, err)
	assert.Equal(t, 1, stats.Rows)
	assert.Equal(t, 1, stats.Duplicates)
	assert.Equal(t, 2, countRows(t, s, "spans"))
}

func TestWriterDedupIsPerProject(t *testing.T) {
	ts := time.Now().UTC().Truncate(time.Microsecond)

	t.Run("within a batch", func(t *testing.T) {
		s, w := newWriter(t)
		stats, err := w.Write(t.Context(), telemetry.Batch{Spans: []telemetry.Span{
			span("p1", "1", "1", ts), span("p2", "1", "1", ts),
		}})
		require.NoError(t, err)
		assert.Equal(t, 2, stats.Rows)
		assert.Zero(t, stats.Duplicates)
		assert.Equal(t, 2, countRows(t, s, "spans"))
	})

	t.Run("across batches", func(t *testing.T) {
		s, w := newWriter(t)
		_, err := w.Write(t.Context(), telemetry.Batch{Spans: []telemetry.Span{span("p1", "1", "1", ts)}})
		require.NoError(t, err)

		stats, err := w.Write(t.Context(), telemetry.Batch{Spans: []telemetry.Span{span("p2", "1", "1", ts)}})
		require.NoError(t, err)
		assert.Equal(t, 1, stats.Rows)
		assert.Zero(t, stats.Duplicates)

		stats, err = w.Write(t.Context(), telemetry.Batch{Spans: []telemetry.Span{span("p2", "1", "1", ts)}})
		require.NoError(t, err)
		assert.Zero(t, stats.Rows)
		assert.Equal(t, 1, stats.Duplicates)
		assert.Equal(t, 2, countRows(t, s, "spans"))
	})
}
