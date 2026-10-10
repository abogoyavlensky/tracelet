# Tracelet: notes for coding agents

Read `docs/VISION.md` for what the product is and is not, and `docs/DESIGN.md`
for the technical decisions. Do not reopen decisions marked **decided** there
without saying so.

## Tasks

All project tasks go through `rite` (`rite.edn`); run `rite tasks` to list them.
Tools come from `.mise.toml` via `mise install`. Before finishing any change run
`rite check`, which is exactly what CI runs: format check, lint, and tests for
both Go and the frontend.

## Layout and conventions

- Go: packages by domain under `internal/`, one composition root in
  `internal/app`, thin HTTP handlers in `internal/httpapi`, standard library
  first, explicit wiring, no globals. Tests use testify and live next to the
  code as external test packages.
- Frontend: `web/` is a Vite project (React, TypeScript, Tailwind v4, TanStack
  Query). It builds into `internal/web/dist`, which Go embeds. Keep
  `internal/web/dist/.gitkeep`; the Vite config recreates it after each build.
- API routes live under `/api/v1/`; OTLP receivers live under `/v1/` (SDKs
  append `/v1/logs` to the endpoint themselves); everything else is the
  single-page frontend. Every route is described in `api/openapi.yaml`, and a
  test fails when a registered pattern is missing from it.
- Packages: `internal/app` (wiring, maintenance loop), `internal/httpapi`,
  `internal/cli`, `internal/project` (projects, tokens), `internal/otlp`
  (decode, convert), `internal/ingest` (queue, single writer loop),
  `internal/duckdb` (hot store, writer), `internal/flush` (flush, reconcile,
  retention), `internal/manifest`, `internal/query` (gate, snapshot, search),
  `internal/sqlite` (`tracelet.sqlite`, migrations), `internal/telemetry`.
- Every telemetry query runs through a `query.Snapshot` and its `Source`:
  that is what keeps a concurrent flush from showing an hour twice or not at
  all. Never query hot tables plus Parquet files any other way. Anything that
  unlinks Parquet files holds `query.Gate.Exclusive`.
- Telemetry rows store the project ID; the API and CLI speak slugs.
- SQLite schema changes are new files in `internal/sqlite/migrations/`,
  applied in name order; never edit an applied migration.
- New dependencies, Go or npm, are a decision: mention them explicitly.

## Backlog

`docs/backlog/` holds known issues and ideas that are not being worked on yet.

- One file per issue, named after it (`format-selection-column-offset.md`).
- Each file starts with `**Status: open**`. When a plan is written for it,
  change the status to `**Status: planned**` with a `Plan: docs/plans/...`
  line under it; when the work ships, change it to `**Status: done**` with a
  line saying where it landed. Never delete an entry.
- "What's in the backlog?" means the open files — list every file whose status
  is not done, with its title.
- Adding an entry is its own commit (`Backlog: <what>`), never mixed with code.
