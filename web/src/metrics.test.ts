import { describe, expect, it } from 'vitest'
import type { MetricResult } from './api'
import {
  aggregateOptions,
  groupByOptions,
  hasIsolatedValue,
  incompatibleSeries,
  kindLabel,
  lastValue,
  seriesLabel,
  toChartData,
  valueAt,
} from './metrics'

const result: MetricResult = {
  name: 'http.server.request.duration',
  type: 'histogram',
  monotonic: false,
  unit: 's',
  aggregate: 'p95',
  group_by: 'http.route',
  step_seconds: 60,
  range: { from: '2026-10-12T10:00:00Z', to: '2026-10-12T10:03:00Z' },
  timestamps: ['2026-10-12T10:00:00Z', '2026-10-12T10:01:00Z', '2026-10-12T10:02:00Z'],
  series: [
    { group: { 'http.route': '/cart' }, values: [0.1, null, 0.3], incompatible_series: 2 },
    { group: { 'http.route': '' }, values: [1, 2, null], incompatible_series: 1 },
  ],
  truncated: false,
}

describe('kinds and aggregations', () => {
  it('labels kinds as instruments', () => {
    expect(kindLabel({ type: 'sum', monotonic: true })).toBe('counter')
    expect(kindLabel({ type: 'sum', monotonic: false })).toBe('updown')
    expect(kindLabel({ type: 'gauge', monotonic: false })).toBe('gauge')
    expect(kindLabel({ type: 'histogram', monotonic: false })).toBe('histogram')
  })

  it('offers the aggregations the server accepts, default first', () => {
    expect(aggregateOptions({ type: 'sum', monotonic: true })).toEqual({
      options: ['rate', 'increase'],
      default: 'rate',
    })
    expect(aggregateOptions({ type: 'sum', monotonic: false }).default).toBe('avg')
    expect(aggregateOptions({ type: 'gauge', monotonic: false }).options).toEqual([
      'avg',
      'min',
      'max',
      'sum',
    ])
    const hist = aggregateOptions({ type: 'histogram', monotonic: false })
    expect(hist.default).toBe('p95')
    expect(hist.options).toContain('rate')
  })

  it('lists group-by choices without repeating the columns', () => {
    expect(groupByOptions(['http.route', 'service'])).toEqual([
      '',
      'service',
      'environment',
      'version',
      'http.route',
    ])
  })
})

describe('chart data', () => {
  it('turns timestamps into epoch seconds and keeps nulls', () => {
    const data = toChartData(result)
    expect(data[0]).toEqual([1_791_799_200, 1_791_799_260, 1_791_799_320])
    expect(data[1]).toEqual([0.1, null, 0.3])
    expect(data[2]).toEqual([1, 2, null])
  })

  it('reads values at an index and the last value of a series', () => {
    expect(valueAt(result, 1)).toEqual([null, 2])
    expect(valueAt(result, 9)).toEqual([null, null])
    expect(lastValue(result.series[0])).toBe(0.3)
    expect(lastValue(result.series[1])).toBe(2)
    expect(lastValue({ group: {}, values: [null, null] })).toBeNull()
  })

  it('labels series by group, with (none) for a missing attribute', () => {
    expect(seriesLabel({ 'http.route': '/cart' }, 'm')).toBe('/cart')
    expect(seriesLabel({ 'http.route': '' }, 'm')).toBe('(none)')
    expect(seriesLabel({}, 'orders.created')).toBe('orders.created')
  })

  it('sums incompatible histogram series', () => {
    expect(incompatibleSeries(result)).toBe(3)
  })
})

describe('isolated values', () => {
  it('finds a value with gaps on both sides', () => {
    expect(hasIsolatedValue([null, 3, null])).toBe(true)
    expect(hasIsolatedValue([3])).toBe(true)
    expect(hasIsolatedValue([1, 2, null, 4, 5])).toBe(false)
    expect(hasIsolatedValue([null, null])).toBe(false)
  })
})
