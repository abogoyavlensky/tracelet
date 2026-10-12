import { describe, expect, it } from 'vitest'
import type { Span, Trace } from './api'
import { buildWaterfall, serviceColorIndex } from './waterfall'

const base = '2026-10-12T10:00:00Z'

function at(ms: number): string {
  return new Date(Date.parse(base) + ms).toISOString()
}

function span(
  id: string,
  parent: string | undefined,
  startMs: number,
  durMs: number,
  service = 'api',
): Span {
  return {
    ts: at(startMs),
    end_ts: at(startMs + durMs),
    duration_ns: durMs * 1e6,
    service,
    trace_id: 't'.repeat(32),
    span_id: id,
    parent_span_id: parent,
    name: id,
    kind: 1,
    status_code: 0,
  }
}

function trace(spans: Span[], durMs: number): Trace {
  return {
    trace_id: 't'.repeat(32),
    start: base,
    end: at(durMs),
    duration_ns: durMs * 1e6,
    services: [...new Set(spans.map((s) => s.service))].toSorted(),
    spans,
    range: { from: base, to: at(durMs) },
    truncated: false,
  }
}

describe('buildWaterfall', () => {
  it('nests the checkout trace and scales offsets and widths', () => {
    const rows = buildWaterfall(
      trace(
        [
          span('worker', 'client', 60, 100, 'worker'),
          span('root', undefined, 0, 200),
          span('client', 'root', 20, 150),
        ],
        200,
      ),
    )
    expect(rows.map((r) => [r.span.span_id, r.depth])).toEqual([
      ['root', 0],
      ['client', 1],
      ['worker', 2],
    ])
    expect(rows[0]).toMatchObject({ offsetPct: 0, widthPct: 100, orphan: false, hasChildren: true })
    expect(rows[1]).toMatchObject({ offsetPct: 10, widthPct: 75 })
    expect(rows[2]).toMatchObject({ offsetPct: 30, widthPct: 50, hasChildren: false })
  })

  it('makes a span whose parent is missing an orphan root', () => {
    const rows = buildWaterfall(
      trace([span('root', undefined, 0, 100), span('lost', 'gone', 50, 10)], 100),
    )
    expect(rows.map((r) => [r.span.span_id, r.depth, r.orphan])).toEqual([
      ['root', 0, false],
      ['lost', 0, true],
    ])
  })

  it('orders siblings by start, then span ID', () => {
    const rows = buildWaterfall(
      trace(
        [
          span('root', undefined, 0, 100),
          span('b', 'root', 10, 5),
          span('a', 'root', 10, 5),
          span('c', 'root', 5, 5),
        ],
        100,
      ),
    )
    expect(rows.map((r) => r.span.span_id)).toEqual(['root', 'c', 'a', 'b'])
  })

  it('visits each span of a cycle once', () => {
    const rows = buildWaterfall(
      trace([span('self', 'self', 0, 10), span('x', 'y', 0, 10), span('y', 'x', 5, 10)], 20),
    )
    expect(rows.map((r) => r.span.span_id).toSorted()).toEqual(['self', 'x', 'y'])
    expect(rows.every((r) => r.depth >= 0)).toBe(true)
  })

  it('gives every bar the minimum width in a zero-duration trace', () => {
    const rows = buildWaterfall(trace([span('root', undefined, 0, 0)], 0))
    expect(rows[0]).toMatchObject({ offsetPct: 0, widthPct: 0.5 })
  })

  it('keeps a minimum-width bar inside the track', () => {
    const rows = buildWaterfall(
      trace([span('root', undefined, 0, 100), span('end', 'root', 100, 0)], 100),
    )
    expect(rows[1].offsetPct + rows[1].widthPct).toBeLessThanOrEqual(100)
    expect(rows[1].widthPct).toBe(0.5)
  })

  it('colours a service the same way everywhere', () => {
    const rows = buildWaterfall(
      trace([span('root', undefined, 0, 10, 'api'), span('w', 'root', 0, 5, 'worker')], 10),
    )
    expect(rows[0].colorIndex).toBe(serviceColorIndex('api'))
    expect(rows[1].colorIndex).toBe(serviceColorIndex('worker'))
    expect(serviceColorIndex('api')).toBeGreaterThanOrEqual(0)
    expect(serviceColorIndex('api')).toBeLessThan(8)
  })
})
