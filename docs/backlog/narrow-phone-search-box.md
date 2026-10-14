# The log search box shrinks to a sliver on a 320 px phone

**Status: open**

## Problem

On the logs page at 320 px wide, the search box shares one row with the
Filters, time range and Live buttons. The buttons keep their width, so the box
is left with about 30 px and shows only the start of its placeholder. At 390 px
it shows "Search m". Nothing overflows; the box is just hard to use. Found by
the phone-width audit in `docs/plans/2026-10-14-1105-pwa-and-mobile-fixes.md`.

## Proposed fix

Let the search box take its own row below 360 px or so (`basis-full` with
`order` on the filter bar in `web/src/components/FilterBar.tsx`), or drop the
text of the Live and time range buttons to icons there. Check the Metrics and
Traces filter bars, which share the component.
