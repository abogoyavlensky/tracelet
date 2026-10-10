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
			TraceID: "0123456789abcdef0123456789abcdef", SpanID: fmt.Sprintf("000000000000000%d", i),
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
