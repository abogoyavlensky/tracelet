# Memory follow-up results (phase 0b)

Date: 9 October 2026. Plan: [2026-10-08-2143-memory-followup](../plans/2026-10-08-2143-memory-followup.md). Code: `spike/storage/` on branch `memory-followup`.

The storage spike missed its 1 GB budget at the `busy` workload, with about 600 MB of native memory unattributed. This follow-up attributes it and tries the cheap levers.

- **Where the memory goes:** at the one-day peak, Go holds about 20 MB, DuckDB's own accounting about 256 MB, and about 600 MB is memory DuckDB's bundled jemalloc keeps without DuckDB counting it.
- **What helps:** only `allocator_background_threads`, which cuts peak RSS by about 200 MB. It does not reach the 700 MB target on seven days (788 MB), and its real-time confirmation run doubled commit p99 (87 ms against 46 ms). So it is not recommended.
- **Recommendation:** keep the baseline DuckDB settings and publish a **1.5 GB** budget instead of 1 GB. The baseline settings peaked at 1,001 MB.

## Environment

Same machine and toolchain as the [storage spike](storage.md#environment): a 4 vCPU KVM VM with 7.6 GB RAM, Go 1.27.1, and `duckdb-go` v2.10506.0 (DuckDB 1.5.6, static, bundled jemalloc). Every run is `spike run --profile busy --threads 2` on a fresh data dir. The runs went one at a time from `spike/storage/scripts/memory-experiments.sh`.

Peak RSS is `VmHWM`, cross-checked with `/usr/bin/time -v`. Each run also samples memory once per wall-clock second. A sample records process RSS, the Go runtime's resident estimate (`Sys - HeapReleased`), DuckDB's `duckdb_memory()` by tag, and the residual that neither accounts for. The residual is a diagnostic: it includes allocator caches, mapped code, and thread stacks, and how it moves between runs says more than its absolute value. Memory is in decimal MB.

Raw reports are in `.tmp/memory/`: `e0-baseline.json` to `e7-confirm-rt.json`, with `summary.md` and one `/usr/bin/time` log per run.

## Where the memory goes

E0 is the baseline: one simulated day with `memory_limit = 256MB` and allocator defaults. Its highest sample matches `VmHWM` exactly (ratio 1.00), so this breakdown is the peak itself.

| E0 at the peak | MB |
| --- | ---: |
| RSS | 874 |
| Go resident estimate | 21 |
| DuckDB, by tag | 256: `COLUMN_DATA` 126, `ORDER_BY` 107, `ALLOCATOR` 19, `BASE_TABLE` 4 |
| Residual | 597 |

- **Allocator retention dominates.** The residual is about 600 MB at the peak and about 400 MB at the median. Go stays near 20 MB, and DuckDB never reports more than its 256 MB limit. At the end of the run DuckDB accounts for 9 MB while the residual is 655 MB: jemalloc keeps freed memory.
- **About 85 MB of the residual is fixed.** The residual never falls below 80 to 90 MB, which matches mapped code in an 80 MB static binary.
- **The shape is a plateau with spikes, not flush peaks.** The median RSS is 550 to 590 MB all day, with spikes to 750 to 870 MB. Only 5 of the 20 highest samples were taken during a flush. The rest come from the 60-second commits that fill `IN_MEMORY_TABLE` for the two hot hours.

## Single-variable runs

Each run changes one thing against E0, over one simulated day. All of them ingested the same 17.28 M rows into 118 files, with no sample errors.

| Run | Peak RSS (MB) | RSS at peak sample (MB) | Go resident at peak (MB) | DuckDB at peak (MB) | Residual at peak (MB) | Peak in flush | HWM / peak sample | Flush p95 (ms) | Commit p99 (ms) | Worst query p99 (ms) |
| --- | ---: | ---: | ---: | ---: | ---: | --- | ---: | ---: | ---: | ---: |
| e0-baseline | 874 | 874 | 21 | 256 | 597 | yes | 1.00 | 1139 | 2293 |  |
| e1-flush-threshold | 886 | 871 | 24 | 256 | 591 | no | 1.02 | 1137 | 2296 |  |
| e2-background-threads | 741 | 659 | 19 | 134 | 506 | yes | 1.12 | 1157 | 2282 |  |
| e3-memory-limit-128 | failed | | | | | | | | | |
| e4-row-group-30k | 828 | 807 | 21 | 255 | 531 | no | 1.03 | 1367 | 2262 |  |
| e5-unsorted | 811 | 803 | 23 | 256 | 524 | no | 1.01 | 707 | 2279 |  |

- **E1, `allocator_flush_threshold = 16MB`:** no effect on the peak. The median outside flushes falls from 548 to 505 MB.
- **E2, `allocator_background_threads = true`:** peak 741 MB, 133 MB below E0, and the median outside flushes falls from 548 to 392 MB. Flush and commit latency are unchanged. The median residual falls from about 405 to about 250 MB, so it acts on allocator retention, as E0 suggested. Its highest sample is 12% below `VmHWM`, so the breakdown at its true peak is unknown.
- **E3, `memory_limit = 128MB`:** fails. The first flush runs out of memory sorting an hour of logs in `COPY ... ORDER BY ts`: "failed to allocate data of size 5.0 MiB (117.3 MiB/122.0 MiB used)". Two attempts failed the same way. With this flush, 128 MB is not usable.
- **E4, `ROW_GROUP_SIZE 30000`:** 46 MB lower, under the 50 MB bar. Flush p95 is 20.0% slower, right at the regression limit. Rejected.
- **E5, `COPY` without `ORDER BY ts` (diagnostic only):** 63 MB lower, and flush p95 falls by 38%. That is the sort's share. Sorted files are a design property, so this is not a candidate.

Each configuration ran once. The storage spike's one-day run of the E0 configuration reached 932 MB against E0's 874 MB, so run-to-run spread is probably tens of MB. That puts E1, E4, and E5 within noise and leaves E2 clearly outside it.

Only E2 cut peak RSS by at least 50 MB without a regression, so the confirmation runs used it alone, with `GOMEMLIMIT=192MiB`.

## Confirmation

E6 is seven accelerated days. E7 is five minutes of real-time ingestion with two query loops, on E6's data. Thresholds are the storage spike's measurements plus 20%.

| Run | Peak RSS (MB) | Before | Flush p95 (ms) | Commit p99 (ms) | Worst query p99 (ms) |
| --- | ---: | ---: | ---: | ---: | ---: |
| e6-confirm-7d | **788** | 997 | 1734 (limit 2186) | 2291 (limit 2790) | |
| e7-confirm-rt | 650 | 852 | 1504 | **87** (limit 55) | 2023 (limit 2460) |

E7 is valid: 0 query errors, and each of the six queries ran 123 or 124 times.

- **Memory misses the target.** Background threads cut about 200 MB in both runs. Seven days still peak at 788 MB, against about 700.
- **Real-time commit latency regresses.** Commit p99 is 87 ms against 46 ms. The p50 is 11.5 ms against 15 ms, so the regression is in the tail.
- **The cause of the regression is not isolated.** E7 differs from the storage spike's real-time run in three ways: background threads, `GOMEMLIMIT`, and the once-per-second memory sampler, which briefly stops the world in `runtime.ReadMemStats`. A control run with baseline settings plus `GOMEMLIMIT` and the sampler would separate them. It was not run.

At E6's highest sample, 720 MB, the memory left after background threads is Go 24 MB, DuckDB 237 MB, and a residual of 460 MB. DuckDB's share is almost all `IN_MEMORY_TABLE`, 208 MB: the two hot hours, which the 256 MB limit caps. Moving that share needs a lower `memory_limit`, which needs a flush that sorts in less memory than one hour of logs takes today. Moving the residual needs allocator settings whose latency effect is understood.

## What surprised us

- **`memory_limit` is close to fully used by the hot window.** At the peak, the two hot hours alone hold 208 MB of the 256 MB limit, so there is little headroom for the flush sort. That is why 128 MB fails at once.
- **jemalloc keeps far more than DuckDB uses.** At the end of the one-day baseline, DuckDB accounts for 9 MB and the process holds 688 MB.
- **`allocator_flush_threshold` does nothing for the peak.** Background threads are what returns memory.
- **The storage spike's harness had a latent bug.** `store.Open` re-ran `SET GLOBAL temp_directory` on every new pooled connection, and DuckDB rejects that once it has spilled: "Cannot switch temporary directory after the current one has been used". At 256 MB no run spilled, so it never showed. The spike now sets the temp directory once, in the DSN, and the product's DuckDB boot must do the same.

## Recommendation

- **Budget:** publish 1.5 GB as the design objective at the `busy` workload. The baseline settings peaked at 1,001 MB over seven days, which leaves about 50% headroom.
- **DuckDB boot settings for the product:** unchanged from phase 0: `memory_limit = 256MB`, `threads = 2`, `preserve_insertion_order = false`, and a temp directory inside the data dir, set once at database creation. No allocator settings.
- **Revisit later:** `allocator_background_threads` saves about 200 MB and is the only lever found. It can be adopted if a run that isolates its effect on real-time commit latency shows the regression was not its doing. A lower `memory_limit` needs a different flush shape first.
