# Metrics are not ingested

**Status: open**

## Problem

The `metric_points` hot table exists and moves through flush and retention,
but there is no `POST /v1/metrics`, no conversion from OTLP gauges, sums,
and histograms, and no metric query. `telemetry.MetricPoint.SeriesHash` is
stored but nothing computes it: the spike's `telemetry.SeriesHash` (FNV-64a
over name, service, and sorted attributes) was dropped in the port because
nothing used it; it is in the spike at commit `52c70cc`.

## Proposed fix

Restore the series hash, convert each data point kind to rows as the design's
metric points table describes, and deduplicate on series hash, start
timestamp, and timestamp. Counter resets and histogram quantiles need the
phase 0 correctness checks ported to tests.

## Origin

Out of scope in `docs/plans/2026-10-10-1027-logs-slice.md` (10 October 2026).
