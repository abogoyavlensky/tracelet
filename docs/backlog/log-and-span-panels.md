# Dashboard panels can only chart metrics

**Status: open**

## Problem

A dashboard panel holds a `metric` query, so it charts OpenTelemetry metrics
only. DESIGN makes named application events structured logs, such as
`signup.completed`, for counts and breakdowns without a separate analytics
subsystem, but nothing counts logs or spans over time: the logs and spans
endpoints search records, they do not aggregate them. So a dashboard cannot
chart signups per hour, errors by service, or span latency by route.

## Proposed fix

Add log and span aggregation queries through `query.Snapshot`: counts per
step grouped by service, severity, or an attribute, and span duration
quantiles. Give a panel a `logs` or `spans` sibling of `metric`, exactly one
set, so stored dashboards keep their meaning, and offer Add to dashboard from
the logs and traces views.

## Origin

Out of scope in `docs/plans/2026-10-18-0827-dashboards.md` (18 October 2026).
