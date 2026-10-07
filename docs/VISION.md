# Tracelet

**Lightweight observability for your projects.**

Status: project vision from the design discussion of 7 October 2026, revised after review the same day. This document records the direction and the boundaries. Technical decisions live in [DESIGN.md](DESIGN.md). Nothing here claims that a feature has been built or a target measured.

## Purpose

Tracelet is a self-hosted observability application for personal web and mobile projects and their backend services. It brings logs, traces, metrics, errors, and alerts together in one small system that is easy to run, back up, and upgrade.

One executable, one process, one data directory, one web interface. A single Docker container with one persistent volume is an equally supported deployment. No external database, message broker, or cloud account is required.

Tracelet should answer everyday questions:

- Is my application healthy?
- Which errors keep happening?
- What changed after the last deployment?
- Why was this request slow?
- Did a background job stop running?

Every answer must be available through the UI, the HTTP API, and a small CLI. Coding agents are first-class users of the API and CLI. MCP is not planned: a documented OpenAPI surface and a JSON-emitting CLI are sufficient for agents and are more stable than a protocol still changing.

## Who it is for

A developer running a handful of projects on modest infrastructure, typically one small VPS. The product must be reliable enough for daily use and flexible enough for common application monitoring needs. The initial focus is backend services instrumented with OpenTelemetry. Browser and mobile apps can send telemetry to the same endpoints, but client SDK guidance comes after the server-side path is trustworthy.

## What it is not

Tracelet is not a distributed observability platform, an enterprise multi-tenant service, or a replacement for every feature of Grafana, Sentry, or ClickStack. Out of scope for the foreseeable future: clustering, enterprise SSO, fine-grained permissions, session replay, profiling, real user monitoring and Web Vitals, product-analytics funnels, and a free-form SQL editor or custom query language.

## Principles

- Keep installation, configuration, backup, and upgrades understandable.
- Prefer established telemetry formats and instrumentation over proprietary SDKs.
- Provide useful defaults before asking users to build dashboards.
- Make logs, traces, metrics, and errors easy to investigate together.
- Make data loss, sampling, query truncation, and ingestion delay visible. Never silently acknowledge discarded data.
- Bound memory, query concurrency, queues, and disk consumption.
- Use durable state and safe retries where work must survive restarts.
- Add infrastructure only when its benefit justifies its complexity.
- Maintain one query model shared by the UI, API, CLI, and alerts.
- Ship API and CLI access with each feature, not after it.

## Positioning

The lightweight single-binary field already exists. OpenObserve is one binary covering logs, metrics, and traces. VictoriaLogs, VictoriaMetrics, and VictoriaTraces run in tens of megabytes each. O11yLite embeds the whole stack. All of these are telemetry databases first: you bring Grafana or build your own views, and they know nothing about your application beyond the data.

Tracelet's difference is not footprint. It is an opinionated application monitor: correlated signals with useful defaults, error grouping, deployment awareness, heartbeat checks, and alerts, in one process, with equal access for humans and coding agents. A smaller footprint than alternatives is a hypothesis to measure, not a claim.

## What ships, in order

0. **Storage spike.** A synthetic load generator and a multi-day benchmark that prove the storage layout, retention, backup, and concurrent query behaviour inside the resource budget. Nothing else starts until this is trustworthy.
1. **Foundation.** Packaging, authentication, projects, OTLP ingestion, logs and traces exploration with correlation, heartbeat checks, retention, basic API and CLI.
2. **Understanding the application.** Metrics with correct semantics, error grouping, deployment markers, overview defaults, saved searches, versioned dashboards, discovery and query API and CLI.
3. **Dependable alerts.** Threshold rules on the shared query model, persistent state, webhook retries, health visibility, validated backup and restore.
4. **Investigation improvements.** Alert silences and maintenance windows, metric exemplars, rollups for long retention, additional notification channels.

Stop expanding scope until the core path is trustworthy.

## Open decisions at the vision level

- Confirm project-name and domain availability.
- Choose a licence.
