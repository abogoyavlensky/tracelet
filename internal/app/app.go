// Package app is the composition root: it builds the dependency graph from
// Config and owns the lifecycle of every long-lived resource.
package app

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/abogoyavlensky/tracelet/internal/duckdb"
	"github.com/abogoyavlensky/tracelet/internal/flush"
	"github.com/abogoyavlensky/tracelet/internal/httpapi"
	"github.com/abogoyavlensky/tracelet/internal/ingest"
	"github.com/abogoyavlensky/tracelet/internal/manifest"
	"github.com/abogoyavlensky/tracelet/internal/project"
	"github.com/abogoyavlensky/tracelet/internal/query"
	"github.com/abogoyavlensky/tracelet/internal/sqlite"
	"github.com/abogoyavlensky/tracelet/internal/web"
)

// querySlots bounds concurrent queries; retention takes every slot.
const querySlots = 4

// App is the running system.
type App struct {
	cfg    Config
	logger *slog.Logger

	db           *sql.DB
	store        *duckdb.Store
	manifest     *manifest.Manifest
	flusher      *flush.Flusher
	gate         *query.Gate
	diskPressure *atomic.Bool
	status       *storageStatus
	writer       *duckdb.Writer
	ingester     *ingest.Ingester

	bootstrapToken string
	listener       net.Listener
	server         *http.Server
}

// New builds the dependency graph and binds the listen address. Read it top
// to bottom to see what depends on what.
func New(ctx context.Context, cfg Config, logger *slog.Logger) (*App, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	a := &App{cfg: cfg, logger: logger, diskPressure: &atomic.Bool{}}
	if err := a.build(ctx); err != nil {
		return nil, errors.Join(err, a.closeStorage())
	}
	return a, nil
}

func (a *App) build(ctx context.Context) error {
	if err := a.openStorage(ctx); err != nil {
		return err
	}

	// Stamps stay above every cold file's cutoff, even after a flush emptied hot.
	floor, err := a.manifest.MaxIngestTS(ctx)
	if err != nil {
		return err
	}
	a.writer, err = duckdb.NewWriter(ctx, a.store, floor)
	if err != nil {
		return err
	}
	a.ingester = ingest.New(a.writer, ingest.DefaultConfig)

	projects := project.NewService(sqlite.NewProjectStore(a.db), time.Now, rand.Reader)
	a.bootstrapToken, err = projects.Bootstrap(ctx, a.cfg.AdminToken)
	if err != nil {
		return err
	}

	a.status = &storageStatus{store: a.store, manifest: a.manifest, dataDir: a.cfg.DataDir, diskPressure: a.diskPressure}
	api := httpapi.NewHandler(httpapi.Deps{
		Info:         httpapi.Info{Version: a.cfg.Version},
		Auth:         projects,
		Projects:     projects,
		Ingester:     a.ingester,
		Queries:      query.Opener{Store: a.store, Manifest: a.manifest, Gate: a.gate, DataDir: a.cfg.DataDir},
		IngestStats:  a.ingester,
		StorageStats: a.status,
		Now:          time.Now,
		DiskPressure: a.diskPressure.Load,
		Logger:       a.logger,
	})

	assets, err := web.Assets()
	if err != nil {
		return fmt.Errorf("load frontend assets: %w", err)
	}
	mux := http.NewServeMux()
	mux.Handle("/api/", api)
	mux.Handle("/v1/", api)
	mux.Handle("/", web.NewHandler(assets))

	a.listener, err = net.Listen("tcp", a.cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	a.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return nil
}

// BootstrapToken is the admin token created on this start, or "" when one
// already existed or was seeded. It is shown once and never stored.
func (a *App) BootstrapToken() string { return a.bootstrapToken }

// Addr is the address the server listens on, with the real port when the
// configured one was 0.
func (a *App) Addr() string { return a.listener.Addr().String() }

// openStorage opens the application database and the hot store, then
// repairs any interrupted flush or retention and applies retention before
// anything else runs, so no query gate is needed yet.
func (a *App) openStorage(ctx context.Context) error {
	db, err := sqlite.Open(filepath.Join(a.cfg.DataDir, "tracelet.sqlite"))
	if err != nil {
		return err
	}
	a.db = db
	if _, err := sqlite.Migrate(ctx, db); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	store, err := duckdb.Open(a.cfg.DataDir, duckdb.Limits{MemoryLimit: a.cfg.DuckDBMemoryLimit, Threads: a.cfg.DuckDBThreads})
	if err != nil {
		return err
	}
	a.store = store
	a.manifest = manifest.New(db)
	a.flusher = &flush.Flusher{Store: store, Manifest: a.manifest, DataDir: a.cfg.DataDir}
	a.gate = query.NewGate(querySlots)

	rec, err := a.flusher.Reconcile(ctx)
	if err != nil {
		return fmt.Errorf("reconcile: %w", err)
	}
	ret, err := a.flusher.ApplyRetention(ctx, a.retentionPolicy(), time.Now())
	if err != nil {
		return fmt.Errorf("retention: %w", err)
	}
	free, err := sampleDisk(a.cfg.DataDir, a.cfg.DiskFloorBytes, a.diskPressure)
	if err != nil {
		return err
	}
	a.logger.InfoContext(ctx, "storage ready",
		"tmp_removed", rec.TmpRemoved, "adopted", rec.Adopted, "dropped", rec.Dropped,
		"hot_rows_deleted", rec.HotRowsDeleted, "retention_files", ret.FilesDeleted,
		"disk_free", free, "disk_pressure", a.diskPressure.Load())
	return nil
}

func (a *App) retentionPolicy() flush.Policy {
	days := map[string]int{}
	for _, signal := range duckdb.Signals {
		days[signal] = a.cfg.RetentionDays
	}
	return flush.Policy{Days: days, MaxBytes: a.cfg.MaxTelemetryBytes}
}

// Run serves until ctx is cancelled or the server fails, then shuts
// everything down: the HTTP server, then the ingester's queue, then the
// maintenance loop, then storage.
func (a *App) Run(ctx context.Context) error {
	maintCtx, stopMaintenance := context.WithCancel(ctx)
	defer stopMaintenance()

	var wg sync.WaitGroup
	// The ingester outlives ctx: its queue drains after the server stops.
	wg.Go(func() { a.ingester.Run(context.WithoutCancel(ctx)) })
	wg.Go(func() {
		runMaintenance(maintCtx, maintenance{
			flusher:      a.flusher,
			gate:         a.gate,
			policy:       a.retentionPolicy(),
			dataDir:      a.cfg.DataDir,
			diskFloor:    a.cfg.DiskFloorBytes,
			diskPressure: a.diskPressure,
			status:       a.status,
			logger:       a.logger,
		}, a.cfg.MaintenanceInterval, time.Now)
	})

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- a.server.Serve(a.listener)
	}()
	a.logger.InfoContext(ctx, "listening", "addr", a.Addr(), "data_dir", a.cfg.DataDir, "version", a.cfg.Version)

	var runErr error
	select {
	case <-ctx.Done():
	case err := <-serveErr:
		runErr = fmt.Errorf("serve http: %w", err)
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), a.cfg.ShutdownTimeout)
	defer cancelShutdown()

	httpErr := a.server.Shutdown(shutdownCtx)
	if httpErr != nil {
		httpErr = fmt.Errorf("shutdown http: %w", httpErr)
	}
	a.ingester.Close()
	stopMaintenance()
	wg.Wait()

	return errors.Join(runErr, httpErr, a.closeStorage())
}

// closeStorage releases storage in reverse order of construction. It is safe
// on a partly built App.
func (a *App) closeStorage() error {
	var errs []error
	if a.writer != nil {
		errs = append(errs, a.writer.Close())
	}
	if a.store != nil {
		errs = append(errs, a.store.Close())
	}
	if a.db != nil {
		errs = append(errs, a.db.Close())
	}
	return errors.Join(errs...)
}
