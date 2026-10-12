//go:build !cli

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/abogoyavlensky/tracelet/internal/app"
)

func serve(args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := app.ParseServeConfig(args, version)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	a, err := app.New(ctx, cfg, logger)
	if err != nil {
		return err
	}
	if token := a.BootstrapToken(); token != "" {
		fmt.Fprintf(os.Stderr, "admin token: %s\nSave it now: it will not be shown again.\n", token)
	}

	return a.Run(ctx)
}
