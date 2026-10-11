# Trace lookup results (phase 1, slice 2)

Date: 11 October 2026. Plan: [2026-10-11-0858-traces-slice](../plans/2026-10-11-0858-traces-slice.md), Task 11. Code: `internal/query/tracelookup_bench_test.go`.

Phase 0 measured a 7-day trace-by-ID lookup at 0.94 s p50 and 1.03 s p95, just over the 1 s target. The slice plan proposed making the span Parquet file its own trace index: sort each hour by trace ID in row groups of 10,000, so row-group statistics prune all but one row group per file. This benchmark measured that layout against the existing one before keeping it.

- **Result:** the existing layout (time-sorted, DuckDB's default row groups) looks up a trace in **151 ms p95** over 7 days. The trace-sorted layout passes too, at 489 ms p95, but it is about 3.5 times slower, flushes about 20% slower, and writes about 15% more bytes.
- **Decision:** span files stay sorted by time, and there is no trace index. Phase 0's 1.03 s does not reproduce on the product's query path.

## What was measured

`TestTraceLookupSevenDays` generates 168 hours of spans shaped like the `busy` workload (50 spans/s), each hour with one `INSERT ... SELECT` into the hot table. Each hour has 180,000 spans in 36,000 traces of five spans, across 10 services, with random 32-hex trace IDs. Each hour is flushed through the real `flush.Flusher` before the next is generated, so the files and the manifest are what the product writes. It then looks up 100 random trace IDs. Each lookup opens its own `query.Snapshot` and runs `query.TraceGet` with a range a little over 7 days, so all 168 files are candidates. Every lookup returned its 5 spans.

The run uses the product's DuckDB settings: 256 MB `memory_limit` and 2 threads. The files are freshly written, so they are in the page cache. This is a single-client measurement: phase 0's slower case, the same lookup under concurrent query load (1.8 s p95), needs a load generator the product does not have, and is left to the packaging and dogfood step.

## Machine

A KVM VM with 6 vCPUs (Intel Haswell, no TSX), 11 GB RAM, and a QEMU virtual disk. Go 1.27.0, `duckdb-go` v2.10506.0 (DuckDB 1.5.6). Phase 0 ran on a smaller VM of the same kind: 4 vCPUs and 7.6 GB RAM.

## Numbers

| Span file layout | Files | Bytes | Flush per hour p50 / p95 | Lookup p50 | Lookup p95 | Lookup max |
| --- | ---: | ---: | --- | ---: | ---: | ---: |
| Phase 0 (spike code, smaller VM) | 168 | | about 2 s per signal and hour | 940 ms | 1,025 ms | 1,059 ms |
| Time-sorted, default row groups (**kept**) | 168 | 670 MB | 465 / 551 ms | 118 ms | 151 ms | 171 ms |
| Trace-sorted, `ROW_GROUP_SIZE 10000` | 168 | 768 MB | 563 / 689 ms | 409 ms | 489 ms | 514 ms |

DuckDB rounds `ROW_GROUP_SIZE 10000` up to 10,240 rows, a multiple of its 2,048-row vector, so a trace-sorted hour had 18 row groups. A time-sorted hour has 2.

## Why sorting did not help

Pruning works: `EXPLAIN ANALYZE` shows the `trace_id` filter pushed into the Parquet scan, and over 24 trace-sorted files a lookup straight from `read_parquet` took about 25 ms. What sorting cannot remove is the per-file cost. Each query reads the footer of every file in the range, and a file with 18 row groups carries nine times the column statistics of one with 2. Over 168 files that costs more than scanning the `trace_id` column of two row groups per file. Phase 0's file-level cost was higher, so it measured a slower scan.

DuckDB's `parquet_metadata_cache` setting keeps footers between queries. On a 24-hour check it brought the trace-sorted layout from 69 ms to 41 ms per lookup, about level with the time-sorted layout's 33 to 38 ms, not ahead of it. It is not adopted: the cache would only bring the sorted layout level with what the time-sorted layout already does.

## When to look again

The trace index stays unneeded while a full-window lookup is well inside 1 s. Revisit it when the dogfood step measures lookups under concurrent query load, when retention or span rates grow well past `busy`, or if late-data compaction changes how many files an hour has.

## Reproduce

Point `TMPDIR` at a real disk; the files take under 1 GB and `/tmp` is often a small tmpfs.

```sh
TMPDIR=$HOME/.cache/tracelet-bench TRACELET_BENCH=1 \
  go test ./internal/query/ -run TraceLookupSevenDays -v -count=1 -timeout 60m
```

`TRACELET_BENCH_HOURS` changes the 168 hours. To measure the trace-sorted layout, make `copyStatement` in `internal/flush/flush.go` write spans with `ORDER BY trace_id, ts` and `ROW_GROUP_SIZE 10000`, as commit `ef257be` did.
