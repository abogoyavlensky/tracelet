package backup_test

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abogoyavlensky/tracelet/spike/storage/internal/backup"
	"github.com/abogoyavlensky/tracelet/spike/storage/internal/gen"
	"github.com/abogoyavlensky/tracelet/spike/storage/internal/query"
	"github.com/abogoyavlensky/tracelet/spike/storage/internal/testenv"
)

func runSuite(t *testing.T, e *testenv.Env, p query.SuiteParams) map[string]any {
	t.Helper()
	r := query.Reader{DB: e.Store.DB(), Manifest: e.Manifest, DataDir: e.DataDir}
	out := map[string]any{}
	for _, q := range query.Suite(p) {
		res, err := q.Run(t.Context(), r)
		require.NoError(t, err, q.Name)
		out[q.Name] = res
	}
	return out
}

func TestBackupRestoresEquivalentData(t *testing.T) {
	t.Parallel()
	e := testenv.New(t)
	ctx := t.Context()
	_, batches := e.IngestBatches(t, gen.NewGenerator(gen.Small, 1, 0), 3*time.Hour)
	_, err := e.Flusher.FlushDue(ctx, testenv.Start.Add(2*time.Hour))
	require.NoError(t, err)

	p := query.SuiteParams{
		Now:     testenv.Start.Add(3 * time.Hour),
		TraceID: batches[0].Spans[0].TraceID,
		Service: "svc-01",
		Route:   "/api/users",
	}
	before := runSuite(t, e, p)
	for name, res := range before {
		assert.NotEmpty(t, res, "%s returns something to compare", name)
	}

	out := filepath.Join(t.TempDir(), "backup")
	rep, err := backup.Backup(ctx, &sync.Mutex{}, e.Flusher, e.Manifest, e.DataDir, out)
	require.NoError(t, err)
	assert.Positive(t, rep.Files)
	assert.Positive(t, rep.FlushedRows, "the hot hours were flushed into the backup")

	assert.FileExists(t, filepath.Join(out, backup.ManifestFile))
	assert.DirExists(t, filepath.Join(out, "telemetry"))
	assert.NoFileExists(t, filepath.Join(out, "hot.duckdb"))

	restored := testenv.Open(t, out)
	after := runSuite(t, restored, p)
	for name := range before {
		assert.Equal(t, before[name], after[name], name)
	}
}

func TestBackupRefusesNonEmptyDir(t *testing.T) {
	t.Parallel()
	e := testenv.New(t)
	out := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(out, "x"), nil, 0o644))

	_, err := backup.Backup(t.Context(), &sync.Mutex{}, e.Flusher, e.Manifest, e.DataDir, out)
	assert.ErrorContains(t, err, "not empty")
}
