// Command tracelet is the Tracelet server and its CLI.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/abogoyavlensky/tracelet/internal/app"
	"github.com/abogoyavlensky/tracelet/internal/cli"
)

// version is set at build time: -ldflags "-X main.version=...".
var version = "dev"

func main() {
	args := os.Args[1:]
	if len(args) > 0 && !isServerCommand(args[0]) {
		os.Exit(cli.Run(args, os.Stdout, os.Stderr))
	}
	if err := run(args, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// isServerCommand reports whether cmd runs here rather than in the API
// client.
func isServerCommand(cmd string) bool {
	switch cmd {
	case "serve", "version", "help", "-h", "--help":
		return true
	}
	return false
}

// run dispatches the command so that deferred cleanup runs before exit.
func run(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stdout, cli.Usage())
		return errors.New("missing command")
	}

	switch args[0] {
	case "serve":
		return serve(args[1:])
	case "version":
		fmt.Fprintln(stdout, version)
		return nil
	case "help", "-h", "--help":
		fmt.Fprint(stdout, cli.Usage())
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

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
