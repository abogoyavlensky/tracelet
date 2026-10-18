import { describe, expect, it } from 'vitest'
import type { MetricResult } from './api'
import {
  allowedReduces,
  allowedTypes,
  dashboardWrite,
  effectiveMetric,
  metricFromSearch,
  movePanel,
  panelQueryParams,
  reducedValue,
  refreshInterval,
  refreshOptions,
  sameMetric,
  searchFromPanel,
  slugify,
  spanClass,
} from './dashboards'

function result(values: (number | null)[], to: string): MetricResult {
  return {
    name: 'orders.created',
    type: 'sum',
    monotonic: true,
    aggregate: 'rate',
    step_seconds: 60,
    range: { from: '2026-10-18T10:00:00Z', to },
    timestamps: values.map((_, i) =>
      new Date(Date.parse('2026-10-18T10:00:00Z') + i * 60_000).toISOString(),
    ),
    series: [{ group: {}, values }],
    truncated: false,
  }
}

describe('effective panel queries', () => {
  it('fills only the filters a panel leaves empty', () => {
    expect(
      effectiveMetric({ name: 'm', service: 'api' }, { service: 'web', environment: 'prod' }),
    ).toEqual({ name: 'm', service: 'api', environment: 'prod' })
    expect(effectiveMetric({ name: 'm' }, {})).toEqual({ name: 'm' })
  })

  it('builds query params with project and range', () => {
    expect(
      panelQueryParams(
        { type: 'timeseries', metric: { name: 'm', aggregate: 'rate', attr: ['plan:pro'] } },
        { environment: 'prod' },
        { last: '6h' },
        'shop',
      ),
    ).toEqual({
      project: 'shop',
      name: 'm',
      aggregate: 'rate',
      attr: ['plan:pro'],
      environment: 'prod',
      last: '6h',
    })
  })
})

describe('reducedValue', () => {
  it('takes the latest step that has ended, not the partial one', () => {
    // Steps start 10:00, 10:01, 10:02; the range ends 10:02:30, inside the last.
    expect(reducedValue(result([1, 2, 0.5], '2026-10-18T10:02:30Z'), 0, 'last')).toBe(2)
    expect(reducedValue(result([1, 2, 3], '2026-10-18T10:03:00Z'), 0, 'last')).toBe(3)
  })

  it('skips empty steps', () => {
    expect(reducedValue(result([1, null, null], '2026-10-18T10:03:00Z'), 0, 'last')).toBe(1)
  })

  it('falls back to the latest value when no ended step has one', () => {
    expect(reducedValue(result([null, null, 4], '2026-10-18T10:02:30Z'), 0, 'last')).toBe(4)
  })

  it('is null without values', () => {
    expect(reducedValue(result([null, null], '2026-10-18T10:02:00Z'), 0, 'last')).toBeNull()
    expect(reducedValue(result([null, null], '2026-10-18T10:02:00Z'), 0, 'sum')).toBeNull()
    expect(reducedValue(result([1], '2026-10-18T10:01:00Z'), 3, 'last')).toBeNull()
  })

  it('sums every step, ignoring empty ones', () => {
    expect(reducedValue(result([1, null, 2.5], '2026-10-18T10:02:30Z'), 0, 'sum')).toBe(3.5)
  })
})

describe('panel constraints', () => {
  it('allows a stat only when ungrouped', () => {
    expect(allowedTypes({ name: 'm' })).toEqual(['timeseries', 'stat', 'table'])
    expect(allowedTypes({ name: 'm', group_by: 'service' })).toEqual(['timeseries', 'table'])
  })

  it('allows sum only for an explicit increase', () => {
    expect(allowedReduces({ name: 'm', aggregate: 'increase' })).toEqual(['last', 'sum'])
    expect(allowedReduces({ name: 'm', aggregate: 'rate' })).toEqual(['last'])
    expect(allowedReduces({ name: 'm' })).toEqual(['last'])
  })
})

describe('layout', () => {
  it('maps every width to literal grid classes', () => {
    expect(spanClass(1)).toBe('md:col-span-1 xl:col-span-1')
    expect(spanClass(2)).toBe('md:col-span-2 xl:col-span-2')
    expect(spanClass(3)).toBe('md:col-span-2 xl:col-span-3')
    expect(spanClass(4)).toBe('md:col-span-2 xl:col-span-4')
    expect(spanClass(9)).toBe(spanClass(2))
  })

  it('moves a panel and clamps at the ends', () => {
    const panels = ['a', 'b', 'c']
    expect(movePanel(panels, 0, 1)).toEqual(['b', 'a', 'c'])
    expect(movePanel(panels, 2, -1)).toEqual(['a', 'c', 'b'])
    expect(movePanel(panels, 0, -1)).toEqual(['a', 'b', 'c'])
    expect(movePanel(panels, 2, 1)).toEqual(['a', 'b', 'c'])
    expect(panels).toEqual(['a', 'b', 'c'])
  })
})

describe('slugify', () => {
  it('makes a uid the server accepts', () => {
    expect(slugify('Shop overview')).toBe('shop-overview')
    expect(slugify('  API: p95 / errors!  ')).toBe('api-p95-errors')
    expect(slugify('Café déjà')).toBe('caf-d-j')
    expect(slugify('---')).toBe('dashboard')
    expect(slugify('')).toBe('dashboard')
    const long = slugify(`${'a'.repeat(63)} b`)
    expect(long).toBe('a'.repeat(63))
    expect(slugify('x'.repeat(100))).toHaveLength(64)
  })
})

describe('refresh', () => {
  it('has an interval for every option', () => {
    expect(refreshOptions.map(refreshInterval)).toEqual([
      false,
      10_000,
      30_000,
      60_000,
      300_000,
      900_000,
    ])
  })
})

describe('metric page and panel queries', () => {
  const search = {
    project: 'shop',
    last: '6h',
    aggregate: 'p95',
    group_by: 'http.route',
    attr: ['plan:pro'],
    step: '5m',
    dashboard: 'shop',
    panel: 'p1',
  }

  it('takes the query from the metric page without project, range, or dashboard keys', () => {
    expect(metricFromSearch('http.server.request.duration', search)).toEqual({
      name: 'http.server.request.duration',
      aggregate: 'p95',
      group_by: 'http.route',
      attr: ['plan:pro'],
      step: '5m',
    })
  })

  it('round trips a panel through the metric page search', () => {
    const metric = metricFromSearch('m', search)
    const back = searchFromPanel(metric, { last: '1h' }, { dashboard: 'shop', panel: 'p1' })
    expect(back).toEqual({
      aggregate: 'p95',
      group_by: 'http.route',
      attr: ['plan:pro'],
      step: '5m',
      last: '1h',
      dashboard: 'shop',
      panel: 'p1',
    })
    expect(metricFromSearch('m', back)).toEqual(metric)
  })

  it('compares queries with missing and empty fields alike', () => {
    expect(sameMetric({ name: 'm', attr: [] }, { name: 'm', service: '' })).toBe(true)
    expect(sameMetric({ name: 'm', attr: ['a:1'] }, { name: 'm', attr: ['a:1'] })).toBe(true)
    expect(sameMetric({ name: 'm', attr: ['a:1'] }, { name: 'm', attr: ['a:2'] })).toBe(false)
    expect(sameMetric({ name: 'm', aggregate: 'rate' }, { name: 'm' })).toBe(false)
  })
})

describe('dashboardWrite', () => {
  it('keeps the definition and the revision, and drops identity and times', () => {
    const d = {
      uid: 'shop',
      revision: 3,
      created_at: '2026-10-18T08:00:00Z',
      updated_at: '2026-10-18T09:00:00Z',
      title: 'Shop',
      schema_version: 1,
      last: '6h',
      refresh: '30s' as const,
      panels: [{ id: 'p1', type: 'stat' as const, width: 1, metric: { name: 'm' } }],
    }
    expect(dashboardWrite(d)).toEqual({
      title: 'Shop',
      schema_version: 1,
      last: '6h',
      refresh: '30s',
      panels: d.panels,
      revision: 3,
    })
    const extra = { type: 'timeseries' as const, metric: { name: 'n' } }
    expect(dashboardWrite(d, [...d.panels, extra]).panels).toHaveLength(2)
  })
})
