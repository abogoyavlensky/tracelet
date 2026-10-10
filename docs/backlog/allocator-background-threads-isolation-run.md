# allocator_background_threads has not been measured in isolation

**Status: open**

## Problem

The memory follow-up (`docs/spikes/memory.md`) found that DuckDB's
`allocator_background_threads` cut peak memory by about 200 MB, but its
real-time run also changed `GOMEMLIMIT` and added sampling, and commit p99
doubled. It is not known which change caused the regression, so the setting
stays off and the memory budget is 1.5 GB. The product store
(`internal/duckdb/store.go`) no longer has the knob: `Limits` keeps only
`MemoryLimit` and `Threads`.

## Proposed fix

Re-add the setting behind a config flag, run the `busy` workload in real
time with only that change against a baseline, and compare commit p99 and
peak RSS. The spike's experiment scripts are at commit `52c70cc`. Adopt the
setting if p99 holds; otherwise record the result and close this entry.

## Origin

Open decision in `docs/DESIGN.md`; listed for the backlog by `docs/plans/2026-10-10-1027-logs-slice.md`
(10 October 2026).
