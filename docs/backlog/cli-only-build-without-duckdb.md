# The CLI cannot be built without DuckDB

**Status: open**

## Problem

`cmd/tracelet` links the server and the client together, and the server
pulls DuckDB's static library through `internal/duckdb`, so every build
needs CGO and a supported glibc. `docs/DESIGN.md` ("Platforms") promises a
CLI-only executable for macOS, Linux, and Windows without DuckDB.

## Proposed fix

`internal/cli` imports nothing from storage, so a second entry point
(`cmd/tracelet-cli`, or a build tag on `cmd/tracelet` that leaves out
`serve`) builds with `CGO_ENABLED=0`. Add it to CI and the release
workflow; check with `go list -deps` that no DuckDB package is linked.

## Origin

Out of scope in `docs/plans/2026-10-10-1027-logs-slice.md` (10 October 2026).
