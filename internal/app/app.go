// Package app is the composition root: it builds the dependency graph from
// Config and owns the lifecycle of every long-lived resource.
package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/abogoyavlensky/tracelet/internal/duckdb"
	"github.com/abogoyavlensky/tracelet/internal/flush"
	"github.com/abogoyavlensky/tracelet/internal/httpapi"
	"github.com/abogoyavlensky/tracelet/internal/manifest"
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

	server *http.Server
}

// New builds the dependency graph. Read it top to bottom to see what
// depends on what.
func New(ctx context.Context, cfg Config, logger *slog.Logger) (*App, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	a := &App{cfg: cfg, logger: logger, diskPressure: &atomic.Bool{}}
	if err := a.openStorage(ctx); err != nil {
		return nil, errors.Join(err, a.closeStorage())
	}

	assets, err := web.Assets()
	if err != nil {
		return nil, errors.Join(fmt.Errorf("load frontend assets: %w", err), a.closeStorage())
	}

	api := httpapi.NewHandler(httpapi.Info{Version: cfg.Version})

	mux := http.NewServeMux()
	mux.Handle("/api/", api)
	mux.Handle("/", web.NewHandler(assets))

	a.server = &http.Server{
		Addr:              cfg.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return a, nil
}

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
// everything down.
func (a *App) Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	wg.Go(func() {
		runMaintenance(runCtx, maintenance{
			flusher:      a.flusher,
			gate:         a.gate,
			policy:       a.retentionPolicy(),
			dataDir:      a.cfg.DataDir,
			diskFloor:    a.cfg.DiskFloorBytes,
			diskPressure: a.diskPressure,
			logger:       a.logger,
		}, a.cfg.MaintenanceInterval, time.Now)
	})

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- a.server.ListenAndServe()
	}()
	a.logger.InfoContext(ctx, "listening", "addr", a.cfg.Addr, "data_dir", a.cfg.DataDir, "version", a.cfg.Version)

	var runErr error
	select {
	case <-ctx.Done():
	case err := <-serveErr:
		runErr = fmt.Errorf("serve http: %w", err)
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), a.cfg.ShutdownTimeout)
	defer cancelShutdown()

	// Stop taking requests, then stop maintenance, then close storage.
	httpErr := a.server.Shutdown(shutdownCtx)
	if httpErr != nil {
		httpErr = fmt.Errorf("shutdown http: %w", httpErr)
	}
	cancel()
	wg.Wait()

	return errors.Join(runErr, httpErr, a.closeStorage())
}

// closeStorage releases storage in reverse order of construction. It is safe
// on a partly built App.
func (a *App) closeStorage() error {
	var errs []error
	if a.store != nil {
		errs = append(errs, a.store.Close())
	}
	if a.db != nil {
		errs = append(errs, a.db.Close())
	}
	return errors.Join(errs...)
}
