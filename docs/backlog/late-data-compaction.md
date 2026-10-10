# Late data leaves many small Parquet files per hour

**Status: open**

## Problem

Every flush of an hour that already has a file writes another file
(`internal/flush/flush.go`, `flushHour`), so late rows add one file per
maintenance tick that sees them. Phase 0 measured that 1% late logs triples
the log file count. Queries read every file the manifest lists for a range
(`internal/query/source.go`), so file count grows query cost and manifest
size.

## Proposed fix

A compaction job merges an hour's files once the hour is "closed" (no new
rows for some window, say a day), writing one file with the hour's greatest
cutoff and swapping manifest rows in one SQLite transaction before unlinking
the old files. Unlinking must take `query.Gate.Exclusive` like retention
does, and reconcile must tolerate a crash between the swap and the unlinks.
When an hour is closed is an open decision in `docs/DESIGN.md`.

## Origin

Out of scope in `docs/plans/2026-10-10-1027-logs-slice.md` (10 October 2026).
