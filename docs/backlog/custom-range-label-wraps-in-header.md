# A custom range wraps to two lines in the header between md and lg

**Status: open**

## Problem

At widths from 768 to about 1000 px, the header's time range button shows an
absolute range such as "Oct 18, 15:30 – Oct 18, 15:45" on two lines, which
makes the button taller than the header row and squeezes the project name to
"Sh…". It shows on every page that keeps its range in the header (logs,
traces, metrics, dashboards); the label in `TimeRangePicker` is allowed to
wrap. Seen in the dashboards end-to-end check at 800 px.

## Proposed fix

Keep the label on one line (`whitespace-nowrap`) and, below lg, show a
shorter form of an absolute range, such as times only when both ends fall on
the same day.

## Origin

Found while checking `docs/plans/2026-10-18-0827-dashboards.md` (18 October 2026).
