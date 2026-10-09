# Storage spike results (phase 0)

Date: 8 October 2026. Plan: [2026-10-07-2303-storage-spike](../plans/2026-10-07-2303-storage-spike.md). Code: `spike/storage/` on branch `storage-spike`.

The spike built the smallest real version of the hot/cold storage path from [DESIGN.md](../DESIGN.md) and drove it with seven simulated days of the `busy` workload. Six of the eight checks pass. Two fail:

- **Memory budget:** peak RSS in the 7-day runs sits at the 1 GB limit, 997 MB and 1,001 MB.
- **Trace lookup:** p95 is 1.03 s against a 1 s threshold.

Neither failure breaks the hot/cold layout itself. Both are now open decisions in the design doc.

## Environment

| | |
| --- | --- |
| Machine | KVM virtual machine, 4 vCPU AMD EPYC-Rome, 7.6 GB RAM, 4 GB swap |
| Disk | 76 GB virtual disk, reported non-rotational |
| OS | Ubuntu 24.04.1, Linux 6.8.0 |
| Toolchain | Go 1.27.1, `duckdb-go` v2.10506.0 (DuckDB 1.5.6, static libraries with bundled jemalloc), `modernc.org/sqlite` v1.60.1 |
| DuckDB settings | `memory_limit = 256MB`, `threads = 2`, `preserve_insertion_order = false`, temp directory inside the data dir |

Each experiment ran alone, one after another, from `spike/storage/scripts/experiments.sh`. Reports are the JSON objects the spike CLI prints, and peak RSS is `VmHWM`, cross-checked with `/usr/bin/time -v`.

## Workload

`busy` profile: 100 logs/s, 50 spans/s, and 50 metric points/s, from 10 services in 2 environments over 500 metric series. That is 17.3 million rows per day and 121 million over seven days: 60.5 M logs, 30.2 M spans, and 30.2 M metric points. One percent of logs arrive 10 minutes to 3 hours late. Counters reset every 6 simulated hours.

The accelerated runs commit one simulated minute per transaction, about 12,000 rows. The real-time run commits once per second, about 200 rows, which is the shape real ingestion has.

## Results by checklist item

### Crash-safe flush: pass

| Threshold | Measured |
| --- | --- |
| All four crash points pass | 4 of 4 |

`scripts/crash-matrix.sh` kills the real process with exit 137 after each flush step (`written`, `renamed`, `recorded`, `deleted`), then reconciles and verifies the result. Hot plus cold equals the generated count for every signal, distinct keys equal that count too, and the manifest matches the files on disk one to one. Reconcile behaved as designed at each step:

- `written`: deleted the orphaned temp file.
- `renamed`: adopted the orphan Parquet file.
- `recorded`: applied the cutoff to delete the already-exported hot rows (69 here).
- `deleted`: had nothing left to repair.

The unit tests add a late row after each crash and prove reconcile leaves it alone.

### Retention reclaims disk: pass, with a caveat

| Threshold | Measured |
| --- | --- |
| Filesystem usage at most limit + one hour partition + hot file | max 1,432 MB, against a 2,000 MB cap |
| `hot.duckdb` stops growing after the first day | 90 MB at day 1 and 91 MB at day 7 (max 105 MB) |
| File count bounded | 352 files from day 3 onward |

The cold manifest plateaus at 1,258 MB once the 3-day age limit takes effect, and filesystem usage stays between 1,394 and 1,432 MB from then on. A retention pass costs 13 ms at p50 and 20 ms at worst.

Caveat: three days of `busy` data is about 1.26 GB, under the 2 GB cap, so the age limit always fired first. The size ring buffer was exercised only by unit tests, not at scale.

### Ingestion under heavy queries: pass

Five minutes in real time on top of the full 7-day dataset, with two goroutines looping the six queries:

| Threshold | Measured |
| --- | --- |
| Commit latency p99 under 500 ms | p99 46 ms (p50 15 ms, max 231 ms) |
| Query p99 under 5 s | worst p99 2.05 s (`jobs_rate`) |

| Query | p50 | p95 | p99 |
| --- | --- | --- | --- |
| `count_by_severity` (24 h) | 379 ms | 555 ms | 583 ms |
| `request_rate` (6 h) | 685 ms | 849 ms | 895 ms |
| `p95_latency` (1 h) | 57 ms | 88 ms | 100 ms |
| `jobs_rate` (24 h) | 1,677 ms | 1,962 ms | 2,047 ms |
| `trace_by_id` (7 d) | 1,575 ms | 1,807 ms | 1,999 ms |
| `error_logs_page` (7 d) | 317 ms | 396 ms | 461 ms |

There were no query errors and no catch-up seconds, so ingestion never fell behind its one-second ticks.

### Trace lookup: fail, narrowly

| Threshold | Measured |
| --- | --- |
| p95 under 1 s for 100 random trace IDs over 7 days | p95 1,025 ms (p50 940 ms, max 1,059 ms) |

The lookup scans the `trace_id` column of about 168 span files (about 940 MB), one per hour in the window. Under the concurrent query load above, the same query's p95 was 1.81 s. One of the 100 sampled IDs was not found: the sampler picked a span from the first two hours of the dataset, which by then had slid out of the moving 7-day window.

As the plan specifies, the trace index follow-up is now open.

### Metric semantics: pass

The unit tests use hand-built series with known answers, and every test runs against both hot and cold data. They cover:

- counter deltas with a value drop and a `start_ts` change;
- integer and double counters;
- merged histogram buckets with p50 = 42 and p90 = 100 by linear interpolation;
- p99 in the unbounded bucket returning the last finite bound;
- incompatible bounds reported rather than merged;
- a bounds change inside one series, which is never subtracted.

### Backup equivalence: pass

The unit test runs the six queries before a backup and against the restored copy, and the results are identical. At scale, a backup of the 7-day dataset copied 846 files (2.98 GB) in 9.7 s with 102 MB RSS, after force-flushing 59,602 hot rows.

### Both Linux architectures: pass

CI runs on `ubuntu-24.04` and `ubuntu-24.04-arm`; both are green, with build and tests at about 2 minutes. Unstripped binaries: amd64 80.1 MB, arm64 73.2 MB.

### Memory budget: fail, no headroom

| Run | Peak RSS (VmHWM) |
| --- | --- |
| 7-day accelerated, no retention | 997 MB |
| 7-day accelerated, with retention | 1,001 MB |
| 1-day accelerated | 932 MB |
| 1-day accelerated, `GOMEMLIMIT=192MiB` | 884 MB |
| 5-minute real time with 2 query goroutines | 852 MB |
| `lookup` alone | 181 MB |
| `backup` alone | 102 MB |

The threshold is under 1 GB. One 7-day run lands just below it and the other just above, so the budget is not met with any margin.

Capping the Go heap saves only about 50 MB, so most of the excess over the 256 MB `memory_limit` is native DuckDB memory. DuckDB's `memory_limit` governs its buffer manager, not every allocation. The 1-day run already reaches 932 MB, so this is a plateau reached early, not growth over time. The DuckDB build bundles its own jemalloc, so glibc allocator tuning will not help.

The [memory follow-up](memory.md) attributes the excess to jemalloc retention and recommends a 1.5 GB budget with these settings unchanged.

## Volume and cost

| | logs | spans | metric points | total |
| --- | --- | --- | --- | --- |
| Parquet for 7 days | 1,672 MB | 941 MB | 346 MB | 2,959 MB |
| Bytes per row | 27.6 | 31.1 | 11.4 | |

- **Storage:** about 423 MB of Parquet per `busy` day.
- **Hot data:** `hot.duckdb` stays at about 90 MB with a WAL of at most 16 MB. DuckDB spilled to `tmp/` (up to 53 MB) while sorting during flushes.
- **Flush:** 838 flushes (one per signal and hour) took 369 ms at p50, 1.82 s at p95, and 2.16 s at most. The hot `DELETE` alone took 6.5 ms at p50, 40 ms at p95, 880 ms at p99, and 1.0 s at most. That `DELETE` is the window in which a query can see an hour twice (design decision 12).
- **Late data:** late logs give most log hours three files. Of the 504 signal-hours, 336 have one file and 166 have three, for 838 files per week.
- **Throughput:** the accelerated run sustained 41,000 rows/s, flushes included, using about 1.3 cores.

## What surprised us

- **Every DuckDB commit costs about 5 ms,** the WAL sync, regardless of size. Real-time per-second commits are fine, but the accelerated mode had to batch a simulated minute per commit to finish in under an hour.
- **Large batched commits stall.** The 12,000-row commits took 73 ms at p50 but 1.2 s at p95 and up to 3.5 s, most likely when they coincide with a checkpoint. The product's writer should cap batch size and not assume commit time scales linearly.
- **`memory_limit` is not a process limit.** Whole-process RSS ran about 3.5 times higher.
- **Late data dominates the log file count.** One percent of late logs triples the number of log files.
- **The manifest is a race participant.** A query that reads the manifest before a flush records a file, then scans hot after the flush's `DELETE`, misses that hour entirely. This is the inverse of the accepted duplicate window. The codex review found it and the spike documents it but does not fix it.

## Recommendations

- **Hot window:** keep the current and previous hour, flushed hourly. The hot file stays flat at about 90 MB, and the flush costs at most about 2 s per signal and hour at `busy`.
- **Cold file resolution:** keep resolving cold files from the manifest, combined with the ingest-cutoff flush rule. Crash recovery and backup depend on both, and both held under real process kills.
- **`threads`:** keep 2. Query p99 stays at 2 s or less with two concurrent query loops while ingestion keeps up.
- **`memory_limit`:** keep 256 MB for now, but do not claim comfortable operation in 1 GB. Before phase 1 storage code is final, run a memory follow-up to find where the native memory goes and bring peak RSS to about 700 MB. Candidates, each to measure on its own:
  - DuckDB's allocator settings (`allocator_flush_threshold`, `allocator_background_threads`);
  - `memory_limit = 128MB`;
  - smaller flush `COPY` row groups;
  - sorting by `ts` in fewer, smaller passes.

  If none of these reaches the target, the honest options are a 1.5 GB budget or a different flush shape. That is a product decision.
- **Trace index:** needed. Full-window lookups are just over 1 s alone and about 1.8 s under load. Build the per-partition index the design names as the fallback: trace ID to time range, written at flush. Bounded lookups from trace lists and log links already carry timestamps and do not need it.
- **Late-data files:** compacting the small late files of an hour into its main file, once the hour can no longer receive late data, keeps the file count, and with it query planning cost, proportional to hours. This is not urgent at 838 files per week.
- **Query and flush consistency:** phase 1 picks one protocol. Either queries hold a read side of the maintenance lock, or hot rows are filtered by each hour's recorded cutoff against a snapshot pinned before the manifest read. The second fixes both the duplicate and the gap.

## Reproducing

```sh
mise install
rite spike-check
spike/storage/scripts/crash-matrix.sh
setsid nohup spike/storage/scripts/experiments.sh > /dev/null 2>&1 < /dev/null &
tail -f .tmp/experiments/status.log   # about 1 h 45 min on the machine above
```

Reports land in `.tmp/experiments/`. The memory diagnostic was the two 1-day runs from the table above: `spike run --profile busy --days 1`, with and without `GOMEMLIMIT=192MiB`.
