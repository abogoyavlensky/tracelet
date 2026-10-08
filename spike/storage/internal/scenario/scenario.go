// Package scenario drives the generator, writer, flusher, and query load for
// one spike run and collects its report.
package scenario

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/abogoyavlensky/tracelet/spike/storage/internal/flush"
	"github.com/abogoyavlensky/tracelet/spike/storage/internal/gen"
	"github.com/abogoyavlensky/tracelet/spike/storage/internal/manifest"
	"github.com/abogoyavlensky/tracelet/spike/storage/internal/query"
	"github.com/abogoyavlensky/tracelet/spike/storage/internal/report"
	"github.com/abogoyavlensky/tracelet/spike/storage/internal/store"
	"github.com/abogoyavlensky/tracelet/spike/storage/internal/telemetry"
)

// ExpectedFile is the per-signal generated row counts, kept in the data dir.
const ExpectedFile = "expected.json"

// RunConfig describes one run.
type RunConfig struct {
	Profile   gen.Profile
	Days      int
	Hours     int
	Minutes   int  // real-time only
	RealTime  bool // drive seconds from the wall clock instead of simulating them
	Retention flush.Policy
	QueryLoad int // concurrent query goroutines, real-time only
	Seed      uint64
	// BatchSeconds is how many simulated seconds go into one commit in
	// accelerated mode. Every DuckDB commit costs a WAL sync (about 5 ms), so
	// per-second commits would make a 7-day run take hours; commit latency is
	// measured by the real-time run, which always commits once per second.
	BatchSeconds int
}

// Deps is everything a run works on, built by the composition root.
type Deps struct {
	Store    *store.Store
	Writer   *store.Writer
	Manifest *manifest.Manifest
	Flusher  *flush.Flusher
	Lock     *sync.Mutex // maintenance lock
	DataDir  string
}

// Expected is the content of ExpectedFile. Counts accumulate across runs on
// the same data dir.
type Expected struct {
	Rows map[string]int64 `json:"rows"`
	Runs int              `json:"runs"`
	// SimulatedEnd is just after the last generated second. A later run must
	// start at or after it, or it would generate metric keys that exist.
	SimulatedEnd time.Time `json:"simulated_end,omitzero"`
}

// ReadExpected reads ExpectedFile from path. A missing file is an empty,
// zero-run Expected.
func ReadExpected(path string) (Expected, error) {
	exp := Expected{Rows: map[string]int64{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return exp, nil
	}
	if err != nil {
		return exp, fmt.Errorf("read expected: %w", err)
	}
	if err := json.Unmarshal(b, &exp); err != nil {
		return exp, fmt.Errorf("parse expected: %w", err)
	}
	if exp.Rows == nil {
		exp.Rows = map[string]int64{}
	}
	return exp, nil
}

// writeExpected replaces the file atomically, so a crash leaves either the
// previous or the new counts.
func writeExpected(path string, exp Expected) error {
	b, err := json.MarshalIndent(exp, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal expected: %w", err)
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create expected: %w", err)
	}
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		return fmt.Errorf("write expected: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("sync expected: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close expected: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename expected: %w", err)
	}
	return nil
}

// runner is the mutable state of one run.
type runner struct {
	cfg      RunConfig
	deps     Deps
	gen      *gen.Generator
	pending  telemetry.Batch // generated, not yet written (accelerated mode)
	exp      Expected
	expPath  string
	rep      report.Report
	commits  report.Latencies
	flushes  report.Latencies
	deletes  report.Latencies
	retained report.Latencies
}

// Run executes one run and returns its report. The caller has already
// reconciled the data dir.
func Run(ctx context.Context, cfg RunConfig, deps Deps) (report.Report, error) {
	wallStart := time.Now()
	r := &runner{cfg: cfg, deps: deps, expPath: filepath.Join(deps.DataDir, ExpectedFile)}

	exp, err := ReadExpected(r.expPath)
	if err != nil {
		return r.rep, err
	}
	// Every run on a data dir gets its own random stream, so trace and span
	// IDs never repeat; seq continues after the logs so far.
	seed := cfg.Seed + uint64(exp.Runs)
	exp.Runs++
	r.exp = exp
	r.gen = gen.NewGenerator(cfg.Profile, seed, exp.Rows["logs"])
	r.rep = report.Report{Profile: cfg.Profile.Name, Rows: map[string]int64{}}

	if cfg.RealTime {
		err = r.realTime(ctx)
	} else {
		err = r.accelerated(ctx)
	}
	if err != nil {
		return r.rep, err
	}

	if err := r.finish(ctx); err != nil {
		return r.rep, err
	}
	r.rep.WallSeconds = time.Since(wallStart).Seconds()
	return r.rep, nil
}

func (r *runner) accelerated(ctx context.Context) error {
	r.rep.Mode = "accelerated"
	total := time.Duration(r.cfg.Days)*24*time.Hour + time.Duration(r.cfg.Hours)*time.Hour
	if total <= 0 {
		return errors.New("accelerated run needs --days or --hours")
	}
	start := time.Now().UTC().Truncate(time.Hour).Add(-total)
	end := start.Add(total)
	if start.Before(r.exp.SimulatedEnd) {
		return fmt.Errorf("run would start at %s, before the previous run's end %s; use a fresh data dir",
			start.Format(time.RFC3339), r.exp.SimulatedEnd.Format(time.RFC3339))
	}
	r.rep.SimulatedStart, r.rep.SimulatedEnd = start, end
	r.rep.SimulatedHours = total.Hours()

	batch := max(r.cfg.BatchSeconds, 1)
	sec := 0
	for now := start; now.Before(end); now = now.Add(time.Second) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if now != start && now.Equal(now.Truncate(time.Hour)) {
			// Everything generated so far is committed before the hour's
			// expected counts are recorded and the flush runs.
			if err := r.writePending(ctx); err != nil {
				return err
			}
			if err := r.hourly(ctx, now); err != nil {
				return err
			}
		}
		r.generate(now)
		if sec++; sec%batch == 0 {
			if err := r.writePending(ctx); err != nil {
				return err
			}
		}
	}
	if err := r.writePending(ctx); err != nil {
		return err
	}
	return r.hourly(ctx, end)
}

// generate adds one second to the pending batch.
func (r *runner) generate(now time.Time) {
	b := r.gen.Next(now)
	r.exp.SimulatedEnd = now.Add(time.Second)
	r.pending.Logs = append(r.pending.Logs, b.Logs...)
	r.pending.Spans = append(r.pending.Spans, b.Spans...)
	r.pending.Points = append(r.pending.Points, b.Points...)
}

func (r *runner) writePending(ctx context.Context) error {
	if r.pending.Len() == 0 {
		return nil
	}
	err := r.commit(ctx, r.pending)
	r.pending = telemetry.Batch{}
	return err
}

// maxCatchUpBatch caps the seconds one real-time commit carries while
// catching up after dropped ticks.
const maxCatchUpBatch = 60

func (r *runner) realTime(ctx context.Context) error {
	r.rep.Mode = "real-time"
	if r.cfg.Minutes <= 0 {
		return errors.New("real-time run needs --minutes")
	}
	dur := time.Duration(r.cfg.Minutes) * time.Minute
	start := time.Now().UTC().Truncate(time.Second)
	r.rep.SimulatedStart, r.rep.SimulatedEnd = start, start.Add(dur)
	r.rep.SimulatedHours = dur.Hours()

	runCtx, cancel := context.WithTimeout(ctx, dur)
	defer cancel()

	load, err := r.startQueryLoad(runCtx)
	if err != nil {
		return err
	}

	// The next second to generate. A slow commit or flush makes the ticker
	// drop ticks; the next tick then generates every second missed, so the
	// offered load stays at the profile's rate and the lag is reported.
	next := start.Add(time.Second) // the first tick lands a second after start
	if next.Before(r.exp.SimulatedEnd) {
		next = r.exp.SimulatedEnd
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	lastHour := start.Truncate(time.Hour)
	var loopErr error
loop:
	for {
		select {
		case <-runCtx.Done():
			break loop
		case tick := <-ticker.C:
			now := tick.UTC().Truncate(time.Second)
			if h := now.Truncate(time.Hour); h.After(lastHour) {
				lastHour = h
				if loopErr = r.hourly(ctx, now); loopErr != nil {
					break loop
				}
			}
			generated := 0
			for ; !next.After(now); next = next.Add(time.Second) {
				r.generate(next)
				// Commit a long backlog in bounded chunks.
				if generated++; generated%maxCatchUpBatch == 0 {
					if loopErr = r.writePending(ctx); loopErr != nil {
						break loop
					}
				}
			}
			r.rep.CatchUpSeconds += max(generated-1, 0)
			if loopErr = r.writePending(ctx); loopErr != nil {
				break loop
			}
		}
	}
	cancel()
	r.rep.Queries, r.rep.QueryErrors, r.rep.QueryLastError = load.wait()
	if loopErr != nil {
		return loopErr
	}
	return r.hourly(ctx, time.Now().UTC())
}

func (r *runner) commit(ctx context.Context, b telemetry.Batch) error {
	st, err := r.deps.Writer.Write(ctx, b)
	if err != nil {
		return err
	}
	r.commits.Add(st.Latency)
	for signal, n := range map[string]int{"logs": len(b.Logs), "spans": len(b.Spans), "metric_points": len(b.Points)} {
		r.exp.Rows[signal] += int64(n)
		r.rep.Rows[signal] += int64(n)
	}
	return nil
}

// hourly records expected counts, then flushes and applies retention under
// the maintenance lock, then samples disk usage. The counts go first so a
// crash inside the flush finds them on disk.
func (r *runner) hourly(ctx context.Context, now time.Time) error {
	if err := writeExpected(r.expPath, r.exp); err != nil {
		return err
	}

	r.deps.Lock.Lock()
	stats, err := r.deps.Flusher.FlushDue(ctx, now)
	if err == nil {
		retStart := time.Now()
		var ret flush.RetentionReport
		ret, err = r.deps.Flusher.ApplyRetention(ctx, r.cfg.Retention, now)
		r.retained.Add(time.Since(retStart))
		r.rep.HoursRetained += ret.HoursDeleted
	}
	r.deps.Lock.Unlock()
	for _, s := range stats {
		r.flushes.Add(s.Duration)
		r.deletes.Add(s.DeleteDuration)
	}
	if err != nil {
		return err
	}

	return r.sample(ctx, now)
}

func (r *runner) sample(ctx context.Context, now time.Time) error {
	mBytes, err := r.deps.Manifest.TotalBytes(ctx)
	if err != nil {
		return err
	}
	entries, err := flush.DirBytes(r.deps.DataDir)
	if err != nil {
		return err
	}
	var total int64
	for _, n := range entries {
		total += n
	}
	files, err := r.deps.Manifest.All(ctx)
	if err != nil {
		return err
	}
	r.rep.Samples = append(r.rep.Samples, report.Sample{
		SimTime: now, ManifestBytes: mBytes, DirBytes: total, Entries: entries, Files: len(files),
	})
	r.rep.MaxDirBytes = max(r.rep.MaxDirBytes, total)
	return nil
}

func (r *runner) finish(ctx context.Context) error {
	if err := writeExpected(r.expPath, r.exp); err != nil {
		return err
	}
	r.rep.Commits = r.commits.Summary()
	r.rep.Flushes = r.flushes.Summary()
	r.rep.Deletes = r.deletes.Summary()
	r.rep.RetentionRuns = r.retained.Summary()

	files, err := r.deps.Manifest.All(ctx)
	if err != nil {
		return err
	}
	perHour := map[string]int{}
	r.rep.ParquetBytes = map[string]int64{}
	for _, f := range files {
		perHour[f.Signal+"|"+f.Hour.Format(time.RFC3339)]++
		r.rep.ParquetBytes[f.Signal] += f.Bytes
	}
	r.rep.FilesPerHour = map[int]int{}
	for _, n := range perHour {
		r.rep.FilesPerHour[n]++
	}
	r.rep.Files = len(files)

	if r.rep.DirBytes, err = flush.DirBytes(r.deps.DataDir); err != nil {
		return err
	}
	if r.rep.PeakRSSBytes, err = report.PeakRSS(); err != nil {
		return err
	}
	return nil
}

// queryLoad is the set of query goroutines of a real-time run.
type queryLoad struct {
	wg      sync.WaitGroup
	mu      sync.Mutex
	byName  map[string]*report.Latencies
	errors  int
	lastErr string
}

// startQueryLoad starts cfg.QueryLoad goroutines, each on its own connection,
// looping the query suite until ctx ends. wait stops nothing; it waits for
// them after the caller cancels ctx.
func (r *runner) startQueryLoad(ctx context.Context) (*queryLoad, error) {
	load := &queryLoad{byName: map[string]*report.Latencies{}}
	if r.cfg.QueryLoad <= 0 {
		return load, nil
	}
	traceIDs, err := SampleTraceIDs(ctx, r.deps.Store, r.deps.Manifest, r.deps.DataDir, 100, r.cfg.Seed)
	if err != nil {
		return nil, err
	}
	if len(traceIDs) == 0 {
		traceIDs = []string{"00000000000000000000000000000000"}
	}

	for i := range r.cfg.QueryLoad {
		conn, err := r.deps.Store.Conn(ctx)
		if err != nil {
			return nil, fmt.Errorf("query connection: %w", err)
		}
		load.wg.Go(func() {
			defer conn.Close()
			reader := query.Reader{DB: conn, Manifest: r.deps.Manifest, DataDir: r.deps.DataDir}
			for n := 0; ctx.Err() == nil; n++ {
				p := query.SuiteParams{
					Now:     time.Now().UTC(),
					TraceID: traceIDs[(n*r.cfg.QueryLoad+i)%len(traceIDs)],
					Service: "svc-01",
					Route:   "/api/users",
				}
				for _, q := range query.Suite(p) {
					start := time.Now()
					_, err := q.Run(ctx, reader)
					load.record(ctx, q.Name, time.Since(start), err)
				}
			}
		})
	}
	return load, nil
}

func (l *queryLoad) record(ctx context.Context, name string, d time.Duration, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err != nil {
		// A query cut off by the end of the run is not a failure.
		if ctx.Err() == nil {
			l.errors++
			l.lastErr = err.Error()
		}
		return
	}
	lat, ok := l.byName[name]
	if !ok {
		lat = &report.Latencies{}
		l.byName[name] = lat
	}
	lat.Add(d)
}

func (l *queryLoad) wait() (map[string]report.Summary, int, string) {
	l.wg.Wait()
	if len(l.byName) == 0 {
		return nil, l.errors, l.lastErr
	}
	out := map[string]report.Summary{}
	for name, lat := range l.byName {
		out[name] = lat.Summary()
	}
	return out, l.errors, l.lastErr
}

// SampleTraceIDs picks up to n trace IDs by reading spans from randomly chosen
// cold span files.
func SampleTraceIDs(ctx context.Context, s *store.Store, m *manifest.Manifest, dataDir string, n int, seed uint64) ([]string, error) {
	files, err := m.All(ctx)
	if err != nil {
		return nil, err
	}
	var spans []manifest.File
	for _, f := range files {
		if f.Signal == "spans" {
			spans = append(spans, f)
		}
	}
	if len(spans) == 0 {
		return nil, nil
	}

	rng := rand.New(rand.NewPCG(seed, seed+1))
	seen := map[string]bool{}
	var ids []string
	for attempt := 0; len(ids) < n && attempt < 4*n; attempt++ {
		f := spans[rng.IntN(len(spans))]
		path := filepath.Join(dataDir, f.Path)
		q := "SELECT trace_id FROM " + flush.ParquetSource([]string{path}) + " USING SAMPLE 1 ROWS"
		var id string
		if err := s.DB().QueryRowContext(ctx, q).Scan(&id); err != nil {
			return nil, fmt.Errorf("sample trace id from %s: %w", f.Path, err)
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids, nil
}
