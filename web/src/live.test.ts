import { describe, expect, it } from 'vitest'
import type { Log, LogsPage } from './api'
import {
  initialLive,
  type LiveState,
  liveEligible,
  liveReducer,
  logKey,
  maxArrivals,
  mergeLive,
  tailLimit,
  tailParams,
} from './live'
import { logsApiParams } from './search'

function log(ts: string, ingest = ts, body = 'b'): Log {
  return { ts, ingest_ts: ingest, service: 'api', severity_number: 9, body }
}

function page(items: Log[], over: Partial<LogsPage> = {}): LogsPage {
  return {
    items,
    range: { from: '2026-10-13T11:45:00Z', to: '2026-10-13T12:00:00Z' },
    limit: 100,
    truncated: false,
    tail_cursor: 'c1',
    ...over,
  }
}

const now = '2026-10-13T12:00:05Z'

function seeded(items: Log[] = []): LiveState {
  return liveReducer(initialLive, { type: 'seed', page: page(items) })
}

describe('logKey', () => {
  it('tells apart records equal in everything but the ingest stamp', () => {
    const a = log('2026-10-13T12:00:00Z', '2026-10-13T12:00:01Z')
    const b = log('2026-10-13T12:00:00Z', '2026-10-13T12:00:02Z')
    expect(logKey(a)).not.toBe(logKey(b))
  })
})

describe('liveReducer', () => {
  it('seeds with the first page: its cursor and window, no arrivals', () => {
    const dirty: LiveState = { ...initialLive, arrivals: [log('2026-10-13T12:00:00Z')] }
    const s = liveReducer(dirty, { type: 'seed', page: page([]) })
    expect(s.cursor).toBe('c1')
    expect(s.from).toBe('2026-10-13T11:45:00Z')
    expect(s.arrivals).toEqual([])
    expect(s.generation).toBe(dirty.generation + 1)
  })

  it('adds a poll to the arrivals, newest first, and moves the cursor and window', () => {
    const s0 = seeded()
    const newer = log('2026-10-13T12:00:03Z', '2026-10-13T12:00:04Z')
    const late = log('2026-10-13T12:00:01Z', '2026-10-13T12:00:05Z')
    const s1 = liveReducer(s0, {
      type: 'poll',
      generation: s0.generation,
      now,
      page: page([late, newer], {
        tail_cursor: 'c2',
        range: { from: '2026-10-13T11:46:00Z', to: '2026-10-13T12:01:00Z' },
      }),
    })
    expect(s1.arrivals).toEqual([newer, late])
    expect(s1.cursor).toBe('c2')
    expect(s1.from).toBe('2026-10-13T11:46:00Z')
    expect(s1.generation).toBe(s0.generation)
  })

  it('does not add a record it already holds', () => {
    const r = log('2026-10-13T12:00:03Z')
    const s0 = seeded()
    const poll = (s: LiveState) =>
      liveReducer(s, { type: 'poll', generation: s.generation, now, page: page([r]) })
    expect(poll(poll(s0)).arrivals).toEqual([r])
  })

  it('still advances the window and the cursor on an empty poll', () => {
    const s0 = seeded()
    const s1 = liveReducer(s0, {
      type: 'poll',
      generation: s0.generation,
      now,
      page: page([], {
        tail_cursor: 'c7',
        range: { from: '2026-10-13T11:50:00Z', to: '2026-10-13T12:05:00Z' },
      }),
    })
    expect(s1.cursor).toBe('c7')
    expect(s1.from).toBe('2026-10-13T11:50:00Z')
    expect(s1.arrivals).toEqual([])
  })

  it('ignores a response from an older generation', () => {
    const s0 = seeded()
    const s1 = liveReducer(s0, { type: 'seed', page: page([], { tail_cursor: 'c9' }) })
    const stale = liveReducer(s1, {
      type: 'poll',
      generation: s0.generation,
      now,
      page: page([log('2026-10-13T12:00:03Z')], { tail_cursor: 'old' }),
    })
    expect(stale).toBe(s1)
  })

  it('asks for a reset when a poll was truncated, and says what was skipped', () => {
    const s0 = seeded()
    const s1 = liveReducer(s0, {
      type: 'poll',
      generation: s0.generation,
      now,
      page: page([log('2026-10-13T12:00:03Z')], { truncated: true }),
    })
    expect(s1.resetting).toBe(true)
    expect(s1.skippedAt).toBe(now)
    expect(s1.arrivals).toEqual([])
    expect(s1.cursor).toBeUndefined()
    expect(s1.generation).toBeGreaterThan(s0.generation)
  })

  it('asks for a reset past the arrival cap, without a skipped notice', () => {
    const s0 = seeded()
    const many = Array.from({ length: maxArrivals + 1 }, (_, i) =>
      log(`2026-10-13T12:00:00.${String(i).padStart(6, '0')}Z`),
    )
    const s1 = liveReducer(s0, { type: 'poll', generation: s0.generation, now, page: page(many) })
    expect(s1.resetting).toBe(true)
    expect(s1.skippedAt).toBeUndefined()
    expect(s1.arrivals).toEqual([])
    expect(s1.cursor).toBeUndefined()
  })

  it('keeps the skipped notice through the re-seed that follows a reset', () => {
    const s0 = seeded()
    const reset = liveReducer(s0, {
      type: 'poll',
      generation: s0.generation,
      now,
      page: page([], { truncated: true }),
    })
    const s1 = liveReducer(reset, { type: 'seed', page: page([], { tail_cursor: 'fresh' }) })
    expect(s1.resetting).toBe(false)
    expect(s1.skippedAt).toBe(now)
    expect(s1.cursor).toBe('fresh')
  })

  it('clears the skipped notice on dismiss and forgets everything on restart', () => {
    const skipped: LiveState = {
      ...seeded(),
      skippedAt: now,
      arrivals: [log('2026-10-13T12:00:00Z')],
    }
    expect(liveReducer(skipped, { type: 'dismiss' }).skippedAt).toBeUndefined()
    const restarted = liveReducer(skipped, { type: 'restart' })
    expect(restarted).toEqual({ ...initialLive, generation: skipped.generation + 1 })
  })
})

describe('mergeLive', () => {
  const from = '2026-10-13T11:45:00Z'

  it('puts a late arrival inside the base list, not on top', () => {
    const base = [
      log('2026-10-13T12:00:09Z', 'i1', 'base new'),
      log('2026-10-13T11:59:00Z', 'i0', 'base old'),
    ]
    const late = log('2026-10-13T12:00:01Z', 'i5', 'late')
    const newest = log('2026-10-13T12:00:20Z', 'i6', 'newest')
    expect(mergeLive([newest, late], base, from).map((l) => l.body)).toEqual([
      'newest',
      'base new',
      'late',
      'base old',
    ])
  })

  it('drops records older than the window', () => {
    const old = log('2026-10-13T11:44:59Z')
    const edge = log('2026-10-13T11:45:00Z')
    expect(mergeLive([], [edge, old], from)).toEqual([edge])
    expect(mergeLive([], [edge, old], undefined)).toEqual([edge, old])
  })

  it('shows a record once, whether it is in the base, the arrivals, or both', () => {
    const r = log('2026-10-13T12:00:03Z', 'i1')
    const other = log('2026-10-13T12:00:04Z', 'i2')
    expect(mergeLive([other, r, r], [r], from)).toEqual([other, r])
  })

  it('keeps two records that differ only in their ingest stamp', () => {
    const a = log('2026-10-13T12:00:03Z', '2026-10-13T12:00:04.000001Z')
    const b = log('2026-10-13T12:00:03Z', '2026-10-13T12:00:04.000002Z')
    expect(mergeLive([a], [b], from)).toEqual([b, a])
  })

  it('orders timestamps that differ only in nanoseconds', () => {
    const a = log('2026-10-13T12:00:03.000000001Z', 'i1')
    const b = log('2026-10-13T12:00:03.000000002Z', 'i2')
    expect(mergeLive([a], [b], from)).toEqual([b, a])
    expect(mergeLive([b], [a], from)).toEqual([b, a])
  })

  it('compares times, not strings', () => {
    const short = log('2026-10-13T12:00:03.5Z', 'i1')
    const long = log('2026-10-13T12:00:03.25Z', 'i2')
    expect(mergeLive([long], [short], from)).toEqual([short, long])
  })
})

describe('liveEligible', () => {
  it('needs a range that ends now', () => {
    expect(liveEligible({ last: '15m' })).toBe(true)
    expect(liveEligible({})).toBe(true)
    expect(liveEligible({ from: '2026-10-13T11:00:00Z', to: '2026-10-13T12:00:00Z' })).toBe(false)
  })
})

describe('tailParams', () => {
  it('is the first page search plus since and the largest page', () => {
    const search = { project: 'shop', level: 'error', last: '15m', live: true } as const
    expect(tailParams(search, 'c1')).toEqual({
      ...logsApiParams(search),
      since: 'c1',
      limit: tailLimit,
    })
    expect(tailParams(search, 'c1')).not.toHaveProperty('live')
  })
})
