# Memory follow-up results (phase 0b)

Date: 8 October 2026. Plan: [2026-10-08-2143-memory-followup](../plans/2026-10-08-2143-memory-followup.md). Code: `spike/storage/` on branch `memory-followup`.

**Draft:** attribution and single-variable runs only. Confirmation runs E6 and E7 are pending.

## Environment

Same machine and toolchain as the [storage spike](storage.md#environment). Every run is `spike run --profile busy --threads 2` on a fresh data dir, from `spike/storage/scripts/memory-experiments.sh`, one at a time. Peak RSS is `VmHWM`, cross-checked with `/usr/bin/time -v`. Each run also samples memory once per wall-clock second: process RSS, the Go runtime's resident estimate (`Sys - HeapReleased`), DuckDB's `duckdb_memory()` by tag, and the residual that neither accounts for. Memory is in decimal MB.

## Where E0's memory goes

E0 is the baseline: one simulated day with `memory_limit = 256MB` and allocator defaults. Its highest sample matches `VmHWM` exactly (ratio 1.00), so the breakdown below is the peak itself, not a neighbour of it.

| At the peak | MB |
| --- | ---: |
| RSS | 874 |
| Go resident estimate | 21 |
| DuckDB, by tag | 256: `COLUMN_DATA` 126, `ORDER_BY` 107, `ALLOCATOR` 19, `BASE_TABLE` 4 |
| Residual | 597 |

The peak sample was taken during a flush.

- **The residual dominates.** It is about 600 MB at the peak and about 400 MB at the median, while Go stays near 20 MB and DuckDB never reports more than its 256 MB limit. The residual is memory DuckDB's bundled jemalloc holds without DuckDB counting it. At the end of the run DuckDB accounts for 9 MB while the residual is 655 MB.
- **The residual has a floor of 80 to 90 MB.** That is the minimum over the run and roughly the first sample. It matches mapped code in an 80 MB static binary.
- **The shape is a plateau with spikes.** The median RSS is 550 to 590 MB throughout the day, with spikes to 750 to 870 MB. The spikes are not tied to flushes: only 5 of the 20 highest samples were taken during a flush. The others come between flushes, when the 60-second commits build up `IN_MEMORY_TABLE` for the two hot hours.

## Single-variable runs

Each run changes one thing against E0. All five completed runs have 0 sample errors and ingested the same 17.28 M rows into 118 files.

| Run | Peak RSS (MB) | RSS at peak sample (MB) | Go resident at peak (MB) | DuckDB at peak (MB) | Residual at peak (MB) | Peak in flush | HWM / peak sample | Flush p95 (ms) | Commit p99 (ms) | Worst query p99 (ms) |
| --- | ---: | ---: | ---: | ---: | ---: | --- | ---: | ---: | ---: | ---: |
| e0-baseline | 874 | 874 | 21 | 256 | 597 | yes | 1.00 | 1139 | 2293 |  |
| e1-flush-threshold | 886 | 871 | 24 | 256 | 591 | no | 1.02 | 1137 | 2296 |  |
| e2-background-threads | 741 | 659 | 19 | 134 | 506 | yes | 1.12 | 1157 | 2282 |  |
| e3-memory-limit-128 | failed | | | | | | | | | |
| e4-row-group-30k | 828 | 807 | 21 | 255 | 531 | no | 1.03 | 1367 | 2262 |  |
| e5-unsorted | 811 | 803 | 23 | 256 | 524 | no | 1.01 | 707 | 2279 |  |

- **E1, `allocator_flush_threshold = 16MB`:** no effect on the peak, which is 12 MB higher and within noise. The median RSS outside flushes falls from 548 to 505 MB.
- **E2, `allocator_background_threads = true`:** peak RSS 741 MB, 133 MB below E0, and the median outside flushes falls from 548 to 392 MB. Flush p95 is 1.6% slower and commit p99 is unchanged. Its highest sample is 12% below `VmHWM`, so the breakdown at its peak is **unknown**. The sampled series shows the residual median falling from about 405 to about 250 MB, so the setting acts where E0 said it should, on allocator retention.
- **E3, `memory_limit = 128MB`:** fails. The first hourly flush runs out of memory sorting an hour of logs in `COPY ... ORDER BY ts` ("failed to allocate data of size 5.0 MiB (117.3 MiB/122.0 MiB used)"). Two attempts failed the same way after about 30 s. With this flush, 128 MB is not a usable setting.
- **E4, `ROW_GROUP_SIZE 30000`:** 46 MB lower, under the 50 MB bar, and flush p95 is 20.0% slower, right at the regression limit. Rejected.
- **E5, `COPY` without `ORDER BY ts` (diagnostic):** 63 MB lower, and flush p95 falls by 38%. That is the sort's share. It is not a candidate, because sorted files are a design property.

E1 to E5 come from one run each. The storage spike's one-day run of the same configuration reached 932 MB against E0's 874 MB, so run-to-run spread is probably tens of MB. That puts E1, E4, and E5 within noise and leaves E2 clearly outside it.

## Chosen combination

Only E2 cut peak RSS by at least 50 MB without a regression over 20%. The confirmation runs E6 and E7 therefore use `--allocator-background-threads` alone, with `GOMEMLIMIT=192MiB`.

Even E2 alone leaves the one-day peak at 741 MB, above the 700 MB target. The seven-day confirmation will show whether it holds there.

## Harness fixes found on the way

- `store.Open` re-ran `SET GLOBAL temp_directory` on every new pooled connection. Once DuckDB has spilled, that statement fails ("Cannot switch temporary directory after the current one has been used"), so every new connection after a spill failed. The 256 MB runs never hit it; E3 hit it at once. The temp directory is now set once, in the DSN. The product's DuckDB boot needs the same.
