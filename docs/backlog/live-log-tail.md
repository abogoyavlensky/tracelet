# The logs explorer has no live view

**Status: done**

Shipped on branch `live-tail` by `docs/plans/2026-10-13-1115-live-tail-and-environment-fallback.md`.

## Problem

The logs explorer in the web UI searches a time range and reloads only when
the user refreshes. DESIGN's freshness section describes a live view: logs
read from a cursor, with a coalesced server-sent event after each commit
telling visible views what to refetch. Watching a deploy or reproducing a bug
today means pressing Refresh repeatedly.

## Proposed fix

Start with polling: while a "Live" toggle is on and the range is relative,
fetch the newest page every few seconds with `from` set to the newest record
already shown, and prepend what arrives, keeping the selected record and the
scroll position. Replace polling with the server-sent event once the server
publishes one after each commit. Stop when the tab is hidden.

## Origin

Deferred by `docs/plans/2026-10-12-1216-explore-ui.md` (12 October 2026),
which built the explorer without auto-refresh.
