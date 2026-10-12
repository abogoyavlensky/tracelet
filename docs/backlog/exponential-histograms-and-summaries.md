# Exponential histograms and summaries are rejected

**Status: open**

## Problem

`POST /v1/metrics` stores gauges, sums, and explicit-bucket histograms.
Exponential histogram points are rejected with "exponential histogram metrics
are not supported yet" and summary points with "summary metrics are not
supported", both counted in `rejected_rows` and named in the partial success
response (`internal/otlp/metrics.go`). An SDK configured with the
`base2_exponential_bucket_histogram` aggregation, or a Prometheus bridge that
emits summaries, loses those metrics.

## Proposed fix

Store exponential buckets (scale, zero count, positive and negative offset
and counts) in new `metric_points` columns, and convert them to explicit
bounds at query time so quantiles reuse the existing interpolation; merging
across series needs a common scale, which downscaling gives. For summaries,
decide whether to store their precomputed quantiles as gauges named after the
quantile, accepting that they cannot be merged across series.

## Origin

Out of scope in `docs/plans/2026-10-12-2159-metrics-slice.md` (12 October 2026).
