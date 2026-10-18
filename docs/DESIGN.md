# Tracelet design

Status: initial technical design, 7 October 2026. This document records the chosen implementation direction for the vision in [VISION.md](VISION.md). Decisions marked **decided** are the plan unless phase 0 measurements contradict them. Everything else is a candidate. Phase 0 measured the storage layout on 8 October 2026; see [the storage spike results](spikes/storage.md). Phase 1 slice 1 (10 October 2026, [plan](plans/2026-10-10-1027-logs-slice.md)) built the storage layer, projects and tokens, OTLP/HTTP logs ingestion, logs search, and the CLI for them. Phase 1 slice 2 (11 October 2026, [plan](plans/2026-10-11-0858-traces-slice.md)) added OTLP/HTTP traces ingestion with span deduplication, trace lookup by ID, span search, and their CLI commands. Packaging shipped on 11 October 2026 ([plan](plans/2026-10-11-2111-release-and-deploy.md)): a Docker image and Linux release binaries on version tags, the CLI-only build, and a deploy of every master push; see [operations](operations.md). Phase 1 slice 3 (12 October 2026, [plan](plans/2026-10-12-1216-explore-ui.md)) added the web UI: token sign-in with a session cookie, the logs explorer, the traces list, and the trace page with its waterfall, span details, and related logs. Phase 2 slice 1 (12 October 2026, [plan](plans/2026-10-12-2159-metrics-slice.md)) added OTLP/HTTP metrics ingestion, metric discovery and queries, `tracelet metrics`, and the Metrics view. Phase 2 slice 2 (18 October 2026, [plan](plans/2026-10-18-0827-dashboards.md)) added saved dashboards of metric panels, written in the UI or applied with `tracelet dashboards`. Nothing else here has been built yet.

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
| Environment | `deployment.environment.name` | Optional, defaults to `default`; falls back to the pre-1.27 `deployment.environment` |
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

**Trace lookup by ID: a time-bounded scan, no index (decided, phase 1).** Trace IDs are random, so min/max statistics on time-sorted files do not prune. A lookup scans the span files of its range through the usual snapshot, defaulting to the last seven days and extendable by the caller; trace lists and log links carry timestamps, so most lookups are bounded more tightly. Phase 0 measured a 7-day lookup at 0.94 s p50 and 1.03 s p95, and 1.8 s p95 under concurrent query load, and proposed an index. Phase 1 measured the product's query path over 168 hours of `busy`-shaped span files at **118 ms p50 and 151 ms p95** ([trace lookup results](spikes/trace-lookup.md)). It also measured the planned index, span files sorted by trace ID in row groups of 10,000 so statistics prune all but one row group per file. That layout was slower at 489 ms p95, because every lookup reads all 168 footers and small row groups make them large, and it flushed slower and wrote more bytes. Span files therefore stay sorted by time, like every signal. Revisit when lookups are measured under concurrent load, or when span rates or retention grow well past `busy`.

**Rollups** for long retention are a later addition computed from cold partitions into separate rollup files. They are not needed until retention exceeds what raw scans handle inside the memory budget.

## Ingestion and durability

Endpoints follow OTLP/HTTP at `/v1/logs`, `/v1/traces`, and `/v1/metrics`, outside `/api/`, because every SDK appends that path to the configured endpoint (decided, phase 1). Users set `OTEL_EXPORTER_OTLP_ENDPOINT=http://host:8080` and nothing else. Gzip is accepted, and the 4 MB body limit applies before and after decompression.

**Protobuf first (decided, phase 1).** OTLP/JSON encodes trace and span IDs as hex, which `protojson` does not, so correct support needs its own decoding. The Go, Java, Python, and .NET SDKs default to protobuf. JSON requests get 415 until the browser and mobile slice ([backlog](backlog/otlp-json-encoding.md)). gRPC is deferred until a real SDK needs it ([backlog](backlog/otlp-grpc.md)). Requests decode into `LogsData` and `TracesData`, which are wire-identical to `ExportLogsServiceRequest` and `ExportTraceServiceRequest`, so the gRPC-importing collector package stays out of the build. Both routes share one handler and one partial success encoding, since the two responses have the same field numbers.

1. Authenticate the token, resolve the project, enforce request size and per-project limits.
2. Decode and validate. Reject unsupported signal types explicitly. Report partial acceptance using the OTLP partial success response.
3. Place accepted records in a bounded in-memory queue.
4. A writer goroutine groups queued records into batches by time or size and writes one DuckDB transaction per batch. **Bounds (decided, phase 1):** a commit closes at 2,000 rows or 250 ms after its first submission, whichever comes first. Phase 0 measured a 5 ms WAL sync per commit and multi-second stalls at 12,000-row commits; at the normal workload this is about four small commits a second. A request is never split across commits: splitting would let a later chunk fail after an earlier one committed, and the client's retry would duplicate the committed part. A request over 2,000 rows commits alone, bounded by the body limit.
5. Respond success only after the batch containing the request's records commits.

Data in the queue is unacknowledged, so a crash loses nothing the exporter believes delivered. A lost response after a commit can still produce duplicates. This is at-least-once, not exactly-once.

Duplicate policy per signal: spans are deduplicated on project, trace ID, and span ID within the hot window. Logs are not deduplicated.

**Span deduplication in the writer (decided, phase 1).** Inside the write transaction, before appending, the writer selects the keys of hot spans that share a trace ID with the batch and fall within its time span, and skips any span whose key is there or earlier in the batch. The project is part of the key because one commit can hold several projects' submissions. Skipped spans are counted as `duplicate_spans` on health, not absorbed into row counts. A retry that arrives after its hour was flushed is not caught; the README says so. Metric points are dropped the same way when their series hash, start timestamp, and timestamp are already in the hot window or earlier in the batch, and counted as `duplicate_points` (decided, phase 2). Timestamps are truncated to microseconds at conversion, the hot table's resolution, so a retried point's key matches the stored one. A retry after its hour was flushed is harmless for cumulative series, whose duplicate delta is zero, and double-counts a delta series; that is accepted as rare.

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

**Built (phase 2 slice 1).** Exponential histograms and summaries are rejected with a reason ([backlog](backlog/exponential-histograms-and-summaries.md)), as are delta up-down counters, whose level cannot be rebuilt from deltas. Exemplars are dropped until phase 4.

- **Series identity (decided).** The series hash is FNV-64a over the project, the metric's name, type, temporality, monotonic flag, and unit, the scope's name, version, and attributes, and the resource and point attributes, each field length-prefixed. Resource attributes are fixed for an SDK's lifetime, so replicas and restarted processes are separate series without Tracelet knowing which keys identify an instance. Hashing only name, service, and point attributes, as the storage spike did, merges replicas and turns their interleaved counters into false resets.
- **Deltas.** Per series ordered by time, a cumulative point is a reset when its value or count drops or its start time changes, and a reset's delta is its own value. A cumulative point without a predecessor counts its whole value when its series started inside the rows the query read, which begin 10 minutes before the range, so the first export after a deploy is not lost. Integer values are subtracted as integers. A bucket-bounds change without a reset nulls that point's bucket delta but keeps its count and sum deltas.
- **Aggregations.** A counter (monotonic sum) takes `rate` or `increase`; a gauge or up-down counter takes `avg`, `min`, `max`, or `sum`, reducing each series within a step first (`sum` takes each series' last point); a histogram takes `pNN`, `rate` (observations per second), or `avg` (sum over count). Within a group, histogram buckets merge across the series that share the most common bounds, and the rest are reported as `incompatible_series`.
- **Steps.** A step is `epoch_us(ts) // step_us`, so steps line up across queries; the range start aligns down to the step. Without one, the step is the first of 10s to 24h that is at least `max(range / 200, 1m)`. A step's value comes from the points whose time falls in it, so the last step reads low until it completes. Results are aligned arrays of timestamps and per-group values with null for empty steps, at most 20 groups, largest first.

### Traces

Trace search, span attributes, parent and child relationships, duration, status, waterfall view, span events and links. Span search lists spans; restricted to root spans (`root=true`) it approximates a trace list, missing only traces whose root span was never exported. A trace response carries its start and end so a client can bound the follow-up logs search. Logs correlate through trace and span IDs. Spans arrive separately and out of order; a trace view may be incomplete and refreshes while open. Sampling is visible and sampled traces are never presented as exact traffic totals.

### Logs and application events

Structured logs with severity, body, timestamps, resource context, attributes, and optional trace context. Time-bounded search, filtering, cursor pagination, record details, and a live view fed from a cursor.

**Live tail (built).** Every logs page carries a `tail_cursor`, and `since=<tail_cursor>` returns only the matching logs the earlier response could not have shown. The cursor is an ingest-stamp watermark, not a timestamp: the single writer commits batches in stamp order, so the hot table's maximum stamp in a pinned snapshot, joined with the manifest's greatest cutoff read before the pin, bounds exactly what that snapshot saw. A `ts` cursor would skip a record that arrives late behind a newer one, since exporters batch and service clocks differ. The cursor also holds the previous range's end, so a stored record with a `ts` in the future is returned once the range reaches it. The only race left repeats a row, never skips one, so clients de-duplicate on `(ts, ingest_ts)`. A response with more new records than `limit` says `truncated`. The UI's Live toggle and `tracelet logs search --follow` poll every 2 seconds; the server-sent event described under freshness is still to come, and the same cursor serves it.

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

Filters are project, service, environment, and release everywhere. Investigation state is encoded in URLs. An explorer URL may carry a relative range (`last=1h`), so a bookmark means "the last hour"; every link between views (a log to its trace, a trace to its logs) carries an absolute range, so following one is reproducible. A shareable URL never bypasses authentication.

**Explore UI (built, phase 1 slice 3).** A top header (project, Logs and Traces, time range, refresh, health, theme, account) above full-width lists under a full-width filter bar. Selecting a record or span opens a detail pane beside the list; the list only narrows. The trace page draws the waterfall against a time grid and opens on the first failed span. Phones get a bottom tab bar and a list-to-detail flow. Light is the default theme and dark is a stored choice; colours are semantic tokens, so components never use raw palette values. The header becomes an icon rail when phase 2 adds more views.

**Metrics view (built, phase 2 slice 1).** A third header item, Metrics, opens a catalog of the project's metrics with their kind, unit, services, and series count. A metric opens a chart page whose aggregation, grouping, filters, and step live in the URL. The chart is uPlot drawn in the semantic tokens and rebuilt on a theme change; lines never cross an empty step, and a value with gaps on both sides is drawn as a point. A legend below shows each group's value at the cursor and toggles its series. The header kept its tabs: three views still fit.

**Installable app (built).** A web app manifest and home screen icons make the UI installable on phones and desktops, and `theme-color` follows the light or dark theme so the browser chrome matches the header. There is no service worker: an observability dashboard has nothing useful to show offline, and a cached app shell could outlive a deploy and run a stale frontend against a newer server. The header's refresh button re-runs the page's searches, and reloads the whole page when the health endpoint reports a different server version than the page loaded with, because an installed app has no reload of its own.

Dashboards start with time series, stat values, tables, and histograms. Panel definitions are versioned JSON holding signal and metric selection, filters, grouping, aggregation, units, visualisation settings, and layout. Import and export through the API and CLI. Layout is a simple CSS grid first; draggable layout is evaluated later.

**Dashboards (built, phase 2 slice 2).** A dashboard is one JSON document per project and `uid`, a stable identifier its author picks, stored as a row in `tracelet.sqlite`. It holds a title, a default range (`last`), a default auto-refresh (`off`, `10s`, `30s`, `1m`, `5m`, or `15m`; 30 s unless set), and up to 30 panels. A panel has a type (`timeseries`, `stat`, or `table`), a width in quarters of a four-column grid, and a `metric` object with the metric query's own parameters; a later signal is a sibling of `metric`, not a new meaning for it. A stat is one number and cannot be grouped. Stat and table reduce steps to one value: `last` is the latest step that has ended, since the step holding the range's end reads low, and `sum` adds every step and is allowed only for `increase`, because no other aggregation adds up to a total. Whether an aggregation fits a metric depends on the data, so a panel checks it when it runs. "Versioned" means a `schema_version`, only 1 so far, and a server-managed `revision`; no edit history is kept. A write that changes nothing keeps the revision, so applying a file twice is a no-op. A write may carry the revision it read: `0` creates only, `N` writes only if the dashboard is still at N, and either is a 409 otherwise, so a browser edit and a CI apply never silently overwrite each other; leaving it out overwrites, which is what `apply` of an exported file does. Reads take a `read` token; writes take `admin`. The dashboard's range is its own unless the URL names one; its service and environment filters live in the URL and fill only the panel filters a panel leaves empty. Each panel runs the metric query endpoint itself; the query gate queues, so a dashboard needs no batch endpoint at personal scale. The metric page is the panel editor: an admin adds the chart in view to a dashboard there, and a panel's Edit query opens it with a Save to panel bar that refuses when that panel's query changed elsewhere since editing began. The header shows a fourth item, Dashboards, as icons only between md and lg.

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
tracelet dashboards list --project shop
tracelet dashboards export shop-overview --project shop > shop-overview.json
tracelet dashboards apply --project shop --file shop-overview.json
tracelet dashboards delete shop-overview --project shop --yes
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

**UI sign-in (decided, phase 1 slice 3).** The UI signs in with an existing `admin` or `read` token; there is no setup URL and no user account. This amends the earlier decided bootstrap, in which a setup URL created an admin user, at the owner's request: tokens already exist, and a single-owner tool gains nothing from a second kind of credential. User accounts can come later if they are ever needed. `POST /api/v1/session` checks the token and sets the `tracelet_session` cookie to it: `HttpOnly`, `SameSite=Strict`, `Path=/api/`, 30 days, and `Secure` over HTTPS (directly or through `X-Forwarded-Proto`). Revoking the token ends the session, so there is no session state on the server. The cookie authenticates only routes under `/api/`; OTLP ignores it, and non-safe cross-origin requests carrying it are refused (`http.CrossOriginProtection`). Ingest tokens cannot sign in. The recommended practice is one token per browser (`tracelet tokens create --scope read --name laptop`), so one device can be revoked without touching the others.

**Server bootstrap (decided, phase 1).** With no active admin token the server creates one and prints it once to stderr, or stores the hash of `TRACELET_ADMIN_TOKEN` for containers. Bootstrap runs last at startup, so a start that fails cannot store a token it never printed.

**Tokens (decided, phase 1).** `tl_` plus 40 hex characters, sent only as `Authorization: Bearer`. SQLite stores the SHA-256 and a 6-character prefix for listing. Scopes are `ingest` (one project, write only), `read` (one project, or all), and `admin` (everything). Telemetry rows store the project's immutable random ID; the API and CLI speak slugs, so renaming a slug never touches telemetry.

## Stack and workflow

| Area | Direction |
| --- | --- |
| Backend | Go, standard library HTTP, explicit SQL |
| Telemetry transport | OTLP/HTTP protobuf and JSON, gzip; gRPC later if needed |
| Frontend | React, TypeScript, Vite, TanStack Query, TanStack Router, Tailwind |
| Charts | uPlot for time series (decided); other panel types evaluated in the UI prototype |
| Dashboard layout | CSS grid first; react-grid-layout evaluated later |
| API description | OpenAPI |
| Tool versions | mise |
| Task management | abogoyavlensky/rite |

Built frontend assets are embedded in the server executable; production needs no Node.js. DuckDB brings CGO and prebuilt native libraries. Phase 0 built and tested them with the bundled static libraries on both Linux architectures: unstripped binaries of 80 MB (amd64) and 73 MB (arm64). The executable is not universally static. CI and release builds pin `ubuntu-24.04` and `ubuntu-24.04-arm` rather than `ubuntu-latest`, because the runner's glibc sets the oldest glibc the binary runs on.

**Platforms (decided).** The combined server and CLI executable ships for Linux amd64 and arm64, plus a multi-arch Docker image. A CLI-only executable without DuckDB (`-tags cli`, no cgo) ships for Linux and macOS on amd64 and arm64. Windows was promised here and is not built; it is added on demand, as are additional server platforms. This amends the decided item at the owner's request when packaging shipped.

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
- Late-data compaction: 1% late logs triples the log file count; decide when an hour is closed and its small late files are merged ([backlog](backlog/late-data-compaction.md)).
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
| OTLP | `/v1/logs` and `/v1/traces` over HTTP, protobuf first | The paths SDKs append; JSON needs hex-ID decoding, gRPC a large dependency |
| Trace lookup | Time-bounded scan of time-sorted span files, 7-day default; no trace index | 151 ms p95 over 7 days at `busy`; a trace-sorted layout with small row groups measured slower (489 ms p95) |
| Span duplicates | Dropped in the writer against the hot window, keyed on project, trace ID, and span ID; counted on health | Catches an SDK retry after a lost response without a second store; a retry after the hour's flush is accepted as rare |
| Jobs before phase 3 | A once-a-minute maintenance ticker | The jobs table arrives with alerts |
| Hot window | Current and previous hour, hourly flush | Hot file flat at about 90 MB; flush at most about 2 s per signal and hour at `busy` |
| MCP | Not planned | OpenAPI plus a JSON CLI is sufficient for agents and more stable |
| Charts | uPlot | Small, fast, proven for time series in Grafana |
| Error grouping | Phase 2 | Most common first question; cheap once logs and spans exist |
| Heartbeats | Phase 1 | Stated purpose; small feature |
| Series identity | Hash of project, metric, scope, resource, and point attributes | Replicas and restarts are separate series, so cumulative counters never interleave into false resets |
| Metric duplicates | Dropped in the writer against the hot window, keyed on series hash, start time, and time; counted on health | One mechanism with spans; an export's points share a timestamp, so the lookup reads few rows |
| Metric query | One endpoint; aggregation by kind; aligned steps; null for empty steps; at most 20 groups | Correct counter, gauge, and histogram semantics behind one shape that charts and agents read alike |
| Dashboard storage | One normalized JSON document per project and uid in SQLite, with a compare-and-swap revision | Read and written whole; idempotent apply on a stable uid; concurrent writers get a 409, never a lost write |
| Dashboard writes | Admin tokens only; read tokens view | DESIGN's admin scope for configuration; the UI hides controls a read token cannot use |
| UI sign-in | An existing admin or read token in an HttpOnly cookie; one token per browser | No users table or sessions table; revoking a token signs its browser out; amends the setup-URL bootstrap at the owner's request |
| UI layout and theme | Top header, full-width lists, detail pane on selection; light default with a stored dark choice; semantic colour tokens | Chosen from four screenshotted variants; scanning wants full width, reading wants a pane beside the list |
| Live tail cursor | Ingest-stamp watermark from the pinned hot table, plus the previous `to` | A `ts` cursor skips late records; stamps commit in order, so the hot maximum is a gap-free prefix |
| Live tail transport | Polling first; SSE later | No server push yet; the cursor is the same either way |
| Installable UI | Manifest and icons, no service worker | Installs on iOS and Android; no stale app shell after a deploy; push for alerts would add one |
| Frontend routing | TanStack Router, code-based routes, plain query strings | Typed search params for URL-encoded investigation state, without a router plugin or codegen |

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
