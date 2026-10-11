# Tracelet

**Lightweight observability for your projects.**

Self-hosted logs, traces, metrics, errors, and alerts for personal projects in one
executable with one data directory. See [docs/VISION.md](docs/VISION.md) for the
direction and [docs/DESIGN.md](docs/DESIGN.md) for the technical design.

Status: early. Tracelet ingests **logs and traces** so far, over OTLP/HTTP with
protobuf encoding, and searches them through the HTTP API and the CLI. Metrics,
the UI, and everything else in the vision come later.

## Quickstart

Start the server. On first start it creates an admin token and prints it once:

```sh
tracelet serve --data-dir ./data
# admin token: tl_...
# Save it now: it will not be shown again.
```

In a container, set `TRACELET_ADMIN_TOKEN=tl_<at least 16 characters>` to seed
the admin token instead. With the admin token, create a project and an ingest
token for your app:

```sh
export TRACELET_TOKEN=tl_...             # the admin token from above
tracelet projects create shop --name "Shop"
tracelet tokens create --project shop --scope ingest --name "api server"
# prints tl_... once: the ingest token
```

Point any OpenTelemetry SDK at Tracelet. The SDK appends `/v1/logs` and
`/v1/traces` itself:

```sh
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:8080
OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
OTEL_EXPORTER_OTLP_HEADERS="Authorization=Bearer tl_..."   # the ingest token
OTEL_SERVICE_NAME=api
OTEL_RESOURCE_ATTRIBUTES="deployment.environment.name=prod,service.version=1.4.0"
```

Tracelet files every record under three resource attributes: `service.name`
(`unknown_service` when missing), `deployment.environment.name` (`default` when
missing), and `service.version`. OTLP/JSON and gRPC are not supported yet.

An SDK retries a whole request when it loses a response, so a span that is
already stored is dropped and counted as `duplicate_spans` on health. That
check covers the current and previous hour, which are still in DuckDB; a retry
arriving after its hour was flushed to Parquet is stored twice. Logs are not
deduplicated. Span `trace_state` and `flags` are not stored.

Search logs:

```sh
tracelet logs search --project shop --service api --level error --last 30m
tracelet logs search --project shop --attr exception.type:Timeout --json
tracelet services --project shop --last 24h    # log and span counts per service
```

Find traces and follow one into its logs:

```sh
# root spans stand in for a trace list; slow failing requests of the last hour
tracelet spans search --project shop --service api --root --status error --min-duration 500ms --last 1h
tracelet traces get 4bf92f3577b34da6a3ce929d0e0e4736 --project shop
# the trace's start and end bound its logs; --to is exclusive
tracelet logs search --project shop --trace-id 4bf92f3577b34da6a3ce929d0e0e4736 \
  --from 2026-10-11T09:14:02Z --to 2026-10-11T09:14:04Z
```

`traces get` prints the spans as a tree and looks back 7 days unless
`--from`/`--to` or `--last` say otherwise; its `--json` output carries `start`
and `end`. A trace whose root span was never exported does not show up in
`spans search --root`; search its other spans instead.

A `read` token created with `--scope read --project shop` limits the CLI to one
project. The API is described at `/api/v1/openapi.yaml`; health and counters are
at `/api/v1/health`.

Server settings are flags of `tracelet serve` with `TRACELET_*` environment
defaults: `--retention-days` (30), `--max-telemetry-bytes` (0, no cap),
`--disk-floor-bytes` (1 GB; ingestion pauses below it), `--duckdb-memory-limit`
(256MB), `--duckdb-threads` (2), `--maintenance-interval` (1m).

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
| `cmd/tracelet` | Entry point: `serve`, `version`, and the CLI commands |
| `api` | The OpenAPI description, embedded and served |
| `internal/app` | Composition root: config, wiring, lifecycle, maintenance loop |
| `internal/httpapi` | HTTP handlers: OTLP at `/v1/`, the API at `/api/v1/` |
| `internal/cli` | The CLI, a client of the HTTP API |
| `internal/project` | Projects and scoped tokens |
| `internal/otlp` | OTLP logs and traces decoding into telemetry rows |
| `internal/ingest` | Bounded queue and the single batching writer loop |
| `internal/duckdb` | Hot DuckDB tables and the writer |
| `internal/flush` | Hourly flush to Parquet, reconcile, retention |
| `internal/manifest` | The Parquet file manifest |
| `internal/query` | Consistent snapshots, logs and span search, trace lookup, services |
| `internal/sqlite` | `tracelet.sqlite`: migrations, project and token store |
| `internal/telemetry` | Row types for the three signals |
| `internal/web` | Serves the embedded frontend build |
| `web` | Frontend source: React, TypeScript, Vite, Tailwind, TanStack Query |
| `docs` | Vision, design, plans, spike results, and the backlog |

The frontend builds into `internal/web/dist`, which the Go binary embeds. Hashed
files under `/assets/` are served with a one-year immutable cache; every other
path returns `index.html`.
