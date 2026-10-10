package flush_test

import (
	"database/sql"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/abogoyavlensky/tracelet/internal/duckdb"
	"github.com/abogoyavlensky/tracelet/internal/flush"
	"github.com/abogoyavlensky/tracelet/internal/manifest"
	"github.com/abogoyavlensky/tracelet/internal/sqlite"
	"github.com/abogoyavlensky/tracelet/internal/telemetry"
)

// start is the first hour of every test's data: an hour boundary.
var start = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

// env is one data dir with everything open on it.
type env struct {
	DataDir  string
	Store    *duckdb.Store
	DB       *sql.DB
	Manifest *manifest.Manifest
	Writer   *duckdb.Writer
	Flusher  *flush.Flusher
}

// counts is rows per signal.
type counts map[string]int64

func newEnv(t *testing.T) *env {
	t.Helper()
	return openEnv(t, t.TempDir())
}

// openEnv opens an environment on an existing data dir, closing it at cleanup.
func openEnv(t *testing.T, dataDir string) *env {
	t.Helper()
	s, err := duckdb.Open(dataDir, duckdb.Limits{MemoryLimit: "256MB", Threads: 2})
	require.NoError(t, err)
	db, err := sqlite.Open(filepath.Join(dataDir, "tracelet.sqlite"))
	require.NoError(t, err)
	_, err = sqlite.Migrate(t.Context(), db)
	require.NoError(t, err)
	w, err := duckdb.NewWriter(t.Context(), s)
	require.NoError(t, err)

	m := manifest.New(db)
	e := &env{
		DataDir:  dataDir,
		Store:    s,
		DB:       db,
		Manifest: m,
		Writer:   w,
		Flusher:  &flush.Flusher{Store: s, Manifest: m, DataDir: dataDir},
	}
	t.Cleanup(func() { e.close() })
	return e
}

// close closes everything; safe to call twice.
func (e *env) close() {
	if e.Writer != nil {
		_ = e.Writer.Close()
		e.Writer = nil
	}
	if e.DB != nil {
		_ = e.DB.Close()
		e.DB = nil
	}
	if e.Store != nil {
		_ = e.Store.Close()
		e.Store = nil
	}
}

// ingest3h writes three hours of hand-built rows from start: per minute two
// logs, one span, and one metric point, committed every ten minutes.
func ingest3h(t *testing.T) (*env, counts) {
	t.Helper()
	e := newEnv(t)
	c := counts{}
	var batch telemetry.Batch
	var seq int64
	for minute := range 180 {
		ts := start.Add(time.Duration(minute) * time.Minute)
		for i := range 2 {
			batch.Logs = append(batch.Logs, logAt(ts.Add(time.Duration(i)*time.Second), seq))
			seq++
		}
		batch.Spans = append(batch.Spans, telemetry.Span{
			TS: ts, EndTS: ts.Add(time.Millisecond), Project: "p1", Service: "api",
			TraceID: fmt.Sprintf("%032x", minute), SpanID: fmt.Sprintf("%016x", minute),
			Name: "GET /", Kind: 2, DurationNS: int64(time.Millisecond),
		})
		value := int64(minute)
		batch.Points = append(batch.Points, telemetry.MetricPoint{
			TS: ts, Project: "p1", Service: "api", Name: "jobs", Type: telemetry.TypeGauge,
			SeriesHash: 1, ValueInt: &value,
		})
		if minute%10 == 9 {
			e.write(t, batch, c)
			batch = telemetry.Batch{}
		}
	}
	return e, c
}

// logAt returns one log for ts; seq identifies it across hot and cold.
func logAt(ts time.Time, seq int64) telemetry.Log {
	return telemetry.Log{
		TS: ts, Project: "p1", Service: "api", Environment: "prod",
		SeverityNumber: 17, SeverityText: "ERROR", Body: "boom",
		Attributes: fmt.Sprintf(`{"seq":%d}`, seq),
	}
}

// write writes one batch and adds it to c.
func (e *env) write(t *testing.T, b telemetry.Batch, c counts) {
	t.Helper()
	_, err := e.Writer.Write(t.Context(), b)
	require.NoError(t, err)
	c["logs"] += int64(len(b.Logs))
	c["spans"] += int64(len(b.Spans))
	c["metric_points"] += int64(len(b.Points))
}

// hotCount counts a signal's hot rows in [from, to).
func (e *env) hotCount(t *testing.T, signal string, from, to time.Time) int64 {
	t.Helper()
	var n int64
	err := e.Store.DB().QueryRow("SELECT count(*) FROM "+signal+" WHERE ts >= ? AND ts < ?", from, to).Scan(&n)
	require.NoError(t, err)
	return n
}

// signalCounts is one signal's row accounting across hot and cold.
type signalCounts struct {
	Hot, Cold, Total, DistinctKeys int64
}

// rowKeys identifies a row uniquely across one test's data.
var rowKeys = map[string]string{
	"logs":          "json_extract(attributes, '$.seq')",
	"spans":         "(trace_id, span_id)",
	"metric_points": "(series_hash, ts)",
}

// accounting counts every signal's rows in hot and in the recorded files that
// exist, with their distinct keys, and lists recorded files missing from disk
// and files on disk missing from the manifest.
func (e *env) accounting(t *testing.T) (bySignal map[string]signalCounts, missing, unrecorded []string) {
	t.Helper()
	files, err := e.Manifest.All(t.Context())
	require.NoError(t, err)
	onDisk := filesWithSuffix(t, e.DataDir, ".parquet")

	present := map[string][]string{}
	var recorded []string
	for _, f := range files {
		recorded = append(recorded, f.Path)
		if slices.Contains(onDisk, f.Path) {
			present[f.Signal] = append(present[f.Signal], "'"+filepath.Join(e.DataDir, f.Path)+"'")
		}
	}
	for _, p := range recorded {
		if !slices.Contains(onDisk, p) {
			missing = append(missing, p)
		}
	}
	for _, p := range onDisk {
		if !slices.Contains(recorded, p) {
			unrecorded = append(unrecorded, p)
		}
	}

	bySignal = map[string]signalCounts{}
	for _, signal := range duckdb.Signals {
		cold := "SELECT * FROM " + signal + " WHERE false"
		if paths := present[signal]; len(paths) > 0 {
			cold = "SELECT * FROM read_parquet([" + strings.Join(paths, ", ") + "])"
		}
		q := fmt.Sprintf(`WITH hot AS (SELECT * FROM %[1]s), cold AS (%[2]s),
			everything AS (SELECT %[3]s AS k FROM hot UNION ALL SELECT %[3]s AS k FROM cold)
			SELECT (SELECT count(*) FROM hot), (SELECT count(*) FROM cold), count(*), count(DISTINCT k) FROM everything`,
			signal, cold, rowKeys[signal])
		var c signalCounts
		require.NoError(t, e.Store.DB().QueryRow(q).Scan(&c.Hot, &c.Cold, &c.Total, &c.DistinctKeys))
		bySignal[signal] = c
	}
	return bySignal, missing, unrecorded
}

// filesWithSuffix lists files under telemetry/ ending in suffix, as
// slash-separated paths relative to the data dir.
func filesWithSuffix(t *testing.T, dataDir, suffix string) []string {
	t.Helper()
	var out []string
	root := filepath.Join(dataDir, flush.TelemetryDir)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return filepath.SkipDir
			}
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, suffix) {
			return nil
		}
		rel, err := filepath.Rel(dataDir, path)
		out = append(out, filepath.ToSlash(rel))
		return err
	})
	require.NoError(t, err)
	return out
}
