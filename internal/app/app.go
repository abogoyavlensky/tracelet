// Package app is the composition root: it builds the dependency graph from
// Config and owns the lifecycle of every long-lived resource.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/abogoyavlensky/tracelet/internal/httpapi"
	"github.com/abogoyavlensky/tracelet/internal/web"
)

// App is the running system.
type App struct {
	cfg    Config
	logger *slog.Logger
	server *http.Server
}

// New builds the dependency graph. Read it top to bottom to see what
// depends on what.
func New(cfg Config, logger *slog.Logger) (*App, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	assets, err := web.Assets()
	if err != nil {
		return nil, fmt.Errorf("load frontend assets: %w", err)
	}

	api := httpapi.NewHandler(httpapi.Info{Version: cfg.Version})

	mux := http.NewServeMux()
	mux.Handle("/api/", api)
	mux.Handle("/", web.NewHandler(assets))

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	return &App{cfg: cfg, logger: logger, server: server}, nil
}

// Run serves until ctx is cancelled or the server fails, then shuts
// everything down.
func (a *App) Run(ctx context.Context) error {
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

	shutdownCtx, cancel := context.WithTimeout(context.Background(), a.cfg.ShutdownTimeout)
	defer cancel()

	return errors.Join(runErr, a.close(shutdownCtx))
}

// close releases resources in reverse order of construction.
func (a *App) close(ctx context.Context) error {
	if err := a.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown http: %w", err)
	}
	return nil
}
