// Command spike runs the storage spike scenarios and prints JSON reports.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/abogoyavlensky/tracelet/spike/storage/internal/backup"
	"github.com/abogoyavlensky/tracelet/spike/storage/internal/flush"
	"github.com/abogoyavlensky/tracelet/spike/storage/internal/gen"
	"github.com/abogoyavlensky/tracelet/spike/storage/internal/manifest"
	"github.com/abogoyavlensky/tracelet/spike/storage/internal/query"
	"github.com/abogoyavlensky/tracelet/spike/storage/internal/report"
	"github.com/abogoyavlensky/tracelet/spike/storage/internal/scenario"
	"github.com/abogoyavlensky/tracelet/spike/storage/internal/store"
)

// version is set at build time: -ldflags "-X main.version=...".
var version = "dev"

const usage = `Usage: spike <command> [flags]

Commands:
  run         Generate telemetry into a data dir and print a report
  reconcile   Repair a data dir after a crash and print what was repaired
  verify      Check a data dir against its expected row counts
  lookup      Time trace lookups by ID over the full window
  backup      Back up a data dir
  version     Print the version

Run "spike <command> -h" for the command's flags.
`

// crashExitCode is what --crash-at exits with, like a SIGKILLed process.
const crashExitCode = 137

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run dispatches the command so that deferred cleanup runs before exit.
func run(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stdout, usage)
		return errors.New("missing command")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch args[0] {
	case "run":
		err = runCmd(ctx, args[1:], stdout)
	case "reconcile":
		err = reconcileCmd(ctx, args[1:], stdout)
	case "verify":
		err = verifyCmd(ctx, args[1:], stdout)
	case "lookup":
		err = lookupCmd(ctx, args[1:], stdout)
	case "backup":
		err = backupCmd(ctx, args[1:], stdout)
	case "version":
		fmt.Fprintln(stdout, version)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
	default:
		err = fmt.Errorf("unknown command %q", args[0])
	}
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	return err
}

// system is everything open on one data dir.
type system struct {
	dataDir  string
	store    *store.Store
	manifest *manifest.Manifest
	writer   *store.Writer
	flusher  *flush.Flusher
	lock     *sync.Mutex
}

func openSystem(ctx context.Context, dataDir string, limits store.Limits) (*system, error) {
	s, err := store.Open(dataDir, limits)
	if err != nil {
		return nil, err
	}
	m, err := manifest.Open(filepath.Join(dataDir, backup.ManifestFile))
	if err != nil {
		_ = s.Close()
		return nil, err
	}
	w, err := store.NewWriter(ctx, s)
	if err != nil {
		_ = m.Close()
		_ = s.Close()
		return nil, err
	}
	return &system{
		dataDir:  dataDir,
		store:    s,
		manifest: m,
		writer:   w,
		flusher:  &flush.Flusher{Store: s, Manifest: m, DataDir: dataDir},
		lock:     &sync.Mutex{},
	}, nil
}

// Close closes everything in reverse order of opening.
func (s *system) Close() error {
	return errors.Join(s.writer.Close(), s.manifest.Close(), s.store.Close())
}

func (s *system) reader() query.Reader {
	return query.Reader{DB: s.store.DB(), Manifest: s.manifest, DataDir: s.dataDir}
}

// commonFlags registers the flags every data-dir command shares.
func commonFlags(fs *flag.FlagSet) (dataDir *string, limits func() store.Limits) {
	dataDir = fs.String("data-dir", ".tmp/spike", "data directory")
	memory := fs.String("memory-limit", "256MB", "DuckDB memory_limit")
	threads := fs.Int("threads", 2, "DuckDB threads")
	flushThreshold := fs.String("allocator-flush-threshold", "", "DuckDB allocator_flush_threshold; empty keeps the default")
	backgroundThreads := fs.Bool("allocator-background-threads", false, "turn on DuckDB allocator_background_threads")
	return dataDir, func() store.Limits {
		return store.Limits{
			MemoryLimit:                *memory,
			Threads:                    *threads,
			AllocatorFlushThreshold:    *flushThreshold,
			AllocatorBackgroundThreads: *backgroundThreads,
		}
	}
}

func runCmd(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	dataDir, limits := commonFlags(fs)
	profile := fs.String("profile", "small", "workload profile: small or busy")
	days := fs.Int("days", 0, "simulated days (accelerated mode)")
	hours := fs.Int("hours", 0, "simulated hours, added to --days (accelerated mode)")
	minutes := fs.Int("minutes", 5, "wall-clock minutes (real-time mode)")
	realTime := fs.Bool("real-time", false, "drive seconds from the wall clock")
	queryLoad := fs.Int("query-load", 0, "concurrent query goroutines (real-time mode)")
	retentionDays := fs.Int("retention-days", -1, "keep hours ending within this many days, every signal; -1 keeps all")
	maxBytes := fs.Int64("max-bytes", 0, "cap on cold file bytes; 0 means no cap")
	seed := fs.Uint64("seed", 1, "generator seed")
	crashAt := fs.String("crash-at", "", "exit 137 at this flush or retention step")
	batchSeconds := fs.Int("batch-seconds", 60, "simulated seconds per commit (accelerated mode)")
	rowGroupSize := fs.Int("row-group-size", 0, "Parquet ROW_GROUP_SIZE of flushed files; 0 keeps the default")
	unsorted := fs.Bool("unsorted-flush", false, "flush without ORDER BY ts (diagnostic only)")
	memInterval := fs.Duration("memory-sample-interval", time.Second, "wall-clock time between memory samples")
	if err := fs.Parse(args); err != nil {
		return err
	}

	p, err := gen.ProfileByName(*profile)
	if err != nil {
		return err
	}
	policy := flush.Policy{MaxBytes: *maxBytes}
	if *retentionDays >= 0 {
		policy.Days = map[string]int{}
		for _, signal := range store.Signals {
			policy.Days[signal] = *retentionDays
		}
	}

	sys, err := openSystem(ctx, *dataDir, limits())
	if err != nil {
		return err
	}
	defer sys.Close()
	sys.flusher.RowGroupSize = *rowGroupSize
	sys.flusher.Unsorted = *unsorted

	// Startup: reconcile, then retention, before any new data.
	repaired, err := sys.flusher.Reconcile(ctx)
	if err != nil {
		return fmt.Errorf("startup reconcile: %w", err)
	}
	retained, err := sys.flusher.ApplyRetention(ctx, policy, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("startup retention: %w", err)
	}

	if *crashAt != "" {
		at := *crashAt
		sys.flusher.Hook = func(step string) error {
			if step == at {
				os.Exit(crashExitCode)
			}
			return nil
		}
	}

	rep, err := scenario.Run(ctx, scenario.RunConfig{
		Profile:   p,
		Days:      *days,
		Hours:     *hours,
		Minutes:   *minutes,
		RealTime:  *realTime,
		Retention: policy,
		QueryLoad: *queryLoad,
		Seed:      *seed,

		BatchSeconds:         *batchSeconds,
		MemorySampleInterval: *memInterval,
	}, scenario.Deps{
		Store:    sys.store,
		Writer:   sys.writer,
		Manifest: sys.manifest,
		Flusher:  sys.flusher,
		Lock:     sys.lock,
		DataDir:  sys.dataDir,
	})
	if err != nil {
		return err
	}
	rep.StartupRepairs, rep.StartupRetained = repaired, retained
	return rep.WriteJSON(stdout)
}

func reconcileCmd(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("reconcile", flag.ContinueOnError)
	dataDir, limits := commonFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	sys, err := openSystem(ctx, *dataDir, limits())
	if err != nil {
		return err
	}
	defer sys.Close()

	repaired, err := sys.flusher.Reconcile(ctx)
	if err != nil {
		return err
	}
	verified, err := sys.flusher.Verify(ctx)
	if err != nil {
		return err
	}
	return report.WriteJSON(stdout, map[string]any{"reconcile": repaired, "verify": verified})
}

func verifyCmd(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	dataDir, limits := commonFlags(fs)
	expectedPath := fs.String("expected", "", "expected row counts (default <data-dir>/expected.json)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *expectedPath == "" {
		*expectedPath = filepath.Join(*dataDir, scenario.ExpectedFile)
	}

	exp, err := scenario.ReadExpected(*expectedPath)
	if err != nil {
		return err
	}
	if exp.Runs == 0 {
		return fmt.Errorf("no expected counts at %s", *expectedPath)
	}

	sys, err := openSystem(ctx, *dataDir, limits())
	if err != nil {
		return err
	}
	defer sys.Close()

	rep, err := sys.flusher.Verify(ctx)
	if err != nil {
		return err
	}
	if err := report.WriteJSON(stdout, map[string]any{"expected": exp.Rows, "verify": rep}); err != nil {
		return err
	}

	var problems []error
	for _, signal := range store.Signals {
		c, want := rep.Signals[signal], exp.Rows[signal]
		if c.Total != want {
			problems = append(problems, fmt.Errorf("%s: %d rows, expected %d", signal, c.Total, want))
		}
		if c.DistinctKeys != want {
			problems = append(problems, fmt.Errorf("%s: %d distinct keys, expected %d", signal, c.DistinctKeys, want))
		}
	}
	if !rep.PathsMatch {
		problems = append(problems, fmt.Errorf("manifest and disk differ: missing %v, unrecorded %v", rep.Missing, rep.Unrecorded))
	}
	return errors.Join(problems...)
}

func lookupCmd(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("lookup", flag.ContinueOnError)
	dataDir, limits := commonFlags(fs)
	samples := fs.Int("samples", 100, "trace IDs to look up")
	seed := fs.Uint64("seed", 1, "sampling seed")
	window := fs.Duration("window", 7*24*time.Hour, "lookup window ending now")
	if err := fs.Parse(args); err != nil {
		return err
	}

	sys, err := openSystem(ctx, *dataDir, limits())
	if err != nil {
		return err
	}
	defer sys.Close()

	ids, err := scenario.SampleTraceIDs(ctx, sys.store, sys.manifest, sys.dataDir, *samples, *seed)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return errors.New("no cold spans to sample trace IDs from")
	}

	now := time.Now().UTC()
	var lat report.Latencies
	var spans, notFound int
	for _, id := range ids {
		start := time.Now()
		got, err := sys.reader().TraceByID(ctx, id, now.Add(-*window), now)
		if err != nil {
			return err
		}
		lat.Add(time.Since(start))
		spans += len(got)
		if len(got) == 0 {
			notFound++
		}
	}
	rss, err := report.PeakRSS()
	if err != nil {
		return err
	}
	return report.WriteJSON(stdout, map[string]any{
		"samples":        len(ids),
		"window_hours":   window.Hours(),
		"latency":        lat.Summary(),
		"spans_found":    spans,
		"not_found":      notFound,
		"peak_rss_bytes": rss,
	})
}

func backupCmd(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	dataDir, limits := commonFlags(fs)
	out := fs.String("out", "", "backup directory; must not exist or be empty")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return errors.New("--out is required")
	}

	sys, err := openSystem(ctx, *dataDir, limits())
	if err != nil {
		return err
	}
	defer sys.Close()

	rep, err := backup.Backup(ctx, sys.lock, sys.flusher, sys.manifest, sys.dataDir, *out)
	if err != nil {
		return err
	}
	return report.WriteJSON(stdout, rep)
}
