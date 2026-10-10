package app

import (
	"io"
	"log/slog"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abogoyavlensky/tracelet/internal/duckdb"
	"github.com/abogoyavlensky/tracelet/internal/flush"
	"github.com/abogoyavlensky/tracelet/internal/manifest"
	"github.com/abogoyavlensky/tracelet/internal/query"
	"github.com/abogoyavlensky/tracelet/internal/sqlite"
	"github.com/abogoyavlensky/tracelet/internal/telemetry"
)

func TestMaintenanceFlushesClosedHours(t *testing.T) {
	dir := t.TempDir()
	store, err := duckdb.Open(dir, duckdb.Limits{MemoryLimit: "256MB", Threads: 1})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	db, err := sqlite.Open(filepath.Join(dir, "tracelet.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = sqlite.Migrate(t.Context(), db)
	require.NoError(t, err)
	w, err := duckdb.NewWriter(t.Context(), store, time.Time{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	h0 := time.Now().UTC().Truncate(time.Hour)
	_, err = w.Write(t.Context(), telemetry.Batch{Logs: []telemetry.Log{
		{TS: h0.Add(time.Minute), Project: "p1", Service: "api", Body: "a"},
		{TS: h0.Add(time.Hour + time.Minute), Project: "p1", Service: "api", Body: "b"},
	}})
	require.NoError(t, err)

	m := manifest.New(db)
	pressure := &atomic.Bool{}
	deps := maintenance{
		flusher:      &flush.Flusher{Store: store, Manifest: m, DataDir: dir},
		gate:         query.NewGate(4),
		policy:       flush.Policy{Days: map[string]int{"logs": 30}},
		dataDir:      dir,
		diskFloor:    1 << 62, // more than any disk has, so pressure is set
		diskPressure: pressure,
		status:       &storageStatus{},
		logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	// The clock has jumped three hours: hour 0 is due, hour 1 is not yet.
	now := func() time.Time { return h0.Add(2*time.Hour + 30*time.Minute) }
	done := make(chan struct{})
	go func() {
		defer close(done)
		runMaintenance(t.Context(), deps, 10*time.Millisecond, now)
	}()
	// Cleanups run last-in first-out, so the loop stops before storage closes.
	t.Cleanup(func() { <-done })

	require.Eventually(t, func() bool {
		hours, err := store.HotHours(t.Context(), "logs")
		return err == nil && len(hours) == 1 && hours[0].Equal(h0.Add(time.Hour))
	}, 5*time.Second, 10*time.Millisecond)
	files, err := m.All(t.Context())
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, h0, files[0].Hour)
	require.Eventually(t, pressure.Load, time.Second, 10*time.Millisecond)
}
