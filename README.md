# Tracelet

**Lightweight observability for your projects.**

Self-hosted logs, traces, metrics, errors, and alerts for personal projects in one
executable with one data directory. See [docs/VISION.md](docs/VISION.md) for the
direction and [docs/DESIGN.md](docs/DESIGN.md) for the technical design.

Status: early scaffolding. Nothing described in the vision is implemented yet.

## Development

Tools are pinned in `.mise.toml` and installed with [mise](https://mise.jdx.dev).
Tasks are defined in `rite.edn` and run with [rite](https://github.com/abogoyavlensky/rite).

```sh
mise install        # Go, Node, golangci-lint, rite
rite setup          # the above plus frontend dependencies
rite tasks          # list every task

rite check          # fmt-check, lint, test: what CI runs
rite build          # frontend + server binary into bin/tracelet
./bin/tracelet serve --data-dir ./data
```

For day-to-day work run the server and the frontend dev server side by side:

```sh
rite dev            # Go server on :8080
rite web-dev        # Vite on :5173, proxies /api to :8080
```

## Layout

| Path | Purpose |
| --- | --- |
| `cmd/tracelet` | Entry point: `serve` and `version` commands |
| `internal/app` | Composition root: config, wiring, lifecycle |
| `internal/httpapi` | HTTP API handlers under `/api/` |
| `internal/web` | Serves the embedded frontend build |
| `web` | Frontend source: React, TypeScript, Vite, Tailwind, TanStack Query |
| `docs` | Vision and design documents |

The frontend builds into `internal/web/dist`, which the Go binary embeds. Hashed
files under `/assets/` are served with a one-year immutable cache; every other
path returns `index.html`.
