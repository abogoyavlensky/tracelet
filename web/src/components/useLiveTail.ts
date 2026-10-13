// Polls the logs API for records newer than the first page and hands them to
// the page. Timers, tab visibility, and fetching live here; what a response
// does to the view is liveReducer's job (live.ts).
import { type QueryKey, useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useReducer, useRef, useState } from 'react'
import { type Log, type LogsPage, searchLogs } from '../api'
import { initialLive, liveReducer, tailParams } from '../live'
import { type LogsSearch, logsApiParams } from '../search'

/** Time between the end of one poll and the start of the next. */
const pollMs = 2000

export function useLiveTail({
  enabled,
  search,
  firstPage,
  queryKey,
}: {
  enabled: boolean
  /** The filters, with the project filled in. */
  search: LogsSearch
  /** The base query's first page; a new object seeds live from its tail cursor. */
  firstPage: LogsPage | undefined
  /** The base query, reset when live must start over from a fresh first page. */
  queryKey: QueryKey
}): {
  arrivals: Log[]
  /** Start of the window the latest poll searched. */
  from: string | undefined
  skippedAt: string | undefined
  dismiss: () => void
  /** The last poll's error, until a poll succeeds. */
  error: string | undefined
} {
  const queryClient = useQueryClient()
  const [state, dispatch] = useReducer(liveReducer, initialLive)
  const [error, setError] = useState<string>()

  // The poll loop and the reset read these when they fire, not when they were
  // scheduled.
  const latest = useRef({ search, queryKey, cursor: state.cursor })
  useEffect(() => {
    latest.current = { search, queryKey, cursor: state.cursor }
  })

  // Turning live off or changing the filters forgets arrivals, the cursor,
  // and the skipped notice. This happens during render, as LogsPage does for
  // its selection, so the restart lands before the effect that seeds.
  const trigger = `${enabled}\u0000${JSON.stringify(logsApiParams(search))}`
  const [seenTrigger, setSeenTrigger] = useState(trigger)
  if (trigger !== seenTrigger) {
    setSeenTrigger(trigger)
    setError(undefined)
    dispatch({ type: 'restart' })
  }

  // Refresh, a filter change, and a reset hand over a new first page object;
  // "Load older" keeps pages[0] as it was and does not.
  useEffect(() => {
    if (enabled && firstPage) dispatch({ type: 'seed', page: firstPage })
  }, [enabled, firstPage])

  useEffect(() => {
    if (state.resetting) void queryClient.resetQueries({ queryKey: latest.current.queryKey })
  }, [state.resetting, queryClient])

  const seeded = state.cursor !== undefined
  const { generation } = state
  // A new generation, or a first cursor after a reset, starts a new chain of
  // polls; the cursor itself moves with every poll and is read from `latest`.
  useEffect(() => {
    if (!enabled || !seeded) return
    let cancelled = false
    let timer: ReturnType<typeof setTimeout> | undefined
    let paused = false

    const poll = async () => {
      const { search: current, cursor } = latest.current
      if (cursor === undefined) return
      const params = tailParams(current, cursor)
      try {
        // fetchQuery, so a 401 reaches the QueryCache's onError and signs out
        // like any other query; gcTime 0 keeps no copy of the page.
        const page = await queryClient.fetchQuery({
          queryKey: ['logs-tail', params],
          queryFn: () => searchLogs(params),
          gcTime: 0,
          staleTime: 0,
          retry: false,
        })
        if (cancelled) return
        setError(undefined)
        dispatch({ type: 'poll', generation, page, now: new Date().toISOString() })
      } catch (e) {
        if (cancelled) return
        setError(e instanceof Error ? e.message : String(e))
      }
    }

    const tick = async () => {
      timer = undefined
      if (document.visibilityState === 'hidden') {
        paused = true
        return
      }
      await poll()
      if (!cancelled) timer = setTimeout(() => void tick(), pollMs)
    }
    const onVisibility = () => {
      if (!paused || document.visibilityState !== 'visible') return
      paused = false
      void tick()
    }

    document.addEventListener('visibilitychange', onVisibility)
    timer = setTimeout(() => void tick(), pollMs)
    return () => {
      cancelled = true
      clearTimeout(timer)
      document.removeEventListener('visibilitychange', onVisibility)
    }
  }, [enabled, seeded, generation, queryClient])

  const dismiss = useCallback(() => dispatch({ type: 'dismiss' }), [])
  return { arrivals: state.arrivals, from: state.from, skippedAt: state.skippedAt, dismiss, error }
}
