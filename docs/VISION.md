# Tracelet

**Lightweight observability for your projects.**

Status: initial project vision, based on the design discussion of 7 October 2026. This document records the chosen direction and distinguishes it from proposed implementation details. It does not claim that features or performance targets have been implemented or validated.

## Purpose

Tracelet is a self-hosted observability application for personal web and mobile projects and their backend services. It brings metrics, traces, logs, and alerts together in a small, useful system that is easy to run and maintain.

The goal is one executable, one process, one data directory, and a useful web interface. A single Docker container with one persistent volume is an equally supported deployment experience. No external database, message broker, or required cloud account should be necessary.

Tracelet should answer everyday questions: Is my application healthy? What changed after deployment? Why was this request slow? Which errors keep happening? Did a background job stop running?

It should work equally well through its UI, HTTP API, and small CLI companion. Coding agents are first-class API and CLI users. **MCP is explicitly out of scope.**

## Audience and boundaries

The initial audience is a developer running a handful of projects on modest infrastructure. The product should be reliable enough for daily use, with flexibility for common application monitoring needs.

It is not initially a distributed observability platform, enterprise multi-tenant service, or complete replacement for every feature of Grafana, Sentry, and ClickStack. Enterprise SSO, complex permissions, clustering, session replay, profiling, and product-analytics funnels are outside the initial scope.

## Principles

- Keep installation, configuration, backup, and upgrades understandable.
- Prefer established telemetry formats and instrumentation over proprietary SDKs.
- Provide useful defaults before requiring users to build dashboards.
- Make logs, traces, and metrics easy to investigate together.
- Make data loss, sampling, query truncation, and ingestion delays visible.
- Bound memory, query concurrency, queues, and disk consumption.
- Use durable state and safe retries where work must survive restarts.
- Add infrastructure only when its benefit justifies its complexity.
- Maintain one application/query model shared by the UI, API, CLI, and alerts.

## Architecture

| Component | Responsibility |
| --- | --- |
| Go application | HTTP server, OTLP ingestion, API, workers, scheduler integration, embedded frontend |
| DuckDB | Metrics, spans, logs, analytical queries, eventual rollups |
| SQLite | Projects, credentials, dashboards, alert rules/state, queue and scheduling state |
| Background job library | Alert evaluation, notification retries, retention, rollups, recovery of unfinished work |
| React frontend | Dashboards, exploration, trace details, configuration |
| CLI | Remote management and retrieval through the same HTTP API |

**Go is the chosen backend direction.** Rust was considered but has no established advantage sufficient to change that choice. DuckDB performs the main analytical work.

SQLite is included for convenient transactional application state and background-job management. It is **not an intermediate telemetry store**. Telemetry is written directly to DuckDB.

River with its SQLite driver is the preferred queue candidate, subject to validation. The documentation reviewed during planning described SQLite support as experimental. Pin the selected version and verify its recovery, migration, concurrency, and scheduling behaviour. Do not assume that every scheduling or queue feature is available in the chosen edition.

Run workers in the same application process. Keep SQLite transactions short and never hold one open while running a long DuckDB query or making a network request. Jobs affecting DuckDB must tolerate retries because transactions do not span both databases.

## Stack and development workflow

| Area | Direction |
| --- | --- |
| Backend | Go, standard HTTP primitives, explicit SQL |
| Telemetry transport | OTLP/HTTP first, protobuf and gzip; gRPC later if needed |
| Frontend | React, TypeScript, Vite, TanStack Query |
| Charts | Apache ECharts as the initial candidate |
| Dashboard layout | Simple grid first; evaluate react-grid-layout for draggable/resizable panels |
| API description | OpenAPI for the management and query API |
| Tool versions/system dependencies | mise |
| Project task management | abogoyavlensky/rite |

Embed built frontend assets in the server executable. There should be no Node.js runtime requirement in production. DuckDB introduces native build dependencies through CGO, so validate release packaging early. A single executable does not imply a tiny or universally static binary.

Prioritise Linux amd64 and arm64 server builds; determine additional platforms based on actual use. A separate lightweight CLI build should be available if bundling DuckDB makes the combined executable inconvenient on client machines.

## Ingestion and durability

The initial telemetry ingestion path is:

1. Authenticate and validate an OTLP request, including size and project limits.
2. Place accepted work in a bounded in-memory queue.
3. Combine work into small batches and write a DuckDB transaction.
4. Return success only after the relevant batch commits.

Queued in-memory data has not yet been acknowledged. Retry-capable exporters can retry requests that fail before acknowledgement. A lost response after a successful commit can still lead to duplicate delivery: this is not an exactly-once guarantee. Define signal-specific duplicate handling, especially for delta metrics.

When overloaded, apply backpressure and return protocol-appropriate retryable failures. Do not silently acknowledge data that was discarded. Document partial acceptance and unsupported signal types accurately.

A SQLite telemetry inbox is a deferred option, only if measurements demonstrate a need to absorb bursts durably or acknowledge receipt independently of DuckDB commit latency. It is not part of the initial design.

## Core signals

### Metrics

Start with a documented OTLP subset:

| User concept | Initial representation | Required semantics |
| --- | --- | --- |
| Count/counter | Monotonic Sum | Cumulative and delta temporality, resets, correct rates |
| Gauge | Gauge | Point values and explicit aggregation choices |
| Distribution | Explicit-bucket Histogram | Bucket merging, count/sum, approximate quantiles |

Preserve resource attributes, instrumentation scope, metric name, unit, timestamps, temporality, and point attributes. Preserve integer values where applicable. Unsupported types must be reported rather than silently reinterpreted. Exponential histograms are a later extension.

Never sum cumulative samples as event counts, average percentiles to obtain a combined percentile, or automatically replace missing data with zero. Combine compatible histogram buckets before estimating quantiles. Do not merge incompatible boundaries without a defined conversion policy.

Keep high-cardinality investigation context, such as request IDs, in logs and spans rather than default metric dimensions.

### Traces

Support trace search, span attributes, parent/child relationships, duration, status, and a waterfall view. Preserve span events and links where supported by the initial ingestion contract. Correlate logs through trace and span IDs.

Expect spans to arrive separately and out of order. A trace view may be incomplete and should refresh while open. Make sampling visible; sampled traces must not be presented as exact totals for application traffic.

### Logs and application events

Support structured logs with severity, body, timestamps, service/resource context, attributes, and optional trace/span IDs. Provide time-bounded search, filtering, pagination, details, and a live view.

Use typed columns for common fields and structured attributes for flexible context. Avoid automatic column creation for every attribute initially.

Named application events, such as `signup.completed` and `payment.failed`, can be represented as structured logs. This supports useful counts and breakdowns without introducing a separate product analytics subsystem.

### Alerts

Start with threshold rules and generic webhook delivery. Rules use the same structured query model as dashboards.

Support evaluation windows, sustained-condition durations, normal/pending/firing/resolved states, explicit no-data behaviour, and delivery retry policies. Account for ingestion delay when selecting evaluation windows.

Persist alert state and enqueue notifications atomically in SQLite. Make delivery attempts and failures inspectable. External webhook delivery can be repeated after an ambiguous failure; do not promise exactly-once notifications.

Define restart behaviour, missed evaluations, overlap prevention, and notification suppression explicitly. Later additions include silences, maintenance windows, and test notifications.

## UI and dashboards

The UI should be minimal, modern, and immediately useful. Prioritise four views:

- **Overview:** request volume, errors, latency, and recent failures where instrumentation supports them.
- **Explore:** filter and inspect logs, spans, and metric series.
- **Trace:** waterfall, span details, and related logs.
- **Dashboards:** saved panels with a shared time range and filters.

Filter consistently by project, service, environment, and release. Encode investigation state in URLs, including absolute time ranges for reproducibility. A shareable URL does not bypass authentication.

Start custom dashboards with time-series charts, stat values, tables, and histogram views. Store versioned panel definitions containing signal/metric selection, filters, grouping, aggregation, units, visualization settings, and layout. Allow import/export through the API and CLI.

Clicking a chart interval or series should open the corresponding filtered records where possible. Do not invent trace links for aggregated metrics without exemplars or another valid association.

Use a structured query model compiled into parameterized SQL. An unrestricted SQL editor and a custom query language are not initial requirements.

## API and CLI

The API is a first-class product surface. The web frontend and CLI use the same management and query operations. CLI commands contact the server; only server mode opens the databases.

Illustrative commands, not a frozen CLI specification:

```sh
tracelet serve --data-dir ./data
tracelet projects list --json
tracelet logs search --service api --level error --last 30m --json
tracelet metrics query http.server.request.duration --last 1h --aggregate p95
tracelet traces get <trace-id> --json
tracelet dashboards export <id>
tracelet dashboards apply --file dashboard.json
tracelet alerts list --json
tracelet alerts apply --file alert.json
```

Agents must be able to discover services, metric names, units, available attributes, and bounded attribute-value samples before querying. Support stable JSON schemas, useful exit codes, stderr errors, stdin input, cursor pagination, explicit result limits, and noninteractive operation.

Query responses should include effective time ranges, units, and truncation information. Configuration `apply` operations should be idempotent using stable identifiers. Destructive actions should be explicit.

Provide separate ingestion, read-only, and administrative credentials. Keep tokens out of URLs and ordinary logs. MCP will not be implemented.

## Freshness and resource targets

Aim for near-real-time personal monitoring. A provisional target is roughly **1–4 seconds from server receipt to UI visibility under the defined normal workload**, subject to benchmarks. Direct ingestion removes the previously considered durable-queue hop, but does not itself guarantee latency.

SDK export intervals, client connectivity, queueing, commit latency, query execution, and UI refresh all contribute to observed delay. Preserve event and ingestion timestamps, particularly for delayed mobile telemetry.

Start with dashboard refresh every 2–5 seconds while visible. Consider coalesced SSE notifications after commits to invalidate relevant queries; live logs can fetch from a cursor. Avoid a full dashboard refresh for every received batch.

Target comfortable operation on a small VPS, initially evaluating a **1 GB RAM budget**. This is a design objective, not a supported minimum or measured result. Specify ingest rate, payload sizes, cardinality, retention, and simultaneous queries before publishing performance claims.

Set explicit DuckDB memory/thread/temp-space controls, bounded batches, and query concurrency/deadlines. Measure whole-process RSS, including native allocations. Go heap limits do not cap DuckDB memory.

## Operation and reliability

Provide retention per signal, storage usage visibility, configurable limits, and clear disk-pressure behaviour. Validate retention and disk reclamation under sustained ingestion before settling the storage layout. Native DuckDB tables are the initial candidate; Parquet, DuckLake, and time partitioning remain options if justified.

Expose a health page and API showing ingestion errors, rejected data, lag, queue backlog, storage pressure, and notification failures. Distinguish successful receipt, successful persistence, and availability to queries.

Support configuration backups independently of telemetry backups, documented restore procedures, and controlled schema migrations. Use database-supported backup methods or a quiesced process; do not assume copying live database files creates a consistent backup. Keep normal startup independent of runtime extension downloads where feasible.

Protect ingestion with request limits, project quotas, and credential scoping. Browser/mobile ingestion credentials are extractable and must never grant read or administrative access. Support attribute redaction/drop rules and avoid collecting secrets by default.

A single instance cannot reliably alert on the failure of its own host. An external heartbeat monitor can complement Tracelet when that coverage is required.

## Delivery sequence

1. **Foundation and vertical slice:** packaging, authentication, projects, direct OTLP ingestion, logs/traces exploration, correlation, retention, and basic API/CLI access.
2. **Metrics and dashboards:** correct metric semantics, useful defaults, saved searches, shared filters, versioned custom panels, discovery/query API and CLI.
3. **Dependable alerts:** SQLite-backed jobs, threshold evaluation, persistent state, webhook retries, health visibility, backup/restore validation.
4. **Investigation improvements:** error grouping, deployment markers, uptime/heartbeat checks, alert silences, and metric exemplars.

API and CLI capabilities should accompany each feature rather than be added after the UI. Supporting features discussed during planning are candidates in this sequence, not an obligation to ship everything in the first release.

Early validation must cover process termination around commits, retry behaviour, disk exhaustion, heavy queries during ingestion, counter resets, histogram correctness, alert recovery, missed schedules, and sustained retention. Stop expanding scope until the core path is trustworthy.

## Open decisions

- Validate River's SQLite driver and choose the required scheduling semantics/features.
- Set measured workload limits, retention defaults, and latency/memory targets.
- Finalise DuckDB layout, compaction strategy, and any indexes for trace lookup.
- Define the precise OTLP subset and signal-specific duplicate/late-data policies.
- Confirm chart and dashboard-layout libraries through a small UI prototype.
- Choose supported release platforms and combined versus CLI-only distributions.
- Choose the simplest initial UI authentication/bootstrap flow.
- Check Tracelet project-name/domain availability and select a project licence.

## References and positioning

- [O11yLite](https://github.com/o11ylite/o11ylite): closest architectural reference for embedded observability.
- [ClickStack](https://clickhouse.com/clickstack): reference for signal exploration and correlation.
- [Sentry Metrics](https://docs.sentry.io/product/metrics/): inspiration for application-oriented measurements and investigation.
- [River SQLite documentation](https://riverqueue.com/docs/sqlite): queue-driver evaluation starting point.
- [OpenTelemetry metrics data model](https://opentelemetry.io/docs/specs/otel/metrics/data-model/): semantics reference.
- [rite](https://github.com/abogoyavlensky/rite): chosen project task-management tool.

Tracelet's intended value is a low-maintenance personal observability experience with predictable resource use, useful defaults, and equal access for humans and coding agents. A smaller footprint than alternatives is a hypothesis to measure, not a claim inferred solely from implementation language.
