# Spans are not ingested, and trace lookup has no index

**Status: done**
Landed in the traces slice, [docs/plans/2026-10-11-0858-traces-slice.md](../plans/2026-10-11-0858-traces-slice.md) (branch `traces-slice`, `ac75571` through the commit "Document the traces slice and record its decisions"): `/v1/traces`, span deduplication in the writer, trace lookup, and span search. No trace index was built: the 7-day lookup measured 151 ms p95 on time-sorted span files, and the planned trace-sorted layout was slower ([docs/spikes/trace-lookup.md](../spikes/trace-lookup.md)).

## Problem

The `spans` hot table exists (`internal/duckdb/store.go`) and flush,
retention, and `Snapshot.Source` handle it, but nothing writes to it: there
is no `POST /v1/traces`, no span conversion in `internal/otlp`, and no
trace query. Phase 0 measured an unbounded 7-day trace-by-ID scan at 1.03 s
p95 alone and 1.8 s under load, over the 1 s target, so a trace index is
needed as soon as traces ship (`docs/DESIGN.md`, "Trace lookup by ID").

## Proposed fix

Mirror the logs path: `otlp.ConvertSpans` into `telemetry.Span`, the same
ingester (batches already carry spans), a `/v1/traces` handler, span
deduplication on trace ID plus span ID within the hot window as the design
specifies. The trace index format and whether it is written at flush or
compaction is still an open decision in `docs/DESIGN.md`.

## Origin

Out of scope in `docs/plans/2026-10-10-1027-logs-slice.md` (10 October 2026).
