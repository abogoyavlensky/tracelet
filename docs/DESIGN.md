# Tracelet design

Status: initial technical design, 7 October 2026. This document records the chosen implementation direction for the vision in [VISION.md](VISION.md). Decisions marked **decided** are the plan unless phase 0 measurements contradict them. Everything else is a candidate. Phase 0 measured the storage layout on 8 October 2026; see [the storage spike results](spikes/storage.md). Phase 1 slice 1 (10 October 2026, [plan](plans/2026-10-10-1027-logs-slice.md)) built the storage layer, projects and tokens, OTLP/HTTP logs ingestion, logs search, and the CLI for them; nothing else here has been built yet.

## Architecture

| Component | Responsibility |
| --- | --- |
| Go application | HTTP server, OTLP ingestion, API, background jobs, embedded frontend |
| DuckDB | Query engine for all telemetry; native tables for the hot window |
| Parquet files | Cold telemetry, one directory tree per signal, partitioned by hour |
| SQLite | One file, `tracelet.sqlite`, with one migration sequence: projects, credentials, the Parquet file manifest, and later dashboards, alert rules and state, error groups, heartbeats, deployments, job queue |
| React frontend | Overview, explore, trace, dashboards, configuration |
| CLI | Remote management and retrieval through the same HTTP API |

**Go is the backend language (decided).** Rust was considered and has no advantage sufficient to change this.

**Two databases, one process (decided).** DuckDB does analytical work and is poor at frequent small transactional updates. SQLite does transactional application state and is poor at analytical scans. SQLite is never an intermediate telemetry store; telemetry goes directly to DuckDB. Transactions do not span the two databases, so every job touching both must tolerate retries.

**`modernc.org/sqlite` is the SQLite driver (decided, phase 1).** It is pure Go, so DuckDB stays the only native dependency, and the application-state workload is tiny. Migrations are embedded `.sql` files applied in name order and recorded in `schema_migrations`; there is no migration library.

## Data model

### Projects and credentials

A project is the tenant boundary. Every OTLP request is attributed to exactly one project by the token that authenticates it. Within a project, telemetry is organised by the OpenTelemetry resource attributes below. Tracelet uses these conventions directly rather than inventing its own, so SDK setup is a few environment variables.

| Filter | Resource attribute | Notes |
| --- | --- | --- |
| Service | `service.name` | Required. The SDK default `unknown_service` is accepted and flagged on the health page |
| Environment | `deployment.environment.name` | Optional, defaults to `default` |
| Release | `service.version` | Optional. Drives deployment markers |

Credentials have three scopes:

- **Ingest** tokens accept OTLP and heartbeat pings for one project. They can never read. Browser and mobile apps embed these, so they are assumed extractable and are rate limited per token.
- **Read** tokens query one project or all projects.
- **Admin** tokens manage projects, credentials, dashboards, alerts, and settings.

Tokens travel in the `Authorization` header only, never in URLs, and are redacted from logs.

### Telemetry tables

Each signal has one logical table with the same shape in the hot DuckDB table and the cold Parquet files. Common fields are typed columns. Everything else lives in two JSON columns, `resource` and `attributes`, which preserve value types. Automatic promotion of attributes to columns is not initial behaviour.

Logs: timestamp, observed timestamp, ingest timestamp, project, service, environment, version, severity number and text, body, trace ID, span ID, resource, attributes, scope name.

Spans: start and end timestamps, ingest timestamp, project, service, environment, version, trace ID, span ID, parent span ID, name, kind, status code and message, duration, resource, attributes, events as JSON, links as JSON, scope name.

Metric points: timestamp, start timestamp, ingest timestamp, project, service, environment, version, metric name, unit, type, temporality, monotonic flag, series hash, resource, attributes, numeric value as both integer and double, histogram count, sum, min, max, bucket counts, bucket bounds.

Every record keeps the event time and the ingest time. Delayed mobile telemetry is expected and is displayed by event time with ingest lag visible.

## Storage layout

**Hot/cold split (decided).** A native DuckDB file under continuous small commits plus rolling deletes is the riskiest assumption in the original plan. Deleted rows reclaim space poorly, the file never shrinks, and schema changes are constrained. Instead:

- **Hot:** native DuckDB tables hold the current and previous hour of each signal. All writes go here. Measured at the `busy` workload, the hot file stays flat at about 90 MB.
- **Cold:** a flush job exports completed hours to Parquet under `data/telemetry/<signal>/date=YYYY-MM-DD/hour=HH/<ulid>.parquet`, sorted by timestamp within the file. Multiple files per hour are allowed; late data for an already flushed hour produces another file.
- **Queries (decided, phase 0).** Each query unions the hot table with `read_parquet([...], union_by_name = true)` over the exact files the manifest lists for the requested time range, using each file's real time range so late-data files are found. This replaces the earlier plan of a view over a hive-partitioned glob: the manifest gives exact pruning, never breaks on an empty match, and is the single source of truth for which files exist. The hive-style directory names stay for human legibility.

**Flush by ingest cutoff (decided, phase 0).** The single writer stamps every row with an `ingest_ts` one microsecond after the previous row's (decided, phase 1), so `(ts, ingest_ts)` is a unique row key that cursor pagination relies on. At startup the writer seeds its last stamp from the greatest stamp in the hot tables and the greatest cutoff in the manifest, so stamps keep increasing across restarts and a clock that stepped back, even after a flush emptied hot. A flush of one hour captures `cutoff = max(ingest_ts)` for that hour and exports the rows at or below it, both in one DuckDB snapshot. It writes to a temporary name, fsyncs the file and its directories, renames it into place, and records the file in the SQLite manifest with `max_ingest_ts = cutoff`. Only then does it delete exactly `ts in hour AND ingest_ts <= cutoff` from the hot table. Rows that arrive for the hour after the cutoff stay hot and flush into another file later, so ingestion never pauses.

On startup, reconciliation does four things:

1. Removes temporary files.
2. Adopts final-named files missing from the manifest, reading the row count, time range, and cutoff from the file itself.
3. Drops manifest entries whose files are missing.
4. Deletes hot rows at or below each recorded hour's greatest cutoff.

Retention runs after reconciliation. Phase 0 killed the process after every flush step and lost and duplicated nothing. The manifest stores signal, hour, row count, time range, ingest cutoff, bytes, and schema version per file, and powers storage visibility.

A flush also adopts published but unrecorded files of its hour before exporting, so a retry after a failed manifest insert does not export the same rows twice.

**Query consistency: pinned snapshot plus cutoff filter (decided, phase 1).** Phase 0 showed a query overlapping a flush could see an hour twice (between the manifest insert and the hot delete) or miss it (manifest read before the insert, hot scanned after the delete). Every query now runs through a snapshot: it opens a DuckDB read transaction and pins it with a first read, then reads the manifest once. For each hour present in hot that the listed files cover, hot rows at or below that hour's greatest file cutoff are excluded, because the files hold them. The snapshot predates any delete that follows a manifest insert, so a flush can neither duplicate nor hide an hour, and flush never waits for queries. A read lock was rejected: queries would wait seconds behind each flush for no gain.

**Queries and retention exclude each other (decided, phase 1).** A DuckDB snapshot protects hot rows only, and retention unlinks files. A slot semaphore of four, the query gate, sits between them: a snapshot holds one slot, retention all four. Every acquisition gives up when its context ends, which a `sync.RWMutex` cannot do, so a cancelled request never waits behind maintenance. Four slots are also the query concurrency bound, and every query request carries a 30 s deadline that covers waiting for a slot.

**Retention** is per signal in days and deletes whole partitions, so reclaiming disk is a file removal. Each file is unlinked and its directory fsynced before its manifest row is removed, so an interrupted deletion completes on the next start and nothing is resurrected. Phase 0 held filesystem usage flat at 1.43 GB under a 3-day limit for 7 simulated days, with a retention pass costing about 15 ms. A configurable total telemetry size acts as a ring buffer: when exceeded, the oldest partitions go first and the health page records it. A free-disk floor rejects ingestion with a retryable error rather than filling the disk.

**Backup** is `tracelet backup`: with retention held off through the query gate, force a flush, snapshot the manifest with `VACUUM INTO`, and copy exactly the files that snapshot lists. Parquet files are immutable so a plain copy is consistent, and restoring is opening a data directory on the copy. Phase 0 backed up 7 days of `busy` data (2.98 GB) in under 10 s. Copying a live SQLite or DuckDB file is documented as unsupported.

**Schema evolution.** New columns are nullable. The hot table changes with `ALTER TABLE ADD COLUMN`; old Parquet files are read with union by name. The manifest records the schema version so migrations can rewrite old partitions if ever required.

**Trace lookup by ID.** Trace IDs are random so min/max statistics do not prune. The initial approach is a time-bounded scan reading only the trace ID column, defaulting to the last seven days and extendable by the caller. Trace lists and log links always carry timestamps, so most lookups are already bounded. Phase 0 measured an unbounded 7-day lookup at the `busy` workload at 0.94 s p50 and 1.03 s p95, and 1.8 s p95 under concurrent query load. That misses the 1 s target, so the fallback is needed: a small per-partition trace index file listing trace ID and time range, written at flush (see open decisions).

**Rollups** for long retention are a later addition computed from cold partitions into separate rollup files. They are not needed until retention exceeds what raw scans handle inside the memory budget.

## Ingestion and durability

Endpoints follow OTLP/HTTP at `/v1/logs` (and later `/v1/traces`, `/v1/metrics`), outside `/api/`, because every SDK appends that path to the configured endpoint (decided, phase 1). Users set `OTEL_EXPORTER_OTLP_ENDPOINT=http://host:8080` and nothing else. Gzip is accepted, and the 4 MB body limit applies before and after decompression.

**Protobuf first (decided, phase 1).** OTLP/JSON encodes trace and span IDs as hex, which `protojson` does not, so correct support needs its own decoding. The Go, Java, Python, and .NET SDKs default to protobuf. JSON requests get 415 until the browser and mobile slice ([backlog](backlog/otlp-json-encoding.md)). gRPC is deferred until a real SDK needs it ([backlog](backlog/otlp-grpc.md)). Requests decode into `LogsData`, which is wire-identical to `ExportLogsServiceRequest`, so the gRPC-importing collector package stays out of the build.

1. Authenticate the token, resolve the project, enforce request size and per-project limits.
2. Decode and validate. Reject unsupported signal types explicitly. Report partial acceptance using the OTLP partial success response.
3. Place accepted records in a bounded in-memory queue.
4. A writer goroutine groups queued records into batches by time or size and writes one DuckDB transaction per batch. **Bounds (decided, phase 1):** a commit closes at 2,000 rows or 250 ms after its first submission, whichever comes first. Phase 0 measured a 5 ms WAL sync per commit and multi-second stalls at 12,000-row commits; at the normal workload this is about four small commits a second. A request is never split across commits: splitting would let a later chunk fail after an earlier one committed, and the client's retry would duplicate the committed part. A request over 2,000 rows commits alone, bounded by the body limit.
5. Respond success only after the batch containing the request's records commits.

Data in the queue is unacknowledged, so a crash loses nothing the exporter believes delivered. A lost response after a commit can still produce duplicates. This is at-least-once, not exactly-once.

Duplicate policy per signal: spans are deduplicated on trace ID plus span ID within the hot window. Logs are not deduplicated. Metric points are deduplicated on series hash, start timestamp, and timestamp, which covers retried delta and cumulative points alike.

When the queue is full, respond with a retryable failure and `Retry-After`. Never acknowledge discarded data. A SQLite inbox for durable burst absorption stays rejected unless phase 0 shows a need.

## Background jobs

**In-process scheduler over a SQLite jobs table (decided).** River was the earlier candidate. Its SQLite driver is experimental by its own documentation and the library targets multi-node Postgres deployments. The job needs here are small and a dependency with that risk profile fails the infrastructure principle.

The `jobs` table holds: id, kind, payload JSON, run at, attempts, max attempts, state (scheduled, running, succeeded, failed, dead), lease expiry, last error, and a unique key for idempotent scheduling. A fixed pool of worker goroutines claims jobs with a short `UPDATE ... WHERE state = 'scheduled' AND run_at <= now` transaction. Leases expire so a crash mid-job is recovered on restart. Periodic schedules are defined in code and enqueue the next run when the current one completes.

Rules: keep SQLite transactions short, never hold one open across a DuckDB query or a network call, and make every job idempotent.

Job kinds in the first releases: hot-to-cold flush, retention, heartbeat evaluation, alert evaluation, notification delivery, deployment detection, error group maintenance, backup.

**A maintenance ticker until phase 3 (decided, phase 1).** The jobs table arrives with alerts. Until then a goroutine owned by the app runs once a minute: flush due hours with no lock, then retention under the query gate's exclusive side (skipped until the next tick if queries hold it past 10 s), then a free-disk sample that pauses ingestion below the floor. Startup runs reconcile, then retention, before serving. Failures are logged and retried on the next tick.

## Core signals

### Metrics

Initial OTLP subset:

| User concept | Representation | Required semantics |
| --- | --- | --- |
| Counter | Monotonic Sum | Cumulative and delta temporality, reset detection, correct rates |
| Gauge | Gauge | Point values, explicit aggregation choice |
| Distribution | Explicit-bucket Histogram | Bucket merging, count and sum, approximate quantiles |

Preserve resource attributes, scope, name, unit, timestamps, temporality, and point attributes. Preserve integer values. Report unsupported types rather than reinterpreting them. Exponential histograms are a later extension.

Never sum cumulative samples as event counts, never average percentiles, never fill gaps with zero. Merge compatible histogram buckets before estimating quantiles and refuse to merge incompatible boundaries without a defined policy. Rates for cumulative counters are computed per series in SQL with reset detection.

High-cardinality investigation context such as request IDs belongs in logs and spans, not metric dimensions.

### Traces

Trace search, span attributes, parent and child relationships, duration, status, waterfall view, span events and links. Logs correlate through trace and span IDs. Spans arrive separately and out of order; a trace view may be incomplete and refreshes while open. Sampling is visible and sampled traces are never presented as exact traffic totals.

### Logs and application events

Structured logs with severity, body, timestamps, resource context, attributes, and optional trace context. Time-bounded search, filtering, cursor pagination, record details, and a live view fed from a cursor.

Named application events such as `signup.completed` are structured logs with an event name attribute. This gives counts and breakdowns without a separate analytics subsystem.

### Errors

Error grouping is phase 2 because it is the most common first question for personal projects. Sources are logs carrying the `exception.*` semantic convention attributes and spans with error status plus exception events. The fingerprint is a hash of service, exception type, and normalised top stack frames, falling back to a normalised message. Group metadata lives in SQLite: fingerprint, title, first and last seen, status (open, resolved, ignored). Counts and samples are queried from telemetry, not duplicated.

### Heartbeats

Heartbeats are phase 1 because "did a background job stop running" is a stated purpose and the feature is small. A check has a name, project, expected period, and grace. Jobs ping `POST /api/v1/heartbeats/{id}/ping` with an ingest token. The evaluation job marks checks late or down and raises notifications through the alert delivery path. For instrumented services, an alert rule of "no logs from service X for N minutes" covers the same need without a ping.

### Deployments

A deployment marker is created when a new `service.version` is first seen for a service and environment, or explicitly through `POST /api/v1/deployments`. Markers are stored in SQLite and overlaid on charts and the overview.

### Alerts

Threshold rules on the shared structured query model with generic webhook delivery first. Rules have an evaluation window, a sustained-condition duration, explicit no-data behaviour, and states normal, pending, firing, resolved. Evaluation windows account for ingestion delay by evaluating a window that ends slightly in the past.

Alert state and the notification job are written atomically in SQLite. Delivery attempts and failures are inspectable. Webhook delivery is at-least-once. Missed evaluations after downtime run once against the current window; they do not replay history, and the health page records the gap. A unique job key prevents overlapping evaluations of one rule. Silences, maintenance windows, and test notifications follow in phase 4.

## UI and dashboards

Four views, in priority order:

- **Overview:** request volume, errors, latency from `http.server.request.duration` and related conventions, recent error groups, recent deployments, heartbeat status.
- **Explore:** filter and inspect logs, spans, and metric series.
- **Trace:** waterfall, span details, related logs.
- **Dashboards:** saved panels with shared time range and filters.

Filters are project, service, environment, and release everywhere. Investigation state is encoded in URLs with absolute time ranges. A shareable URL never bypasses authentication.

Dashboards start with time series, stat values, tables, and histograms. Panel definitions are versioned JSON holding signal and metric selection, filters, grouping, aggregation, units, visualisation settings, and layout. Import and export through the API and CLI. Layout is a simple CSS grid first; draggable layout is evaluated later.

Clicking a chart interval or series opens the corresponding filtered records. Aggregated metrics link to traces only through exemplars or another valid association.

**Query model.** A structured query model is compiled to parameterised SQL. There is no SQL editor and no custom query language.

## API and CLI

The API is a product surface. Frontend and CLI use the same operations, described by OpenAPI. CLI commands contact a server; only `serve` opens databases.

Illustrative commands, not a frozen specification:

```sh
tracelet serve --data-dir ./data
tracelet backup --data-dir ./data --out backup.tar
tracelet projects list --json
tracelet logs search --service api --level error --last 30m --json
tracelet metrics query http.server.request.duration --last 1h --aggregate p95
tracelet traces get <trace-id> --json
tracelet errors list --service api --last 24h --json
tracelet heartbeats create --name nightly-report --period 24h --grace 1h
tracelet dashboards export <id>
tracelet dashboards apply --file dashboard.json
tracelet alerts apply --file alert.json
```

Agents can discover services, metric names, units, attributes, and bounded attribute value samples before querying. Stable JSON schemas, meaningful exit codes, errors on stderr, stdin input, cursor pagination, explicit limits, and fully noninteractive operation. Query responses include effective time range, units, and truncation flags. `apply` is idempotent on stable identifiers. Destructive actions require an explicit flag.

## Freshness and resource targets

Provisional target: 1 to 4 seconds from server receipt to UI visibility under the defined normal workload. Export intervals, queueing, commit latency, query time, and refresh cadence all contribute.

Dashboards refresh every 2 to 5 seconds while visible. A coalesced server-sent event after each commit invalidates affected queries rather than refreshing everything. Live logs read from a cursor.

Resource budget: comfortable operation in 1.5 GB RAM on a small VPS at the normal workload below. This is a design objective, not a supported minimum. DuckDB gets an explicit memory limit, defaulting to 256 MB, two threads, and a temp directory inside the data directory, set once when the database is created. Query concurrency and deadlines are bounded. Whole-process RSS is what gets measured; Go heap limits do not cap DuckDB.

**Why 1.5 GB, not 1 GB.** Phase 0 peaked at 997 MB and 1,001 MB over 7 simulated days. The [memory follow-up](spikes/memory.md) attributed it: Go holds about 20 MB, DuckDB's own accounting stays within its 256 MB limit, and about 600 MB is memory DuckDB's bundled jemalloc keeps after DuckDB frees it. `allocator_background_threads` cut about 200 MB but still peaked at 788 MB over 7 days, and its real-time run doubled commit p99. A 128 MB `memory_limit` runs out of memory in the hourly flush's sort. No setting reached the 700 MB target without a regression, so the default settings stay and the budget is 1.5 GB.

**Normal workload (phase 0).** The `busy` profile is 100 logs/s, 50 spans/s, and 50 metric points/s from 10 services in 2 environments over 500 metric series, with 1% of logs up to 3 hours late. That is 17.3 million rows and about 423 MB of Parquet per day. Measured at this workload:

- commit latency: 15 ms p50 and 46 ms p99 for one-second batches; every commit costs a WAL sync of about 5 ms, and batches of 12,000 rows occasionally stall for seconds;
- investigation queries over 1 hour to 7 days: 2.1 s p99 or less under ingestion.

## Operation and reliability

A health page and API show ingestion errors, rejected data, lag, queue depth, hot table size, cold partition count and bytes, disk pressure, missed job runs, and notification failures. Receipt, persistence, and query availability are reported separately.

Configuration migrations are versioned and run at startup. Startup never depends on downloading DuckDB extensions; required extensions are bundled.

Ingestion is protected by request limits, per-project quotas, and credential scoping. Attribute redaction and drop rules are supported. Secrets are not collected by default.

A single instance cannot alert on its own host failing. An external heartbeat monitor complements Tracelet where that matters.

**UI bootstrap (decided).** On first start with no admin, the server prints a one-time setup URL to stdout. Visiting it creates the admin user. The UI uses a session cookie; the API uses bearer tokens. The setup URL arrives with the UI slice. Until then the server bootstraps for the CLI (decided, phase 1): with no active admin token it creates one and prints it once to stderr, or stores the hash of `TRACELET_ADMIN_TOKEN` for containers. Bootstrap runs last at startup, so a start that fails cannot store a token it never printed.

**Tokens (decided, phase 1).** `tl_` plus 40 hex characters, sent only as `Authorization: Bearer`. SQLite stores the SHA-256 and a 6-character prefix for listing. Scopes are `ingest` (one project, write only), `read` (one project, or all), and `admin` (everything). Telemetry rows store the project's immutable random ID; the API and CLI speak slugs, so renaming a slug never touches telemetry.

## Stack and workflow

| Area | Direction |
| --- | --- |
| Backend | Go, standard library HTTP, explicit SQL |
| Telemetry transport | OTLP/HTTP protobuf and JSON, gzip; gRPC later if needed |
| Frontend | React, TypeScript, Vite, TanStack Query |
| Charts | uPlot for time series (decided); other panel types evaluated in the UI prototype |
| Dashboard layout | CSS grid first; react-grid-layout evaluated later |
| API description | OpenAPI |
| Tool versions | mise |
| Task management | abogoyavlensky/rite |

Built frontend assets are embedded in the server executable; production needs no Node.js. DuckDB brings CGO and prebuilt native libraries. Phase 0 built and tested them with the bundled static libraries on both Linux architectures: unstripped binaries of 80 MB (amd64) and 73 MB (arm64). The executable is not universally static. CI and release builds pin `ubuntu-24.04` and `ubuntu-24.04-arm` rather than `ubuntu-latest`, because the runner's glibc sets the oldest glibc the binary runs on.

**Platforms (decided).** The combined server and CLI executable ships for Linux amd64 and arm64, plus a Docker image. A CLI-only executable without DuckDB ships for macOS, Linux, and Windows. Additional server platforms are added on demand.

## Delivery sequence and validation

Phase 0 is done; its results are in [spikes/storage.md](spikes/storage.md). It was a throwaway spike, not product code: a load generator producing realistic logs, spans, and metrics at configurable rates and cardinality, driving a minimal ingestion path into the hot/cold layout for several simulated days. It had to demonstrate, inside what was then a 1 GB budget:

- flush and reconciliation under process termination at every step;
- retention and ring-buffer deletion reclaiming disk under sustained ingestion;
- heavy queries during ingestion without starving either;
- trace lookup latency over the full retention window;
- counter reset handling and histogram quantile correctness against known inputs;
- backup and restore producing identical query results;
- release packaging on both Linux architectures.

Phases 1 to 4 follow the vision. Each feature ships with API and CLI access. Scope stops expanding until the core path is trustworthy.

## Open decisions

- Memory below 1.5 GB: `allocator_background_threads` saves about 200 MB but regressed real-time commit p99 in a run that also changed `GOMEMLIMIT` and added sampling. Adopt it only after a run that isolates its effect on latency ([backlog](backlog/allocator-background-threads-isolation-run.md)). A `memory_limit` below 256 MB needs a flush that sorts an hour in less memory.
- Trace index: phase 0 showed it is needed (7-day lookup p95 1.03 s alone, 1.8 s under load). Open questions are its format and whether it is written at flush or compaction ([backlog](backlog/spans-ingestion-and-trace-index.md)).
- Late-data compaction: 1% late logs triples the log file count; decide when an hour is closed and its small late files are merged ([backlog](backlog/late-data-compaction.md)).
- Duplicate handling on ingest, out of scope for phase 0, needs its own design against the measured Appender throughput.
- Non time-series chart components after the UI prototype.
- Exact attribute redaction rule format.

## Decision log

| Decision | Choice | Reason |
| --- | --- | --- |
| Backend language | Go | Ecosystem, DuckDB bindings, single-binary tooling; Rust offered no decisive advantage |
| Telemetry store | DuckDB hot tables plus Parquet cold partitions | Retention, backup, and schema evolution become file operations; avoids DuckDB delete and file-growth behaviour |
| Application state | SQLite | Transactional, well understood, single file |
| Job queue | In-process scheduler over SQLite table | Small needs; River's SQLite driver is experimental and the library targets multi-node Postgres |
| Telemetry inbox in SQLite | Rejected | Direct DuckDB writes with ack after commit are sufficient; revisit only on measured need |
| Cold file resolution | Manifest-resolved file lists, not a glob view | Exact pruning, no empty-glob failures, one source of truth; measured in phase 0 |
| Flush rule | Per-hour ingest cutoff | Exports and deletes exactly the same rows without pausing ingestion; survived a process kill at every step in phase 0 |
| Memory budget | 1.5 GB at the `busy` workload, default DuckDB settings | Default settings peaked at 1,001 MB over 7 days; allocator background threads, the only lever that helped, missed 700 MB and regressed commit latency; measured in the memory follow-up |
| Query consistency | Pinned DuckDB snapshot plus per-hour cutoff filter; retention excluded by a 4-slot query gate | Closes both overlap windows phase 0 found without making queries wait for flush |
| Ingest stamps | One per row, a microsecond apart, seeded from hot and the manifest at startup | `(ts, ingest_ts)` becomes a unique key for cursors; survives restarts and clock steps |
| Writer batches | 2,000 rows or 250 ms; a request is never split | Small commits at the normal workload; no partial commit for a retry to duplicate |
| SQLite driver | `modernc.org/sqlite`, one `tracelet.sqlite` | Pure Go keeps DuckDB the only native dependency |
| OTLP | `/v1/logs` over HTTP, protobuf first | The path SDKs append; JSON needs hex-ID decoding, gRPC a large dependency |
| Jobs before phase 3 | A once-a-minute maintenance ticker | The jobs table arrives with alerts |
| Hot window | Current and previous hour, hourly flush | Hot file flat at about 90 MB; flush at most about 2 s per signal and hour at `busy` |
| MCP | Not planned | OpenAPI plus a JSON CLI is sufficient for agents and more stable |
| Charts | uPlot | Small, fast, proven for time series in Grafana |
| Error grouping | Phase 2 | Most common first question; cheap once logs and spans exist |
| Heartbeats | Phase 1 | Stated purpose; small feature |

## References

- [O11yLite](https://github.com/o11ylite/o11ylite): closest architectural reference for embedded observability.
- [OpenObserve](https://openobserve.ai): single-binary competitor using SQLite metadata and Parquet storage.
- [VictoriaLogs](https://docs.victoriametrics.com/victorialogs/), [VictoriaMetrics](https://victoriametrics.com), [VictoriaTraces](https://docs.victoriametrics.com/victoriatraces/): lightweight Go telemetry databases to benchmark against.
- [ClickStack](https://clickhouse.com/clickstack): reference for signal exploration and correlation.
- [Sentry](https://docs.sentry.io): reference for error grouping and application-oriented investigation.
- [OpenTelemetry metrics data model](https://opentelemetry.io/docs/specs/otel/metrics/data-model/) and [semantic conventions](https://opentelemetry.io/docs/specs/semconv/): semantics reference.
- [DuckDB Parquet and hive partitioning](https://duckdb.org/docs/data/partitioning/hive_partitioning): storage layout reference.
- [uPlot](https://github.com/leeoniya/uPlot): chart library.
- [rite](https://github.com/abogoyavlensky/rite): project task-management tool.
