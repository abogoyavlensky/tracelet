# Tracelet

**Lightweight observability for your projects.**

Self-hosted logs, traces, metrics, errors, and alerts for personal projects in one
executable with one data directory. See [docs/VISION.md](docs/VISION.md) for the
direction and [docs/DESIGN.md](docs/DESIGN.md) for the technical design.

Status: early. Tracelet ingests **logs, traces, and metrics** so far, over
OTLP/HTTP with protobuf encoding, and explores them through a web UI, the HTTP
API, and the CLI. Errors, alerts, and everything else in the vision come later.

## Install

The CLI, a small static binary for Linux and macOS (amd64 and arm64), comes
from a [release](https://github.com/abogoyavlensky/tracelet/releases):

```sh
mise use github:abogoyavlensky/tracelet   # or unpack tracelet-<os>-<arch>.tar.gz
```

It talks to a server over HTTP; point it at one with `TRACELET_URL`. The
server runs from the Docker image, or from `tracelet-server-linux-<arch>.tar.gz`
(glibc 2.39 or newer), which is also a full CLI:

```sh
docker run -p 8080:8080 -v tracelet-data:/app/data \
  -e TRACELET_ADMIN_TOKEN=tl_<at least 16 characters> \
  ghcr.io/abogoyavlensky/tracelet
```

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

Point any OpenTelemetry SDK at Tracelet. The SDK appends `/v1/logs`,
`/v1/traces`, and `/v1/metrics` itself:

```sh
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:8080
OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
OTEL_EXPORTER_OTLP_HEADERS="Authorization=Bearer tl_..."   # the ingest token
OTEL_SERVICE_NAME=api
OTEL_RESOURCE_ATTRIBUTES="deployment.environment.name=prod,service.version=1.4.0"
```

Tracelet files every record under three resource attributes: `service.name`
(`unknown_service` when missing), `deployment.environment.name` (`default` when
missing), and `service.version`. Records sent with the older
`deployment.environment` key, which semantic conventions before v1.27 used, file
under its value when `deployment.environment.name` is absent. OTLP/JSON and gRPC are not supported yet.

An SDK retries a whole request when it loses a response, so a span that is
already stored is dropped and counted as `duplicate_spans` on health. That
check covers the current and previous hour, which are still in DuckDB; a retry
arriving after its hour was flushed to Parquet is stored twice. Logs are not
deduplicated. Span `trace_state` and `flags` are not stored. Metric points are
deduplicated the same way and counted as `duplicate_points`.

Metrics may use cumulative or delta temporality; both give the same rates.
Counter resets are detected per series, and every SDK process is its own series,
so replicas never mix and the first export after a restart is counted.
Gauges, sums, and explicit-bucket histograms are stored; exponential
histograms, summaries, and delta up-down counters are rejected with a reason in
the export response. Exemplars are not stored.

Open the server in a browser (`http://localhost:8080`) and sign in with a
token. Create one for each browser, so a lost device can be revoked without
touching the others; a `read` token is enough to explore:

```sh
tracelet tokens create --scope read --name laptop
# prints tl_... once: paste it on the sign-in page
```

The browser keeps the token in an `HttpOnly` cookie. The admin token works too,
but it is better kept for the CLI. The UI has a logs explorer, a traces list,
a trace page with the waterfall, span details, and the trace's logs, and a
metrics catalog with a chart per metric; every view's filters and time range
live in its URL. The logs explorer has a Live toggle for a relative time range:
it polls every 2 seconds and merges new records into the list without moving
the selected record or the scroll position.

On a phone, use "Add to Home Screen" (iOS) or "Install app" (Android) to open
Tracelet as an app.

Search logs from the CLI:

```sh
tracelet logs search --project shop --service api --level error --last 30m
tracelet logs search --project shop --attr exception.type:Timeout --json
# print the newest page, then new logs as they arrive, until Ctrl-C (--json: one object per line)
tracelet logs search --project shop --level warn --last 15m --follow
tracelet services --project shop --last 24h    # log, span, and point counts per service
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

Find metrics and chart them in the terminal:

```sh
tracelet metrics list --project shop --last 1h
tracelet metrics attributes http.server.request.duration --project shop
# p95 latency per route; counters default to a per-second rate
tracelet metrics query http.server.request.duration --project shop --aggregate p95 --group-by http.route --last 6h
tracelet metrics query orders.created --project shop --aggregate increase --step 1h --last 24h
```

A query returns one value per step and group: counters take `rate` or
`increase`, gauges and up-down counters `avg`, `min`, `max`, or `sum`, and
histograms `p50`-style quantiles, `rate`, or `avg`. Empty steps stay empty
rather than reading zero, and the last step can read low until it completes.

Keep dashboards in files and apply them, from a terminal or CI, with an admin
token:

```sh
cat > shop-overview.json <<'JSON'
{
  "uid": "shop-overview",
  "title": "Shop overview",
  "last": "6h",
  "panels": [
    {"title": "Orders per second", "type": "timeseries",
     "metric": {"name": "orders.created", "aggregate": "rate", "group_by": "service"}},
    {"title": "Orders in range", "type": "stat", "reduce": "sum",
     "metric": {"name": "orders.created", "aggregate": "increase"}},
    {"title": "p95 by route", "type": "table",
     "metric": {"name": "http.server.request.duration", "aggregate": "p95", "group_by": "http.route"}}
  ]
}
JSON
tracelet dashboards apply --project shop --file shop-overview.json   # applying again changes nothing
tracelet dashboards export shop-overview --project shop             # the file back, for git
tracelet dashboards list --project shop
```

Dashboards made or edited in the web UI export the same way. A read token
views dashboards; only an admin token changes them.

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

rite check          # fmt-check, lint, test, check-cli: what CI runs
rite build          # frontend + server binary into bin/tracelet
./bin/tracelet serve --data-dir ./data
```

For day-to-day work run the server and the frontend dev server side by side:

```sh
rite dev            # Go server on :8080
rite web-dev        # Vite on :5173, proxies /api to :8080
```

Releases, the Docker image, and the master deploy are described in
[docs/operations.md](docs/operations.md).

## Layout

| Path | Purpose |
| --- | --- |
| `cmd/tracelet` | Entry point: `serve`, `version`, and the CLI commands |
| `api` | The OpenAPI description, embedded and served |
| `internal/app` | Composition root: config, wiring, lifecycle, maintenance loop |
| `internal/httpapi` | HTTP handlers: OTLP at `/v1/`, the API at `/api/v1/` |
| `internal/cli` | The CLI, a client of the HTTP API |
| `internal/project` | Projects and scoped tokens |
| `internal/otlp` | OTLP logs, traces, and metrics decoding into telemetry rows |
| `internal/ingest` | Bounded queue and the single batching writer loop |
| `internal/duckdb` | Hot DuckDB tables and the writer |
| `internal/flush` | Hourly flush to Parquet, reconcile, retention |
| `internal/manifest` | The Parquet file manifest |
| `internal/query` | Consistent snapshots, logs and span search, trace lookup, metric queries, services |
| `internal/sqlite` | `tracelet.sqlite`: migrations, project and token store |
| `internal/telemetry` | Row types for the three signals |
| `internal/web` | Serves the embedded frontend build |
| `web` | Frontend source: React, TypeScript, Vite, Tailwind, TanStack Query |
| `docs` | Vision, design, plans, spike results, and the backlog |

The frontend builds into `internal/web/dist`, which the Go binary embeds. Hashed
files under `/assets/` are served with a one-year immutable cache; every other
path returns `index.html`.
