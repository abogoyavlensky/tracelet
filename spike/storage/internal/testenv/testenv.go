// Package testenv builds the store, manifest, writer, and flusher that the
// spike's package tests share, and ingests generated telemetry into them.
package testenv

import (
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/abogoyavlensky/tracelet/spike/storage/internal/flush"
	"github.com/abogoyavlensky/tracelet/spike/storage/internal/gen"
	"github.com/abogoyavlensky/tracelet/spike/storage/internal/manifest"
	"github.com/abogoyavlensky/tracelet/spike/storage/internal/store"
	"github.com/abogoyavlensky/tracelet/spike/storage/internal/telemetry"
)

// Start is the simulated start of every test run: an hour boundary.
var Start = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

// Env is one data dir with everything open on it.
type Env struct {
	DataDir  string
	Store    *store.Store
	Manifest *manifest.Manifest
	Writer   *store.Writer
	Flusher  *flush.Flusher
}

// Counts is rows per signal.
type Counts map[string]int64

// New opens a fresh environment in a temp dir.
func New(t *testing.T) *Env {
	t.Helper()
	return Open(t, t.TempDir())
}

// Open opens an environment on an existing data dir, closing it at cleanup.
func Open(t *testing.T, dataDir string) *Env {
	t.Helper()
	s, err := store.Open(dataDir, store.Limits{MemoryLimit: "256MB", Threads: 2})
	require.NoError(t, err)
	m, err := manifest.Open(filepath.Join(dataDir, "manifest.sqlite"))
	require.NoError(t, err)
	w, err := store.NewWriter(t.Context(), s)
	require.NoError(t, err)

	e := &Env{
		DataDir:  dataDir,
		Store:    s,
		Manifest: m,
		Writer:   w,
		Flusher:  &flush.Flusher{Store: s, Manifest: m, DataDir: dataDir},
	}
	t.Cleanup(func() { e.Close() })
	return e
}

// Close closes everything; safe to call twice.
func (e *Env) Close() {
	if e.Writer != nil {
		_ = e.Writer.Close()
		e.Writer = nil
	}
	if e.Manifest != nil {
		_ = e.Manifest.Close()
		e.Manifest = nil
	}
	if e.Store != nil {
		_ = e.Store.Close()
		e.Store = nil
	}
}

// Ingest writes dur of generated telemetry starting at Start, one write per
// simulated minute, and returns the rows written per signal. Late logs that
// would land before Start are dropped so tests can reason about exact hours.
func (e *Env) Ingest(t *testing.T, g *gen.Generator, dur time.Duration) Counts {
	t.Helper()
	counts := Counts{}
	var batch telemetry.Batch
	flushBatch := func() {
		if batch.Len() == 0 {
			return
		}
		_, err := e.Writer.Write(t.Context(), batch)
		require.NoError(t, err)
		counts["logs"] += int64(len(batch.Logs))
		counts["spans"] += int64(len(batch.Spans))
		counts["metric_points"] += int64(len(batch.Points))
		batch = telemetry.Batch{}
	}
	for sec := range int(dur / time.Second) {
		b := g.Next(Start.Add(time.Duration(sec) * time.Second))
		for _, l := range b.Logs {
			if !l.TS.Before(Start) {
				batch.Logs = append(batch.Logs, l)
			}
		}
		batch.Spans = append(batch.Spans, b.Spans...)
		batch.Points = append(batch.Points, b.Points...)
		if sec%60 == 59 {
			flushBatch()
		}
	}
	flushBatch()
	return counts
}

// Write writes one batch and adds it to counts.
func (e *Env) Write(t *testing.T, b telemetry.Batch, counts Counts) {
	t.Helper()
	_, err := e.Writer.Write(t.Context(), b)
	require.NoError(t, err)
	counts["logs"] += int64(len(b.Logs))
	counts["spans"] += int64(len(b.Spans))
	counts["metric_points"] += int64(len(b.Points))
}

// HotCount counts a signal's hot rows in [from, to).
func (e *Env) HotCount(t *testing.T, signal string, from, to time.Time) int64 {
	t.Helper()
	var n int64
	err := e.Store.DB().QueryRow("SELECT count(*) FROM "+signal+" WHERE ts >= ? AND ts < ?", from, to).Scan(&n)
	require.NoError(t, err)
	return n
}

// LateLog returns one log for the given time with a seq beyond any the
// generator will produce.
func LateLog(ts time.Time, seq int64) telemetry.Log {
	return telemetry.Log{
		TS: ts, Project: "default", Service: "svc-01", Environment: "prod",
		SeverityNumber: 17, SeverityText: "ERROR", Body: "late",
		Attributes: `{"seq":` + strconv.FormatInt(seq, 10) + `,"http.route":"/late"}`,
	}
}
