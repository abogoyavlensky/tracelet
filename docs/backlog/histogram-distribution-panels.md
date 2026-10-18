# Dashboards cannot show a histogram's distribution

**Status: open**

## Problem

DESIGN lists histograms among the first dashboard panel types, but a panel
can only chart what `GET /api/v1/metrics/query` returns: one value per step
and group, such as a quantile, a rate, or an average. Showing how a
histogram's observations spread across its buckets over the range needs the
merged bucket counts, and no query returns them. Panels are `timeseries`,
`stat`, or `table` (`internal/dashboard/dashboard.go`).

## Proposed fix

Add a query that merges a histogram's bucket deltas over the whole range per
group, with the same bounds rule and `incompatible_series` count as the
quantile query, and a `histogram` panel type that draws the buckets as bars.

## Origin

Out of scope in `docs/plans/2026-10-18-0827-dashboards.md` (18 October 2026).
