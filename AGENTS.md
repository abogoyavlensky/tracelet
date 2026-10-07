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
- API routes live under `/api/`; everything else is the single-page frontend.
- New dependencies, Go or npm, are a decision: mention them explicitly.
