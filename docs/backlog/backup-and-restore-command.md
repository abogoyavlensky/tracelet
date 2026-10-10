# There is no backup command

**Status: open**

## Problem

`docs/DESIGN.md` specifies `tracelet backup`: force a flush, snapshot the
SQLite database with `VACUUM INTO`, and copy exactly the files that snapshot
lists. None of it exists. The spike's `internal/backup` (last present at
`52c70cc`) was not ported. Copying a live data directory is unsupported, so
today there is no safe way to back up.

## Proposed fix

Port the spike's backup onto `internal/sqlite` (the manifest now lives in
`tracelet.sqlite` with projects and tokens, so one `VACUUM INTO` covers
all application state). It must run inside the server or talk to it, since
only `serve` opens databases: an admin API endpoint that streams a tar, or a
maintenance job writing to a directory. Holding `query.Gate.Exclusive` is
not needed for copying (Parquet files are immutable) but retention must not
unlink files mid-copy, so take the gate or pause retention for the duration.

## Origin

Out of scope in `docs/plans/2026-10-10-1027-logs-slice.md` (10 October 2026).
