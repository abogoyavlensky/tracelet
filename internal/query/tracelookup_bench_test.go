package query_test

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"math/rand/v2"
	"os"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abogoyavlensky/tracelet/internal/query"
)

// Shape of the benchmark's data: the busy workload's span rate, five spans
// per trace.
const (
	benchSpansPerHour = 180000
	benchTraceSpans   = 5
	benchLookups      = 100
)

// benchInsert generates one hour of spans directly in the hot table. Hour k's
// trace i/5 is md5('t<k>-<i/5>'), so a lookup can name one without reading
// the data back.
const benchInsert = `INSERT INTO spans
SELECT
  ?::TIMESTAMP + to_microseconds(i * 20000) AS ts,
  ?::TIMESTAMP + to_microseconds(i * 20000 + 50000) AS end_ts,
  ?::TIMESTAMP + to_microseconds(i) AS ingest_ts,
  'p1', 'svc' || (i % 10), 'prod', '1.0.0',
  md5('t' || ? || '-' || (i // 5)),
  left(md5('s' || ? || '-' || i), 16),
  CASE WHEN i % 5 = 0 THEN NULL ELSE left(md5('s' || ? || '-' || (i // 5 * 5)), 16) END,
  'GET /x', 2, 1, NULL, 50000000, 'http',
  '{"service.name":"svc","host.name":"box"}', '{"http.route":"/x","http.status_code":200}',
  NULL, NULL
FROM range(?) t(i)`

// TestTraceLookupSevenDays measures trace lookup by ID over seven days of
// span files written by the real flush. It is opt-in: TRACELET_BENCH=1, and
// TRACELET_BENCH_HOURS to change the 168 hours. Point TMPDIR at a real disk;
// the files take under 1 GB. Results: docs/spikes/trace-lookup.md.
func TestTraceLookupSevenDays(t *testing.T) {
	if os.Getenv("TRACELET_BENCH") != "1" {
		t.Skip("set TRACELET_BENCH=1 to run the trace lookup benchmark")
	}
	hours := 168
	if s := os.Getenv("TRACELET_BENCH_HOURS"); s != "" {
		n, err := strconv.Atoi(s)
		require.NoError(t, err)
		hours = n
	}

	e := newEnv(t)
	ctx := t.Context()
	now := time.Now().UTC()
	first := now.Truncate(time.Hour).Add(-time.Duration(hours+1) * time.Hour)
	ingestBase := now

	flushes := make([]time.Duration, 0, hours)
	for k := range hours {
		hour := first.Add(time.Duration(k) * time.Hour)
		ingest := ingestBase.Add(time.Duration(k*benchSpansPerHour) * time.Microsecond)
		_, err := e.store.DB().ExecContext(ctx, benchInsert, hour, hour, ingest, k, k, k, benchSpansPerHour)
		require.NoError(t, err)

		start := time.Now()
		stats, err := e.flusher.FlushAll(ctx)
		require.NoError(t, err)
		flushes = append(flushes, time.Since(start))
		require.Len(t, stats, 1)
		require.Equal(t, int64(benchSpansPerHour), stats[0].Rows)
	}

	files, err := e.manifest.All(ctx)
	require.NoError(t, err)
	var bytes int64
	for _, f := range files {
		bytes += f.Bytes
	}

	rng := rand.New(rand.NewPCG(7, 11))
	rangeAll := query.Range{From: first, To: now}
	lookups := make([]time.Duration, 0, benchLookups)
	for range benchLookups {
		k, i := rng.IntN(hours), rng.IntN(benchSpansPerHour)
		sum := md5.Sum(fmt.Appendf(nil, "t%d-%d", k, i/benchTraceSpans))
		traceID := hex.EncodeToString(sum[:])

		start := time.Now()
		snap, err := query.Open(ctx, e.store, e.manifest, e.gate, e.dataDir)
		require.NoError(t, err)
		tr, err := query.TraceGet(ctx, snap, query.TraceParams{ProjectID: "p1", TraceID: traceID, Range: rangeAll})
		snap.Close()
		lookups = append(lookups, time.Since(start))
		require.NoError(t, err)
		assert.Len(t, tr.Spans, benchTraceSpans, traceID)
	}

	t.Logf("hours %d, files %d, bytes %d (%.1f MB per file)", hours, len(files), bytes, float64(bytes)/float64(len(files))/1e6)
	t.Logf("flush per hour: p50 %s, p95 %s, max %s", pct(flushes, 50), pct(flushes, 95), pct(flushes, 100))
	t.Logf("lookup: p50 %s, p95 %s, max %s", pct(lookups, 50), pct(lookups, 95), pct(lookups, 100))
	assert.Less(t, pct(lookups, 95), time.Second, "p95 trace lookup over %d hours", hours)
}

// pct is the nearest-rank percentile p of ds.
func pct(ds []time.Duration, p int) time.Duration {
	s := slices.Clone(ds)
	slices.Sort(s)
	i := (len(s)*p+99)/100 - 1
	return s[max(i, 0)].Round(time.Millisecond)
}
