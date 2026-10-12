// Metric view logic: which aggregations a kind takes, how a query result
// becomes uPlot's data, and how series are labelled. Mirrors the server's
// rules in internal/query/metrics.go.
import type { MetricKind, MetricResult, MetricSeries } from './api'

export type KindLabel = 'counter' | 'updown' | 'gauge' | 'histogram'

export function kindLabel(kind: MetricKind): KindLabel {
  if (kind.type === 'sum') return kind.monotonic ? 'counter' : 'updown'
  return kind.type
}

const counterAggregates = ['rate', 'increase']
const gaugeAggregates = ['avg', 'min', 'max', 'sum']
const histogramAggregates = ['p50', 'p90', 'p95', 'p99', 'rate', 'avg']

/** The aggregations a kind takes and the server's default for it. */
export function aggregateOptions(kind: MetricKind): { options: string[]; default: string } {
  switch (kindLabel(kind)) {
    case 'counter':
      return { options: counterAggregates, default: 'rate' }
    case 'histogram':
      return { options: histogramAggregates, default: 'p95' }
    default:
      return { options: gaugeAggregates, default: 'avg' }
  }
}

/** Steps to choose from; '' lets the server pick one. */
export const stepOptions = [
  '',
  '10s',
  '15s',
  '30s',
  '1m',
  '2m',
  '5m',
  '10m',
  '15m',
  '30m',
  '1h',
  '2h',
  '3h',
  '6h',
  '12h',
  '24h',
] as const

/** Columns that group_by takes besides point attribute keys. */
export const groupColumns = ['service', 'environment', 'version'] as const

/** Group-by choices: none, the columns, then attribute keys not named like a column. */
export function groupByOptions(attributeKeys: string[]): string[] {
  const columns: readonly string[] = groupColumns
  return ['', ...columns, ...attributeKeys.filter((k) => !columns.includes(k))]
}

/** uPlot's aligned data: epoch seconds, then one array per series with nulls kept. */
export type ChartData = [number[], ...(number | null)[][]]

export function toChartData(result: MetricResult): ChartData {
  const xs = result.timestamps.map((ts) => Date.parse(ts) / 1000)
  return [xs, ...result.series.map((s) => s.values.map((v) => (v === null ? null : v)))]
}

/** A series' legend label: its group values, or the metric name when ungrouped. */
export function seriesLabel(group: Record<string, string>, name: string): string {
  const values = Object.values(group)
  if (values.length === 0) return name
  return values.map((v) => (v === '' ? '(none)' : v)).join(', ')
}

/** Each series' value at a timestamp index. */
export function valueAt(result: MetricResult, index: number): (number | null)[] {
  return result.series.map((s) => s.values[index] ?? null)
}

/** A series' last non-null value. */
export function lastValue(series: MetricSeries): number | null {
  for (let i = series.values.length - 1; i >= 0; i--) {
    const v = series.values[i]
    if (v !== null) return v
  }
  return null
}

/** Histogram series left out because their bucket bounds differ, summed over groups. */
export function incompatibleSeries(result: MetricResult): number {
  return result.series.reduce((n, s) => n + (s.incompatible_series ?? 0), 0)
}

/**
 * Whether a series has a value with no neighbour on either side. Lines are
 * not drawn across gaps, so such a value is invisible unless its point is.
 */
export function hasIsolatedValue(values: (number | null)[]): boolean {
  return values.some(
    (v, i) => v !== null && (values[i - 1] ?? null) === null && (values[i + 1] ?? null) === null,
  )
}

function pad(n: number): string {
  return String(n).padStart(2, '0')
}

/** An x-axis label: 24-hour local time, with the date when the chart spans days. */
export function axisTime(epochSeconds: number, spanSeconds: number): string {
  const d = new Date(epochSeconds * 1000)
  const hm = `${pad(d.getHours())}:${pad(d.getMinutes())}`
  if (spanSeconds <= 24 * 3600) return hm
  return `${d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })} ${hm}`
}
