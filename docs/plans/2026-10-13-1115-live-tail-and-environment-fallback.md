# Live Log Tail and the Deprecated Environment Attribute: Implementation Plan

> **For agentic workers:** Use executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The logs explorer and `tracelet logs search` can follow new logs as they arrive, without skipping any, and records sent with the pre-1.27 `deployment.environment` attribute file under their real environment instead of `default`.

**Tech Stack:** Go standard library, DuckDB through the existing `query.Snapshot`, testify; React, TypeScript, TanStack Query, vitest. No new Go or npm dependencies.

Work on a branch named `live-tail`, cut from `master`.

---

## Design

### What this delivers

1. **Environment fallback.** A resource with `deployment.environment` and no `deployment.environment.name` files under the old key's value. When both are present, the new key wins. This closes `docs/backlog/deprecated-environment-attribute.md`.
2. **Live tail in the API.** `GET /api/v1/logs` returns a `tail_cursor` on every response. A request with `since=<tail_cursor>` returns only the matching logs that the earlier response could not have shown, plus a new `tail_cursor`. Polling with it never skips a log silently: the only loss is a poll with more new records than `limit`, and that response says so with `truncated`. Every log in a response now carries its `ingest_ts`, which with `ts` identifies the record.
3. **Live tail in the UI.** A Live toggle on the logs page polls every 2 seconds while the range is relative and the tab is visible. New records are merged into the list in timestamp order. The selected record and the scroll position stay put.
4. **Live tail in the CLI.** `tracelet logs search --follow` prints the newest page, then new records as they arrive, until it is interrupted. With `--json` it prints one JSON object per line.

This closes `docs/backlog/live-log-tail.md`.

Out of scope:
- **A server-sent event after each commit.** DESIGN's freshness section still describes it. Polling is the first step that the backlog entry proposed, and the tail cursor stays valid when an event later replaces the timer.
- **Live mode for traces and metrics.**
- **OTLP/JSON, gRPC, and exponential histograms.** They keep their backlog entries.

### Why the tail cursor is an ingest stamp, not a timestamp

The backlog entry proposed polling with `from` set to the newest record's `ts`. That skips records. SDKs export in batches, and services' clocks differ, so a record with `ts` 12:00:04 can arrive after one with `ts` 12:00:05 is already on screen. A `from=12:00:05` poll never returns it, and nothing says it was missed.

Ingest stamps don't have this problem. The single writer gives every row a stamp, each a microsecond after the last, and commits batches in stamp order (`internal/duckdb/writer.go`, `Writer.Write`). So once a snapshot sees stamp W, it sees every row stamped at or below W. "Rows stamped after W" is therefore exactly "rows this snapshot did not see."

**The tail cursor** is opaque base64 JSON, like the page cursor, holding two values:

```go
// tail is a live-tail position: everything stamped at or below IngestTS,
// and everything before To, has already been answered.
type tail struct {
	IngestTS time.Time `json:"ingest_ts"`
	To       time.Time `json:"to"`
}
```

**A `since` request** adds one filter to the normal search:

```sql
(ingest_ts > ? OR ts >= ?)   -- tail.IngestTS, tail.To
```

The second half catches records that were already stored but had a timestamp at or after the previous response's `to`. A service whose clock runs ahead sends such "future" records. The previous range excluded them, so an ingest-stamp filter alone would skip them for good. With the second half, each poll picks them up once its own range reaches them.

**The watermark W** comes from a new `Snapshot.Watermark(ctx, signal)`, the greater of two values, each bounded by the pin:

- **The hot maximum:** `SELECT max(ingest_ts) FROM <signal>` on the snapshot's pinned transaction, over the hot table only. Within the pinned snapshot, hot stamps are a gap-free prefix.
- **The cold maximum:** `Manifest.MaxIngestTS`, read in `Open` after the gate slot is taken and **before** the pin, and kept on the `Snapshot`. Every file it counts already existed before the pin. Retention cannot unlink one while the snapshot holds its slot, so `Source`'s later manifest read lists it.

Rows flushed to Parquet before the pin are listed by the manifest that `Source` reads after the pin, so every row stamped at or below W is visible to the same request. The cost is one small SQLite query per snapshot.

Watermarks taken any other way are wrong:
- Taking the maximum over `Source`'s result can skip rows. `Source`'s cold half can include a late hour flushed after the pin with a higher stamp, while a lower-stamped row of the current hour is still invisible in hot.
- Taking the maximum over the returned items is wrong too: the page is ordered by `ts`, not by stamp.

Reading the manifest after the pin would be wrong too, which is why the cold maximum is read before it. Suppose hot logs are empty at the pin. A batch then writes log A in the current hour and a higher-stamped late log B in an old hour, and B's hour is flushed. A post-pin manifest maximum would cover B's stamp, so it would also cover A's, although A was invisible to the pinned query.

The response's `tail_cursor` uses `max(W, since.IngestTS)`, so the watermark never goes backwards.

The one race left can only **repeat** a row, never skip one: a late row committed after the pin and flushed in the same few milliseconds shows up in the cold half and again on the next poll. The UI de-duplicates by record identity, and the CLI accepts the rare repeat.

**Record identity.** `query.Log` gains `` IngestTS time.Time `json:"ingest_ts"` ``, already selected and scanned today. `(ts, ingest_ts)` is unique per row (DESIGN's ingest stamps decision). The frontend's `logKey` becomes `` `${ts}\u0000${ingest_ts}` ``; the old key of time, span, service, and body was not unique, and merging would have dropped real records that shared it. `LogList` keys its rows by `logKey` instead of `` `${log.ts}-${i}` ``. Otherwise rows inserted above would remount every row below and re-run the selected row's `scrollIntoView` effect.

**A `since` response:**
- has no `next_cursor`, because a tail is not paged;
- has `truncated: true` when more than `limit` new records matched. In that case it returns the newest `limit` records, and the client must not pretend it has the rest: the UI resets to a fresh first page, and the CLI prints a warning to stderr;
- uses the request's range as usual, so `last=15m` slides forward with each poll.

`since` cannot be combined with `cursor` (400). It can be combined with `from`/`to`/`last`, or with none of them, in which case the range is the default last hour.

### UI behaviour

- **URL.** `live=1` in the logs URL turns live mode on, so a bookmark can open a live view. It only takes effect when the range is relative (`last`, or no range at all). With an absolute range, the toggle is disabled with the title "Live needs a relative time range".
- **Toggle.** A "Live" button sits in the filter bar before the summary, on phones too. When on, it shows a pulsing dot in the `accent` token. If the last poll failed, it switches to the `sev-warn` tone and its title carries the error.
- **Polling.** A `useLiveTail` hook runs a `setTimeout` chain: one poll 2 seconds after the previous one finishes, so polls never overlap.
  - It pauses while `document.visibilityState` is `hidden` and polls immediately when the tab becomes visible.
  - Each poll goes through `queryClient.fetchQuery` with `gcTime: 0`. A 401 then reaches the shared `QueryCache.onError` and redirects to sign-in, as for every other query.
- **Stale responses.** A poll in flight when the filters change, or a reset happens, must not write its result. The hook keeps a generation counter, bumped on every re-seed and reset; a response from an older generation is dropped.
- **Testable state.** The hook's state changes (seed, poll response, reset, dismiss) are a pure reducer, `liveReducer(state, event)` in `live.ts`, tested with vitest. The hook only owns timers, visibility, and `fetchQuery`. That is how stale-generation, window, cap, and truncation behaviour get tests without adding a React testing library.
- **Merging.** Arrivals live in hook state, separate from the infinite query's pages. The list shows `mergeLive(arrivals, basePages, from)`: the union, de-duplicated by `logKey`, sorted newest first. Comparisons use `nsSince` from `format.ts`; timestamps are compared as times, not as strings.
- **Window.** Every successful poll, empty or not, carries its resolved `range`. The hook keeps the latest `range.from`, and `mergeLive` drops records older than it, so a "last 15 minutes" view never shows a record from 16 minutes ago. While live, "Load older" is hidden: live is a tail, and paging back means turning Live off.
- **Reset.** The hook resets the infinite query (`queryClient.resetQueries` on its key, which refetches only the first page) and clears arrivals when either:
  - a poll comes back `truncated`, or
  - arrivals pass 500 records, so the DOM stays bounded.

  A reset after truncation sets a sticky `skippedAt` time, so the loss is not silent. The page shows a notice above the list: "More records arrived at HH:MM:SS than live could show; some were skipped. Turn Live off to search them." It stays until it is dismissed, Live is turned off, or the filters change. A reset for the 500 cap skips nothing: every dropped arrival was already on screen, and a search still finds it. So it shows no notice.
- **Re-seeding.** The hook clears arrivals and takes the tail cursor from `pages[0].tail_cursor` whenever the first page **object** changes: Refresh, a filter change, or a reset. "Load older" appends a page and keeps `pages[0]` by reference, so it does not re-seed. Do not key this on `dataUpdatedAt`, which changes on "Load older" too.
- **Selection.** It stays keyed by record, as now. A selected record pushed out by a reset closes the pane, which is the existing behaviour for a record that disappears.
- **Scrolling.** New rows insert above. Browser scroll anchoring (`overflow-anchor`, on by default) keeps a scrolled list in place. Check this in Task 7.
- **Summary.** In live mode, the filter bar's summary reads `N records, live, newest first`.

### CLI behaviour

- `--follow` refuses `--cursor`, `--from`, and `--to` with a usage error. It accepts `--last` or no range.
- It prints the first page **oldest first**, then each poll's new records oldest first, like `tail -f`. A late record prints when it arrives, so the stream is in arrival order, not strictly in `ts` order.
- `--interval` sets the poll interval (default `2s`).
- A truncated poll prints `more than N new records since the last poll; some were not shown` to stderr.
- With `--json`, every record is one JSON object on its own line (JSON Lines). A single JSON document cannot stream.
- On SIGINT or SIGTERM it exits 0.

`cli.Run` today wraps every command in one `context.WithTimeout(..., time.Minute)`, which would end `--follow` after a minute. Instead:
- `Run` becomes a thin wrapper over a new exported `RunContext(ctx, args, stdout, stderr)`.
- `Run` builds its context with `signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)`.
- The one-minute bound is already per request: `newFlags`' env sets `http.Client{Timeout: time.Minute}` (`internal/cli/cli.go`), so dropping `Run`'s overall deadline changes nothing for other commands.

Every existing command keeps the same one-minute bound per request. Tests call `RunContext` with a context they cancel.

### Testing

- `internal/otlp`: conversion tests for the old key, the new key, and both.
- `internal/query`, `since` mode:
  - returns only rows written after the first page's snapshot;
  - returns a row with an older `ts` that was written later, the late-arrival case the `ts` cursor would skip;
  - returns a row stored before the first page with `ts` at or after its `to`;
  - truncates past `limit` with no `next_cursor`;
  - returns nothing when nothing new arrived;
  - keeps the watermark when hot is empty after a flush, falling back to the manifest;
  - does not skip a row written and hidden behind a later flush while a snapshot is open (the case above);
  - carries `ingest_ts` on every item;
  - rejects an invalid tail cursor.
- `internal/httpapi`: `since` round trip, `since` with `cursor` is 400, `tail_cursor` is present on first pages.
- `internal/cli`: `--follow` against the fake API (first page oldest first, then a tail page, then cancel); `--json` lines; usage errors; the truncation warning.
- `web/src/live.test.ts`: `liveReducer` (seeding takes the first page's cursor and clears arrivals; a stale-generation response is ignored; a truncated response asks for a reset and sets `skippedAt`; passing 500 arrivals asks for a reset without `skippedAt`; an empty response still advances the window and the cursor; dismiss clears `skippedAt`), merge order, the window drop, de-duplication by `(ts, ingest_ts)` against base and earlier arrivals, two records identical but for `ingest_ts` both kept, nanosecond-precision ordering, the eligibility rule, and tail params.
- An end-to-end run with `telemetrygen` against `rite dev` and a screenshot of live mode.

## File Structure

- Modify `internal/otlp/resource.go`: fall back to `deployment.environment`.
- Modify `internal/otlp/logs_test.go`: environment key cases.
- Modify `internal/query/snapshot.go`: the cold maximum read in `Open`, and `Watermark`.
- Modify `internal/query/range.go`: the `tail` type, `encodeTail`, `decodeTail`.
- Modify `internal/query/logs.go`: `Log.IngestTS`, `LogsParams.Since`, `LogsPage.TailCursor`, the `since` filter, no `next_cursor` in `since` mode.
- Modify `internal/query/logs_test.go`: `since` tests.
- Modify `internal/httpapi/logs.go`: parse `since`; reject it with `cursor`.
- Modify `internal/httpapi/logs_test.go`.
- Modify `api/openapi.yaml`: the `since` parameter, `LogsPage.tail_cursor`, `Log.ingest_ts`, the description.
- Modify `internal/cli/cli.go`: `RunContext`, signal context, no overall deadline.
- Modify `internal/cli/logs.go`: `--follow`, `--interval`, JSON Lines.
- Modify `internal/cli/cli_test.go`: follow tests.
- Modify `cmd/tracelet/main.go` only if `Run`'s signature changes; it should not.
- Modify `web/src/api.ts`: `since` in `LogsParams`, `tail_cursor` in `LogsPage`, `ingest_ts` in `Log`.
- Modify `web/src/components/LogList.tsx`: `logKey` from `(ts, ingest_ts)`; rows keyed by it.
- Modify `web/src/search.ts`: `live?: true` in `LogsSearch`, validated as a boolean flag; `logsApiParams` never sends it.
- Create `web/src/live.ts`: `liveReducer`, `mergeLive`, `liveEligible`, `tailParams`. Create `web/src/live.test.ts`.
- Create `web/src/components/useLiveTail.ts`: the polling hook.
- Modify `web/src/components/FilterBar.tsx`: an optional `actions` slot.
- Modify `web/src/pages/LogsPage.tsx`: the Live toggle, merged items, live summary.
- Modify `README.md`, `docs/DESIGN.md`, and the two backlog entries.

## Tasks

### Task 0: Branch and backlog status

**Files:**
- Modify: `docs/backlog/live-log-tail.md`, `docs/backlog/deprecated-environment-attribute.md`

- [x] **Step 1:** `git switch -c live-tail master`.
- [x] **Step 2:** In both backlog entries, change `**Status: open**` to `**Status: planned**`, with `Plan: docs/plans/2026-10-13-1115-live-tail-and-environment-fallback.md` on the next line.
- [x] **Step 3:** Commit the plan and the two status changes: `git commit -m "Plan live log tail and the environment attribute fallback"`.

### Task 1: Environment attribute fallback

**Files:**
- Modify: `internal/otlp/resource.go`
- Test: `internal/otlp/logs_test.go`

- [x] **Step 1: Write the failing tests.** Add a table test converting a logs request whose resource has (a) only `deployment.environment=staging`, which gives `staging`; (b) both keys, `deployment.environment.name=prod` and `deployment.environment=staging`, which gives `prod`; (c) neither, which gives `default`. The resource JSON keeps whatever attributes were sent.
- [x] **Step 2:** Run `go test ./internal/otlp/ -run Environment`. Expected: FAIL on (a).
- [x] **Step 3: Implement.** Add `attrEnvironmentDeprecated = "deployment.environment"` and read it in `resourceInfo` only when the new key is empty. Update `DefaultEnvironment`'s comment. Spans and metrics share `resourceInfo`, so they get the fallback too. The metric series hash covers the raw resource attributes and is unchanged.
- [x] **Step 4:** Run `go test ./internal/otlp/`. Expected: PASS.
- [x] **Step 5: Docs.** In `README.md`, after the sentence listing the three resource attributes, add: `` Records sent with the older `deployment.environment` key, which semantic conventions before v1.27 used, file under its value when `deployment.environment.name` is absent. `` In `docs/DESIGN.md`, change the Environment row's note to `Optional, defaults to \`default\`; falls back to the pre-1.27 \`deployment.environment\``.
- [x] **Step 6: Commit.** `git commit -m "Fall back to the deprecated deployment.environment attribute"`

### Task 2: Tail cursor and `since` in the logs query

**Files:**
- Modify: `internal/query/snapshot.go`, `internal/query/range.go`, `internal/query/logs.go`
- Test: `internal/query/logs_test.go`

- [x] **Step 1: Write the failing tests** with the existing `searchEnv`/`newEnv` helpers. Write rows with `e.writer.Write` between searches so stamps increase.
  - **`TestLogsTailReturnsOnlyNewRows`:** search the range and keep `TailCursor`. Write two rows, one with a `ts` older than the newest row already shown (a late arrival). Search with `Since`: both come back, newest `ts` first. Search again with the new cursor: nothing.
  - **`TestLogsTailCatchesFutureRows`:** write a row with `ts` after the range's `to`. Search `[from, to)` and keep the cursor. Search with `Since` and a range that now includes that `ts`: the row comes back.
  - **`TestLogsTailTruncates`:** write `limit+1` new rows. The `Since` search returns `limit` items, `Truncated` is true, and `NextCursor` is empty.
  - **`TestLogsTailAfterFlush`:** flush everything so hot logs are empty. A first search's cursor still carries the manifest's cutoff, so a `Since` search returns nothing old.
  - **`TestLogsTailFlushDuringSnapshot`:** flush so hot logs are empty, then open a snapshot S. Write log A in the latest hour and a later-stamped late log B in an old hour, then flush B's hour. `LogsSearch` on S does not show A. A `Since` search with S's `TailCursor` on a fresh snapshot returns A.
  - Every item carries a non-zero `IngestTS`, and the stamps are distinct.
  - **Validation:** `Since` together with `Cursor` and a garbage `Since` are both validation errors.
- [x] **Step 2:** Run `go test ./internal/query/ -run LogsTail`. Expected: FAIL (fields missing).
- [x] **Step 3: Implement the watermark.** In `Open`, after `gate.Acquire` and before `BeginTx`, read `m.MaxIngestTS(ctx)` into a new `Snapshot.coldMax` field. Add `Snapshot.Watermark(ctx, signal) (time.Time, error)`:
  - check the signal with `duckdb.CheckSignal`;
  - run `SELECT max(ingest_ts) FROM <signal>` on `s.tx`;
  - return the later of that and `s.coldMax`, or the zero time when both are empty.
  Its doc comment must state the prefix argument from the design and why both a maximum over `Source` and a post-pin manifest read would be wrong. Add `IngestTS` to `query.Log` and set it in `scanLog`.
- [x] **Step 4: Implement the tail cursor** in `range.go`: `tail`, `encodeTail`, and `decodeTail`, mirroring `encodeCursor`/`decodeCursor`. A decoded tail with a zero `To` is invalid; a zero `IngestTS` is allowed.
- [x] **Step 5: Implement `since` in `LogsSearch`.**
  - Add `LogsParams.Since string` and `` LogsPage.TailCursor string `json:"tail_cursor,omitempty"` ``.
  - Reject `Since` with `Cursor` (`invalid("since cannot be combined with cursor")`).
  - With `Since`, decode it and add `f.add("(ingest_ts > ? OR ts >= ?)", t.IngestTS, t.To)`.
  - After the query, take `Watermark(ctx, "logs")` from the same snapshot. Set `TailCursor` to `encodeTail(tail{IngestTS: max(W, since.IngestTS), To: rng.To})` on every response, including `cursor` pages; it is harmless there.
  - In `since` mode, keep `Truncated` but never set `NextCursor`.
  - Update `LogsSearch`'s doc comment.
- [x] **Step 6:** Run `go test -race ./internal/query/`. Expected: PASS.
- [x] **Step 7: Commit.** `git commit -m "Add a live-tail cursor to the logs search"`

### Task 3: `since` over HTTP and in OpenAPI

**Files:**
- Modify: `internal/httpapi/logs.go`, `api/openapi.yaml`
- Test: `internal/httpapi/logs_test.go`

- [x] **Step 1: Write the failing tests.**
  - `TestLogsEndpointTail`: the first response has `tail_cursor`; after more rows are written, `?since=<it>&last=...` returns only them.
  - `cursor` plus `since` is 400.
  - A bad `since` is 400.
- [x] **Step 2:** Run `go test ./internal/httpapi/ -run Logs`. Expected: FAIL.
- [x] **Step 3: Implement.** Pass `q.Get("since")` into `LogsParams.Since`. `parseSearch` already rejects range parameters only with `cursor`, so `since` with a range needs no change there. Add `tail_cursor` to `pageBody` in the test.
- [x] **Step 4: OpenAPI.** Add `- { name: since, in: query, description: A tail_cursor from an earlier response; returns only logs that response could not have shown. Not with cursor., schema: { type: string } }`. Add `tail_cursor: { type: string, description: Pass as since to fetch only newer logs. }` to `LogsPage`, and `ingest_ts: { type: string, format: date-time, description: When the server stored the record; with ts it identifies the record. }` to `Log`. Extend the operation description with one paragraph on live tailing: `since` responses have no `next_cursor`, and `truncated` means records were left out.
- [x] **Step 5:** Run `go test ./internal/httpapi/`. Expected: PASS, including the OpenAPI coverage test.
- [x] **Step 6: Commit.** `git commit -m "Serve the logs tail cursor over the API"`

### Task 4: `tracelet logs search --follow`

**Files:**
- Modify: `internal/cli/cli.go`, `internal/cli/logs.go`
- Test: `internal/cli/cli_test.go`

- [ ] **Step 1: Restructure `Run`.**
  - Add `RunContext(ctx context.Context, args []string, stdout, stderr io.Writer) int`, holding today's body minus the one-minute context.
  - `Run` calls it with `signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)`.
  - Drop the overall one-minute deadline; the HTTP client's `Timeout: time.Minute` already bounds each request.
  - `os/signal` and `syscall` are standard library and fine in the `cli` build.
- [ ] **Step 2:** Run `go test ./internal/cli/`. Expected: PASS, unchanged behaviour.
- [ ] **Step 3: Write the failing tests.** Extend `fakeAPI` so `/api/v1/logs` serves a first page with `tail_cursor`, then, when `since` is present, a page of new records, then empty pages. Add a `runCtx` helper calling `cli.RunContext`.
  - `TestLogsFollow`: with `--follow --interval 10ms`, cancel the context once the fake has served two `since` requests. Check that exit is 0, the first page printed oldest first, then the new records, and that each `since` request carried the latest cursor and the same filters.
  - `TestLogsFollowJSONLines`: every stdout line parses as one log object.
  - `TestLogsFollowTruncated`: a `truncated` tail page prints the warning to stderr.
  - Usage errors: `--follow` with `--cursor`, with `--from`, and with `--to` exit 2.
- [ ] **Step 4:** Run `go test ./internal/cli/ -run Follow`. Expected: FAIL.
- [ ] **Step 5: Implement in `logs.go`.**
  - Add the `--follow` and `--interval` (default `2s`) flags.
  - Factor the existing row printing into a function that takes the items and a direction.
  - The follow loop: fetch, print oldest first, sleep for the interval or until `ctx` is done, then fetch with `since`. A cancelled context returns `nil`; any other error returns as now.
  - Decode `items` as `[]json.RawMessage` for the JSON path, so records print exactly as the server sent them.
  - Update the usage text.
- [ ] **Step 6:** Run `go test -race ./internal/cli/` and `rite check-cli`. Expected: PASS.
- [ ] **Step 7: Commit.** `git commit -m "Follow new logs with tracelet logs search --follow"`

### Task 5: Frontend live-tail logic

**Files:**
- Modify: `web/src/api.ts`, `web/src/search.ts`
- Create: `web/src/live.ts`, `web/src/live.test.ts`
- Test: `web/src/search.test.ts`

- [ ] **Step 1: Types and identity.** Add `ingest_ts: string` to `Log`, and change `logKey` in `LogList.tsx` to `` `${log.ts}\u0000${log.ingest_ts}` `` with its comment updated. Key `LogRow` by `logKey(log)`. `SpanDetail` already uses `logKey`, so it follows. Add `since?: string` to `LogsParams` and `tail_cursor?: string` to `LogsPage`; check whether `Page<T>` is shared with spans and add the field to the logs page type only. Add `live?: true` to `LogsSearch`. `validateLogsSearch` accepts `live` as `true`, `'1'`, or `'true'` and drops anything else. `logsApiParams` must not forward `live`.
- [ ] **Step 2: Write the failing tests** in `live.test.ts`:
  - `liveReducer` covers each case listed under Testing in the design.
  - `mergeLive(arrivals, base, from)` drops records older than `from` and returns the union newest first and de-duplicates by `logKey` across both lists and within arrivals. Two records equal in everything but `ingest_ts` are both kept.
  - It orders two timestamps that differ only in nanoseconds correctly.
  - It places a late arrival (older `ts`) inside the base list, not on top.
  - `liveEligible(range)` is true for `{last}` and `{}` and false for `{from,to}`.
  - `tailParams(search, cursor)` equals `logsApiParams(search)` plus `since` and `limit: 1000`. The maximum page makes truncation need more than 500 records a second.
  - Add a `search.test.ts` case for `live`.
- [ ] **Step 3:** Run `cd web && npx vitest run src/live.test.ts src/search.test.ts`. Expected: FAIL.
- [ ] **Step 4:** Implement `web/src/live.ts`. The reducer's state is `{ generation, cursor, from, arrivals, skippedAt }`, and its result tells the hook whether to reset the base query. Sort with `nsSince` from `format.ts`; if two records compare equal, keep arrivals before base.
- [ ] **Step 5:** Run the same vitest command. Expected: PASS.
- [ ] **Step 6: Commit.** `git commit -m "Add live-tail merging and search params"`

### Task 6: Live toggle on the logs page

**Files:**
- Create: `web/src/components/useLiveTail.ts`
- Modify: `web/src/components/FilterBar.tsx`, `web/src/pages/LogsPage.tsx`

- [ ] **Step 1: The hook.** Write `useLiveTail({ enabled, search, firstPage, queryKey })`, which returns `{ arrivals, from, skippedAt, dismiss, error }`, driving `liveReducer` with `useReducer`. Its behaviour follows the design's UI section:
  - a `setTimeout` chain at 2000 ms, starting only after `firstPage` exists;
  - pausing on hidden visibility and polling immediately on visible;
  - `queryClient.fetchQuery({ queryKey: ['logs-tail', params], queryFn, gcTime: 0, staleTime: 0 })`;
  - re-seeding when the `firstPage` reference changes;
  - resetting via `queryClient.resetQueries({ queryKey })` on truncation or more than 500 arrivals;
  - clearing timers on unmount or when `enabled` turns false;
  - dropping any response from an older generation, where a re-seed or reset starts a new generation.
  Keep the constants (`pollMs = 2000`, `maxArrivals = 500`) at the top of the file.
- [ ] **Step 2: FilterBar.** Add an optional `actions?: ReactNode`, rendered just before the summary at every width.
- [ ] **Step 3: LogsPage.**
  - Compute `live = !!search.live && liveEligible(range)` and call the hook.
  - Build `items` with `mergeLive(arrivals, basePages, from)` when live, and hide "Load older".
  - Show the skipped-records notice while `skippedAt` is set, with a dismiss button; use the `sev-warn` tone.
  - Render the Live toggle: it sets `live: true` or `undefined` through `set`, is disabled with a title when the range is not relative, and shows the pulsing `accent` dot, or the warning tone with the error as its title.
  - Change the summary in live mode.
  - Use only semantic colour tokens from `web/src/index.css`.
- [ ] **Step 4:** Run `cd web && npm run lint && npx vitest run`. Expected: PASS.
- [ ] **Step 5: Commit.** `git commit -m "Live toggle on the logs page"`

### Task 7: End-to-end check

- [ ] **Step 1:** Run `rite web-build`, then `rite dev` in the background. Create a project and an ingest token with the CLI, following `docs/operations.md`'s local steps.
- [ ] **Step 2:** Stream logs: `go run github.com/open-telemetry/opentelemetry-collector-contrib/cmd/telemetrygen@latest logs --otlp-http --otlp-insecure --otlp-endpoint localhost:8080 --otlp-header 'Authorization="Bearer tl_..."' --rate 20 --duration 5m --otlp-attributes 'deployment.environment="staging"'`. It must not be added to `go.mod`; check its `--help` if a flag name differs.
- [ ] **Step 3: Live UI.** Sign in through the preview browser (`mcp__t3-code__preview_*`) and open `/logs?last=15m&live=1`. Check each of these:
  - rows appear within about 3 s;
  - a selected record stays open while rows arrive, and the list neither jumps nor re-scrolls to it;
  - a list scrolled down does not jump;
  - the toggle is disabled with an absolute range;
  - with `last=1m` and the generator stopped, records leave the list as they age past a minute;
  - a burst above the limit (`telemetrygen --rate 1000` for a few seconds) shows the skipped notice;
  - switching the tab away stops requests (watch the server log);
  - the environment filter `staging` matches the generated records, which proves the fallback end to end.
  Take screenshots in light and dark.
- [ ] **Step 4: Live CLI.** Run `go run ./cmd/tracelet logs search --follow --last 5m` against the same server for about 20 s and confirm that new lines stream. Stop it with Ctrl-C and confirm it exits 0.
- [ ] **Step 5:** Note poll latency from the server log, and anything surprising, in this plan under the task.

### Task 8: Documentation and backlog

**Files:**
- Modify: `docs/DESIGN.md`, `README.md`, `docs/backlog/live-log-tail.md`, `docs/backlog/deprecated-environment-attribute.md`

- [ ] **Step 1: DESIGN.**
  - Under "Logs and application events", add a **Live tail (built)** paragraph: the tail cursor, polling every 2 s, and the SSE still to come.
  - Add decision log rows:
    - "Live tail cursor | Ingest-stamp watermark from the pinned hot table, plus the previous `to` | A `ts` cursor skips late records; stamps commit in order, so the hot maximum is a gap-free prefix".
    - "Live tail transport | Polling first; SSE later | No server push yet; the cursor is the same either way".
- [ ] **Step 2: README.** Document `--follow` in the CLI section and the Live toggle where the explorer is described.
- [ ] **Step 3: Backlog.** Set both entries to `**Status: done**`, each with a line saying it shipped on branch `live-tail` by this plan.
- [ ] **Step 4: Commit.** `git commit -m "Document the live tail and the environment fallback"`

### Task 9: Check and pull request

- [ ] **Step 1:** Run `rite check`. Expected: every step passes.
- [ ] **Step 2:** Push `live-tail` and open a PR. It summarises both changes, explains why the tail cursor is an ingest stamp, attaches the Task 7 screenshots, and lists the new `since` parameter and `tail_cursor` field as API additions. Link the PR to the thread with `mcp__t3-code__link_pull_request`.
- [ ] **Step 3:** Wait for `ci` to pass on both architectures. Do not merge; the owner merges, which deploys.
