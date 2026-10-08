# Memory Follow-up (Phase 0b) Implementation Plan

> **For agentic workers:** Use executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Find where the storage spike's whole-process memory goes at the `busy` workload and bring peak RSS from about 1.0 GB to about 700 MB with measured settings, or produce the evidence for changing the published budget.

**Tech Stack:** The existing `spike/storage` module (Go 1.27, `duckdb-go` v2.10506.0 on DuckDB 1.5.6, `modernc.org/sqlite`), `/proc/self/status`, Go `runtime.MemStats`, DuckDB's `duckdb_memory()` and allocator settings, `/usr/bin/time -v`.

---

## Design

### Why

The storage spike ([results](../spikes/storage.md)) passed every structural check but missed the memory budget: peak RSS of 997 MB and 1,001 MB over seven simulated days against a 1 GB limit, and 932 MB after one day. DuckDB's `memory_limit` of 256 MB governs its buffer manager only, and capping the Go heap with `GOMEMLIMIT` saved about 50 MB, so roughly 600 MB is native memory nobody has attributed yet. The design doc now says no 1 GB claim is published until a follow-up brings this down. This plan is that follow-up. It reuses the spike harness, which goes away once phase 1 ports the storage code, so it runs now.

### What the spike cannot tell us today

The spike records one number, `VmHWM` at exit. That cannot distinguish three very different situations, and each has a different fix:

- **Transient peak.** RSS spikes during each hourly flush (sort plus Parquet encoding) and sits much lower in between. Then the lever is the flush shape: row group size, sort, write buffer.
- **DuckDB-accounted plateau.** `duckdb_memory()` shows DuckDB holding most of the RSS in buffers, column data, or the allocator tag. Then the lever is `memory_limit` and the allocator flush threshold.
- **Unaccounted plateau.** RSS stays high while DuckDB reports little and Go reports little. That is allocator retention (jemalloc holding freed pages) or something outside both accountings, and the levers are `allocator_flush_threshold` and `allocator_background_threads`.

So the first deliverable is attribution, not tuning. Every run gets a memory time series with three parts: process RSS, Go runtime memory, and DuckDB memory by tag. The difference between RSS and the sum of the other two is the unaccounted part.

### Key decisions

1. **Attribution before tuning.** Experiment E0 is the current settings with sampling on. Its breakdown at the peak decides which of E1 to E4 matter; the plan names all four so the executor runs them regardless, but the results note leads with the attribution.
2. **One-day runs for the matrix, seven days only for confirmation.** The spike showed the plateau is reached within the first day (932 MB of 997 MB). A one-day `busy` run takes under ten minutes, so the whole matrix fits in an hour. The winning combination is confirmed on seven days plus a five-minute real-time run with query load, because those are the two runs the original threshold was defined on.
3. **One variable per run.** Each experiment changes exactly one thing against E0. Combinations come only in the confirmation run.
4. **Settings become flags, not code edits.** `store.Limits` and `flush.Flusher` grow fields; the CLI exposes them. A script runs the matrix. Nothing in the spike is special-cased for an experiment.
5. **A sampler goroutine on the wall clock, once per second, in both modes.** Flushes are synchronous in the run loop, so sampling from the loop would miss everything allocated and freed inside a flush, which is exactly where a transient peak would be. The sampler runs on its own connection, tags each sample with the current simulated time, and stops when the run ends. Each sample also records whether a flush was in progress, from a flag the run loop sets around `FlushDue`, so transient peaks can be tied to flushes without guessing. The report compares the highest sampled `RSS` with `VmHWM`. If `VmHWM` is more than 10% higher, the peak fell between samples and its attribution is **unknown**: the nearest sample says nothing reliable about what was allocated at the peak. In that case E0 is re-run once with `--memory-sample-interval 100ms` before any conclusion is drawn; if that still misses, the note states the peak attribution as unknown and reasons only from the sampled series.
6. **The target is about 700 MB peak RSS at `busy`, measured the same way as before (`VmHWM`), with no regression beyond 20% in flush p95, commit p99, or query p99.** If no combination reaches it, the plan's output is still complete: the results note recommends a budget of 1.5 GB with the attribution as evidence, and the design doc changes accordingly. Not reaching 700 MB is a valid outcome, not a failed task.
7. **No product code, no new dependencies.** Everything lands in `spike/storage` and `docs/`.

### Memory sample

```go
// MemorySample is one point of the memory time series.
type MemorySample struct {
	SimTime    time.Time        `json:"sim_time"`
	Wall       time.Time        `json:"wall"`
	RSS        int64            `json:"rss_bytes"`      // VmRSS
	HWM        int64            `json:"hwm_bytes"`      // VmHWM so far
	GoResident int64            `json:"go_resident_bytes"` // MemStats.Sys - MemStats.HeapReleased: an estimate of Go's resident share
	GoHeapUsed int64            `json:"go_heap_bytes"`     // MemStats.HeapInuse
	DuckDB     map[string]int64 `json:"duckdb_bytes"`      // duckdb_memory() memory_usage_bytes by tag, non-zero tags only
	DuckDBTemp int64            `json:"duckdb_temp_bytes"` // sum of temporary_storage_bytes
	Residual   int64            `json:"residual_bytes"`    // RSS - GoResident - sum(DuckDB); approximate, see below
	InFlush    bool             `json:"in_flush"`          // a flush was running when the sample was taken
}
```

`GoResident` is an estimate: `Sys` is what Go reserved from the OS and `HeapReleased` is what it gave back, so the difference approximates Go's resident pages. `Residual` is what neither accounting claims: allocator caches and fragmentation inside DuckDB's jemalloc, thread stacks, mapped library text, and any native allocation outside DuckDB's tagged allocators. It is a diagnostic, not a precise number; the note reports it as such and leans on how it moves between experiments rather than on its absolute value.

The report gains `memory_samples`, plus `memory_peak`, the sample with the highest `RSS`, so the breakdown at the peak is one lookup, and `memory_peak_vs_hwm`, the ratio of `VmHWM` to the highest sampled `RSS`.

### Experiments

All runs: `--profile busy`, `--threads 2`, fresh data dir, `/usr/bin/time -v` as a cross-check of `VmHWM`. E0 to E5 are one-day accelerated runs.

| Id | Change against E0 | What it tests |
| --- | --- | --- |
| E0 | none: `memory_limit 256MB`, allocator defaults, sorted `COPY`, default row groups | attribution baseline |
| E1 | `allocator_flush_threshold = 16MB` | whether the allocator is holding freed memory |
| E2 | `allocator_background_threads = true` | whether background decay returns pages |
| E3 | `memory_limit = 128MB` | how much of the plateau tracks the buffer manager limit |
| E4 | `COPY ... (ROW_GROUP_SIZE 30000)` | Parquet writer buffering during flush |
| E5 | `COPY` without `ORDER BY ts` | the sort's share of flush memory; a diagnostic, not a candidate on its own, since sorted files are a design property |
| E6 | best combination of E1 to E4, seven days | confirmation on the original volume run |
| E7 | best combination, real time, 5 minutes, `--query-load 2`, on E6's data dir | confirmation on the original latency run, with query memory included |

`GOMEMLIMIT` is not an experiment: the spike already measured it at about 50 MB and the product will set it anyway; E6 and E7 run with `GOMEMLIMIT=192MiB` so the confirmation reflects the product shape.

### Reading the results

- If E0's peak shows `Residual` as the largest part and E1 or E2 cuts it, the allocator settings go into the product's DuckDB boot sequence.
- If `DuckDB` tags dominate and E3 cuts the peak roughly in proportion, `memory_limit` is the lever and the product default moves to 128 MB only if E7's query latencies hold.
- If the peak is transient and coincides with flushes, E4 (and the information from E5) decide the flush shape.
- If nothing reaches about 700 MB, the note recommends 1.5 GB and says which part of memory is irreducible with this layout.

A setting is accepted only when it helps memory and does not regress flush p95, commit p99, or query p99 by more than 20%. Meeting the memory target with a latency regression is a rejection, not a trade. The only settings the note may recommend are ones whose own E7 passed every latency threshold. If no combination gets there, the recommendation is the baseline settings, whose latencies the storage spike already measured and which passed, plus the budget change. That holds for every outcome, including memory and latency both missing.

## File Structure

```
spike/storage/
  internal/report/report.go              MemorySample, memory_samples and memory_peak on Report
  internal/report/memory.go              ReadProcMemory (VmRSS, VmHWM), GoMemory
  internal/report/memory_test.go
  internal/store/store.go                Limits gains AllocatorFlushThreshold, AllocatorBackgroundThreads; MemoryByTag
  internal/store/store_test.go           settings round-trip, MemoryByTag returns tags
  internal/flush/flush.go                Flusher gains RowGroupSize, Unsorted; COPY options
  internal/flush/flush_test.go           COPY SQL reflects the options; unsorted file still verifies
  internal/scenario/scenario.go          runs the sampler goroutine; records memory_peak and the HWM ratio
  cmd/spike/main.go                      flags for the new settings; summarize subcommand
  internal/report/summarize.go           Summarize(reports) -> markdown table
  internal/report/summarize_test.go
  scripts/memory-experiments.sh          E0 to E5 in order, then E6 and E7 for a named combination
docs/spikes/memory.md                    results note
docs/DESIGN.md                           memory paragraph, open decision, decision log
docs/spikes/storage.md                   link forward to the memory note
```

Go style follows `/go-style`; match the spike's existing conventions (external test packages, testify, hand-written fakes). Run `rite spike-check` before every commit, and `rite check` for commits that touch `docs/` or root files.

---

### Task 1: Memory attribution in reports

**Files:**
- Create: `spike/storage/internal/report/memory.go`
- Create: `spike/storage/internal/report/memory_test.go`
- Modify: `spike/storage/internal/report/report.go`
- Modify: `spike/storage/internal/store/store.go`
- Modify: `spike/storage/internal/store/store_test.go`

- [x] **Step 1: Failing tests**
  `report`: `ParseProcStatus(r io.Reader)` on a hand-written `/proc/self/status` excerpt returns `VmRSS` and `VmHWM` in bytes (the file reports kB). `GoMemory()` returns `Resident` greater than zero and not greater than `Sys`. `store`: `MemoryByTag(ctx)` on a fresh store returns a map that contains the `BASE_TABLE` key after a small insert, and a `Temp` total.

- [x] **Step 2: Implement**
  Move the existing `PeakRSS` parsing into `ParseProcStatus` and keep `PeakRSS` as a thin wrapper so nothing else changes. Add `ReadProcMemory() (rss, hwm int64, err error)`. `GoMemory()` wraps `runtime.ReadMemStats` and returns `Resident = Sys - HeapReleased`, `HeapInuse`, and `Sys`. `MemoryByTag` runs `SELECT tag, memory_usage_bytes, temporary_storage_bytes FROM duckdb_memory()` and drops zero rows. Add `MemorySample` from the design and `MemorySamples []MemorySample`, `MemoryPeak *MemorySample`, and `MemoryPeakVsHWM float64` to `Report`, all `omitempty`.

- [x] **Step 3: Verify**
  Run: `cd spike/storage && go test ./internal/report/ ./internal/store/`
  Expected: PASS

- [x] **Step 4: Commit**
  `git commit -m "Attribute spike memory to Go, DuckDB tags, and the remainder"`

> Deviation: `MemoryByTag` is a package function taking a `*sql.DB` or `*sql.Conn` (`store.MemoryByTag(ctx, q)`) instead of a `Store` method, so the Task 2 sampler can run it on its own connection. It returns `store.Memory{ByTag, Temp}`.
> Deviation: a small committed insert is accounted under `IN_MEMORY_TABLE`, not `BASE_TABLE`, so the test asserts that some non-zero tag is returned rather than `BASE_TABLE` specifically.

### Task 2: Sample memory during runs

**Files:**
- Modify: `spike/storage/internal/scenario/scenario.go`

- [x] **Step 1: Implement the sampler**
  Add a `memorySampler` type to the scenario package: `start(ctx, store, interval)` launches one goroutine with its own `*sql.Conn` that every tick builds a `MemorySample` from `ReadProcMemory`, `GoMemory`, and `MemoryByTag`, computes `Residual`, reads the current simulated time from an `atomic.Int64` the run loop updates, and appends under a mutex, tracking the sample with the highest `RSS`. `stop()` cancels, waits, and returns the samples and the peak. The sampler reads an `atomic.Bool` that `hourly` sets around `FlushDue` and records it as `InFlush`. The runner starts it at the beginning of both modes with the interval from `RunConfig.MemorySampleInterval` (CLI flag `--memory-sample-interval`, default 1 s) and stops it in `finish`, which also sets `MemoryPeakVsHWM = VmHWM / peak.RSS`. `PeakRSSBytes` from `VmHWM` stays as before so old and new reports stay comparable. The goroutine has an owner (the runner), a cancel (ctx), a wait (`stop`), and an error path (a failed sample is counted in `MemorySampleErrors` on the report, never fatal).

- [x] **Step 2: Verify by a short run**
  Run: `rite spike-build && spike/storage/bin/spike run --profile small --hours 3 --data-dir .tmp/mem-smoke > .tmp/mem-smoke.json && grep -c '"rss_bytes"' .tmp/mem-smoke.json`
  Expected: roughly one sample per wall second of the run, `memory_peak` with a `duckdb_bytes` map, and `memory_peak_vs_hwm` close to 1.

- [x] **Step 3: Commit**
  `git commit -m "Sample process, Go, and DuckDB memory during spike runs"`

> Deviation: the sampler lives in its own file, `internal/scenario/memory.go`, and needs no mutex: only its goroutine touches the samples until `stop` has waited for it, and the run loop shares only the two atomics. `stop` is idempotent, so `Run` also defers it to stop the goroutine on error paths.
> Deviation: the smoke run (4.7 s wall) got 4 samples and `memory_peak_vs_hwm` 1.25 at 1 s, since a run that short peaks between samples; the same run at `--memory-sample-interval 100ms` got 45 samples and a ratio of 1.01, which confirms the mechanism.

### Task 3: Settings as flags

**Files:**
- Modify: `spike/storage/internal/store/store.go`
- Modify: `spike/storage/internal/store/store_test.go`
- Modify: `spike/storage/internal/flush/flush.go`
- Modify: `spike/storage/internal/flush/flush_test.go`
- Modify: `spike/storage/cmd/spike/main.go`

- [ ] **Step 1: Failing tests**
  `store`: opening with `Limits{AllocatorFlushThreshold: "16MB", AllocatorBackgroundThreads: true}` makes `current_setting('allocator_flush_threshold')` contain `16.0 MiB` and `current_setting('allocator_background_threads')` equal `true`; an empty `AllocatorFlushThreshold` leaves the default. `flush`: with `RowGroupSize: 30000` the generated `COPY` statement contains `ROW_GROUP_SIZE 30000`; with `Unsorted: true` it contains no `ORDER BY`; a flush with both set still passes `Verify` and `parquet_metadata` of the file shows more than one row group for an hour of `small` logs. Expose the statement through a small unexported `copyStatement(signal, hour, cutoff, tmp string) string` function tested from an internal test file, since the SQL string is the thing under test.

- [ ] **Step 2: Implement**
  `Limits` gains the two fields; `Open` appends `SET GLOBAL allocator_flush_threshold = '...'` only when non-empty and `SET GLOBAL allocator_background_threads = true` only when set. `Flusher` gains `RowGroupSize int` and `Unsorted bool`; `copyStatement` builds the options list `FORMAT parquet, COMPRESSION zstd` plus `ROW_GROUP_SIZE n` when `n > 0`, and omits `ORDER BY ts` when `Unsorted`. `commonFlags` adds `--allocator-flush-threshold` (default empty) and `--allocator-background-threads`; `runCmd` adds `--row-group-size` (default 0) and `--unsorted-flush`, and sets them on the flusher after `openSystem`.

- [ ] **Step 3: Verify**
  Run: `rite spike-check`
  Expected: lint clean, all tests pass.

- [ ] **Step 4: Commit**
  `git commit -m "Expose allocator, row group, and sort settings as spike flags"`

### Task 4: Summarizer and experiment script

**Files:**
- Create: `spike/storage/internal/report/summarize.go`
- Create: `spike/storage/internal/report/summarize_test.go`
- Modify: `spike/storage/cmd/spike/main.go`
- Create: `spike/storage/scripts/memory-experiments.sh`

- [ ] **Step 1: Failing test**
  `Summarize(named []NamedReport) string` on two hand-built reports returns a markdown table with one row per report and the columns: name, peak RSS (MB, from `PeakRSSBytes`), RSS at peak sample, Go resident estimate at peak, DuckDB total at peak, residual at peak, whether the peak sample was in a flush, the HWM ratio, flush p95 (ms), commit p99 (ms), worst query p99 (ms, blank when no queries). Numbers are rounded to whole MB and ms. The same column names are used in the results note.

- [ ] **Step 2: Implement**
  `summarize` subcommand: `spike summarize <dir>` reads every `*.json` in the directory that decodes as a `Report`, orders them by file name, and prints the table. Files that are not reports (the `lookup` and `backup` outputs) are skipped with a note on stderr.

- [ ] **Step 3: Script**
  `scripts/memory-experiments.sh` follows `experiments.sh` in shape: detached-friendly, `.tmp/memory/` output dir, `status.log`, `/usr/bin/time -v` around every run, one fresh data dir per run. Unlike `experiments.sh` it never deletes the whole output dir. Each step writes to `<name>.json.part` and renames it to `<name>.json` only after the command exits 0, so an existing `<name>.json` always means a completed run. On start the script deletes every `*.part` and the data dir of each step that has no `<name>.json`, then runs only the steps whose report is missing. `FRESH=1` removes the output dir first. It runs E0 to E5 as one-day `busy` runs with the flags from the design table. Confirmation is switched on with `CONFIRM=1`, and `COMBO` holds the extra flags, which may be empty for the baseline settings (for example `CONFIRM=1 COMBO="--allocator-flush-threshold 16MB --row-group-size 30000"` or `CONFIRM=1 COMBO=""`). With `CONFIRM=1` it runs E6 (seven days) and E7 (real time, 5 minutes, `--query-load 2`, on E6's data dir) with those flags and `GOMEMLIMIT=192MiB`; a re-run with a different `COMBO` deletes only the E6 and E7 reports and data dir first. Without `CONFIRM` it stops after E5. Every invocation ends by writing `summary.md` from `spike summarize`.

- [ ] **Step 4: Verify**
  Run: `rite spike-check`, then `spike/storage/bin/spike summarize .tmp/experiments` against the previous spike's reports.
  Expected: a table with `full-7d`, `retained-7d`, and `full-rt` rows, with blank memory-at-peak columns since those reports predate sampling, and a stderr note for `lookup.json` and `backup.json`.

- [ ] **Step 5: Commit**
  `git commit -m "Add memory experiment script and report summarizer"`

### Task 5: Run E0 to E5 and attribute the peak

**Files:**
- Create: `docs/spikes/memory.md` (draft)

- [ ] **Step 1: Run the matrix**
  `setsid nohup spike/storage/scripts/memory-experiments.sh > /dev/null 2>&1 < /dev/null &` then follow `.tmp/memory/status.log`. About one hour.

- [ ] **Step 2: Attribute**
  First check `memory_peak_vs_hwm`; above 1.1, re-run E0 with `--memory-sample-interval 100ms` as the design says. Then from E0's `memory_peak`: write down RSS, the Go resident estimate, DuckDB by tag, residual, and `in_flush`. From the time series: whether RSS between flushes sits near the peak (plateau) or well below it (transient), and whether the highest samples are flush samples.

- [ ] **Step 3: Compare**
  From `summary.md`: the peak RSS of each of E1 to E5 against E0, and the flush p95 and commit p99 to catch regressions.

- [ ] **Step 4: Choose the combination**
  Pick every single-variable change that cut peak RSS by at least 50 MB without a regression over 20%. E5 never joins the combination; it only explains the sort's share. Record the choice and the reasoning in the draft note.

- [ ] **Step 5: Commit the draft**
  `git commit -m "Record memory attribution and single-variable results"`

### Task 6: Confirmation runs

- [ ] **Step 1: Run E6 and E7**
  `CONFIRM=1 COMBO="<chosen flags>" setsid nohup spike/storage/scripts/memory-experiments.sh > /dev/null 2>&1 < /dev/null &`. With an empty combination, `COMBO=""` still runs both with baseline settings plus `GOMEMLIMIT`. E0 to E5 are skipped because their reports exist, so this takes about an hour for the seven days plus five minutes real time.

- [ ] **Step 2: Check against the thresholds**
  Peak RSS of E6 and E7 against about 700 MB; E6 flush p95 against 1.82 s and commit p99 against the earlier accelerated run's; E7 commit p99 against 46 ms and worst query p99 against 2.05 s, all within 20%. E7 is valid only with `query_errors` of 0 and a non-zero count for every query; a run with failed or missing queries is re-run, not interpreted.

- [ ] **Step 3: If a threshold is missed**
  Memory missed, latency fine: do not retune further; record which part of memory stays after the combination and what it would take to move it, and recommend the 1.5 GB budget with the combination's settings kept if they helped. Memory met, latency regressed: drop the flag most likely responsible (a flush or commit regression points at the flag whose E-run regressed the same metric; a query regression points at `memory_limit`, the only flag that changes query execution) and re-run E6 and E7 once with the rest; if that run misses either threshold, treat it as the first case. Both missed: the first case. In no outcome does the note recommend settings whose own E7 did not pass.

### Task 7: Results note and design update

**Files:**
- Modify: `docs/spikes/memory.md`
- Modify: `docs/DESIGN.md`
- Modify: `docs/spikes/storage.md`

- [ ] **Step 1: Write the note**
  `docs/spikes/memory.md`: environment; the attribution of E0's peak as a small table; the experiment table with peak RSS and the regression columns; the confirmation results; "what surprised us"; and the recommended DuckDB boot settings for the product, or the budget change. Link the raw reports' names.

- [ ] **Step 2: Update the design**
  In `docs/DESIGN.md`: replace the "Phase 0 did not meet the budget" paragraph with the measured outcome and the settings the product adopts (or the new budget); resolve or restate the memory open decision; add a decision-log row. In `docs/spikes/storage.md`, add one line under the memory section pointing to the memory note.

- [ ] **Step 3: Verify**
  Run: `rite check && rite spike-check`
  Expected: both green.

- [ ] **Step 4: Commit**
  `git commit -m "Record memory follow-up results and update the design"`
