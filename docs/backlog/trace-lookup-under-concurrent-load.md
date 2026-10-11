# Trace lookup by ID has not been measured under concurrent query load

**Status: open**

## Problem

A full-window trace lookup scans the span files of its range: there is no
trace index (`docs/DESIGN.md`, "Trace lookup by ID"). Alone, on a 6 vCPU VM, a
7-day lookup over `busy`-shaped files measured 118 ms p50 and 151 ms p95
(`docs/spikes/trace-lookup.md`). Phase 0's numbers under concurrent query load
were 1.7 times its numbers alone (1.81 s against 1.03 s p95), and the gate
admits four queries at once (`internal/query/gate.go`). The same factor
applied to 151 ms is comfortably inside 1 s, but nothing has measured it, and
the phase 0 machine (4 vCPU, 7.6 GB) was smaller than the one that produced
151 ms.

The measurement needs a load generator: concurrent span ingestion plus other
investigation queries holding gate slots while lookups run. The product no
longer carries phase 0's generator.

## Proposed fix

Measure first. In the packaging and dogfood step, run
`TestTraceLookupSevenDays` (`internal/query/tracelookup_bench_test.go`) on the
target VM size while three goroutines run 7-day span and log searches through
their own snapshots, and record p50 and p95 next to the single-client numbers
in `docs/spikes/trace-lookup.md`. About 40 lines added to the benchmark.

Only if p95 nears 1 s, look at an index. What was tried, and why it lost:
sorting span files by trace ID with `ROW_GROUP_SIZE 10000` pruned correctly
but cost more in footer reads across 168 files than the scan it saved (489 ms
p95). DuckDB's `parquet_metadata_cache` brought that layout level with the
time-sorted one on a 24-hour check, not ahead. A per-hour sidecar of trace ID
to file and row group would be read through the same footer path. Measure
before adding files to flush, reconcile, back up, and retain.

## Origin

Deferred by the traces slice plan,
`docs/plans/2026-10-11-0858-traces-slice.md`, Task 11 (11 October 2026), and
named in `docs/DESIGN.md` as the condition for revisiting the trace index.
