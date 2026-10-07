package store_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abogoyavlensky/tracelet/spike/storage/internal/store"
	"github.com/abogoyavlensky/tracelet/spike/storage/internal/telemetry"
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

func countRows(t *testing.T, s *store.Store, table string) int {
	t.Helper()
	var n int
	require.NoError(t, s.DB().QueryRow("SELECT count(*) FROM "+table).Scan(&n))
	return n
}

func TestWriterWritesBatch(t *testing.T) {
	s := openStore(t)
	w, err := store.NewWriter(t.Context(), s)
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
	w, err := store.NewWriter(t.Context(), s)
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	stats, err := w.Write(t.Context(), telemetry.Batch{})
	require.NoError(t, err)
	assert.Equal(t, 0, stats.Rows)
	assert.Equal(t, 0, countRows(t, s, "logs"))
}

func TestWriterStampsIncrease(t *testing.T) {
	s := openStore(t)
	w, err := store.NewWriter(t.Context(), s)
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	// Back-to-back writes routinely land in the same microsecond.
	for range 20 {
		_, err := w.Write(t.Context(), telemetry.Batch{Logs: sampleBatch(time.Now().UTC()).Logs[:1]})
		require.NoError(t, err)
	}

	rows, err := s.DB().Query("SELECT ingest_ts FROM logs ORDER BY ingest_ts")
	require.NoError(t, err)
	defer rows.Close()
	var stamps []time.Time
	for rows.Next() {
		var ts time.Time
		require.NoError(t, rows.Scan(&ts))
		stamps = append(stamps, ts)
	}
	require.NoError(t, rows.Err())

	require.Len(t, stamps, 20)
	for i := 1; i < len(stamps); i++ {
		assert.True(t, stamps[i].After(stamps[i-1]), "stamp %d not after %d", i, i-1)
	}
}
