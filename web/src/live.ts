// Live tail: the pure part. The hook in components/useLiveTail.ts owns timers
// and fetching; what a poll's result does to the view is decided here, so
// stale responses, the time window, and the caps are tested without React.
import type { Log, LogsPage, LogsParams } from './api'
import { nsSince } from './format'
import { type LogsSearch, logsApiParams } from './search'
import type { TimeRange } from './timeRange'

/** How many arrivals live keeps before it starts over from a fresh first page. */
export const maxArrivals = 500

/** The largest page the API serves: truncation then needs more than 500 records a second. */
export const tailLimit = 1000

/** (ts, ingest_ts) is unique per record; the API guarantees it. */
export function logKey(log: Log): string {
  return `${log.ts}\u0000${log.ingest_ts}`
}

/** Live follows "now", so it needs a range that ends there. */
export function liveEligible(range: TimeRange): boolean {
  return !('from' in range)
}

export function tailParams(search: LogsSearch, cursor: string): LogsParams {
  return { ...logsApiParams(search), since: cursor, limit: tailLimit }
}

export type LiveState = {
  /** Bumped on every re-seed, reset, and restart; a poll answers for one generation. */
  generation: number
  /** Where the next poll starts; unset until a first page seeds it and while a reset is under way. */
  cursor: string | undefined
  /** Start of the window the latest poll searched; older records leave the view. */
  from: string | undefined
  /** Records that arrived after the first page, newest first. */
  arrivals: Log[]
  /** When live last had to skip records, until dismissed. */
  skippedAt: string | undefined
  /** The base query must be reset; cleared when its new first page seeds. */
  resetting: boolean
}

export const initialLive: LiveState = {
  generation: 0,
  cursor: undefined,
  from: undefined,
  arrivals: [],
  skippedAt: undefined,
  resetting: false,
}

export type LiveEvent =
  /** The base query's first page changed: take its cursor, drop the arrivals. */
  | { type: 'seed'; page: LogsPage }
  /** A poll answered; `generation` is the one it was started for. */
  | { type: 'poll'; generation: number; page: LogsPage; now: string }
  /** The filters changed or live was turned off: forget everything. */
  | { type: 'restart' }
  | { type: 'dismiss' }

export function liveReducer(state: LiveState, event: LiveEvent): LiveState {
  switch (event.type) {
    case 'seed':
      return {
        ...state,
        generation: state.generation + 1,
        cursor: event.page.tail_cursor,
        from: event.page.range.from,
        arrivals: [],
        resetting: false,
      }
    case 'restart':
      return { ...initialLive, generation: state.generation + 1 }
    case 'dismiss':
      return { ...state, skippedAt: undefined }
    case 'poll': {
      if (event.generation !== state.generation) return state
      const { page } = event
      if (page.truncated) {
        return { ...startOver(state), skippedAt: event.now }
      }
      const arrivals = insertNewest(state.arrivals, page.items)
      if (arrivals.length > maxArrivals) {
        // Every dropped arrival was on screen and a search still finds it.
        return startOver(state)
      }
      return {
        ...state,
        cursor: page.tail_cursor ?? state.cursor,
        from: page.range.from,
        arrivals,
      }
    }
  }
}

/** Waits for a fresh first page: nothing polls until the next seed. */
function startOver(state: LiveState): LiveState {
  return {
    ...state,
    generation: state.generation + 1,
    cursor: undefined,
    arrivals: [],
    resetting: true,
  }
}

/** Newest first by (ts, ingest_ts); ts is compared as a time, to the nanosecond. */
function newestFirst(a: Log, b: Log): number {
  return nsSince(a.ts, b.ts) || nsSince(a.ingest_ts, b.ingest_ts)
}

function insertNewest(arrivals: Log[], items: Log[]): Log[] {
  if (items.length === 0) return arrivals
  return dedupe([...items, ...arrivals]).toSorted(newestFirst)
}

function dedupe(logs: Log[]): Log[] {
  const seen = new Set<string>()
  return logs.filter((l) => {
    const k = logKey(l)
    if (seen.has(k)) return false
    seen.add(k)
    return true
  })
}

/**
 * What the live list shows: the arrivals and the base pages together, each
 * record once, newest first, without anything older than the window.
 */
export function mergeLive(arrivals: Log[], base: Log[], from: string | undefined): Log[] {
  const all = dedupe([...arrivals, ...base])
  const inWindow = from === undefined ? all : all.filter((l) => nsSince(l.ts, from) <= 0)
  // Sorting is stable, so arrivals win ties.
  return inWindow.toSorted(newestFirst)
}
