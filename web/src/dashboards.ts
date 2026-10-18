// Dashboard logic: how a panel's query is built under the dashboard's range
// and filters, how a stat or table reduces steps to one value, which types
// and reductions a query allows, and how the metric page and a panel trade
// queries. Mirrors the server's rules in internal/dashboard.
import type {
  DashboardPanel,
  MetricQueryParams,
  MetricResult,
  PanelMetric,
  PanelType,
  Reduce,
  Refresh,
} from './api'
import type { MetricSearch } from './search'
import { rangeParams, type TimeRange } from './timeRange'

/** Auto-refresh choices; off leaves only the refresh button. */
export const refreshOptions: readonly Refresh[] = ['off', '10s', '30s', '1m', '5m', '15m']

export const defaultRefresh: Refresh = '30s'

const refreshMs: Record<Refresh, number | false> = {
  off: false,
  '10s': 10_000,
  '30s': 30_000,
  '1m': 60_000,
  '5m': 300_000,
  '15m': 900_000,
}

/** The refetch interval for a refresh choice, or false for none. */
export function refreshInterval(refresh: Refresh): number | false {
  return refreshMs[refresh]
}

/** Filters the dashboard applies to every panel. */
export type DashboardFilters = { service?: string; environment?: string }

/**
 * A panel's query under the dashboard's filters: a filter fills the panel's
 * own only when the panel leaves it empty, so a panel pinned to one service
 * keeps showing it.
 */
export function effectiveMetric(metric: PanelMetric, filters: DashboardFilters): PanelMetric {
  return compact({
    ...metric,
    service: metric.service || filters.service || undefined,
    environment: metric.environment || filters.environment || undefined,
  })
}

export function panelQueryParams(
  panel: DashboardPanel,
  filters: DashboardFilters,
  range: TimeRange,
  project: string | undefined,
): MetricQueryParams {
  return compact({ project, ...effectiveMetric(panel.metric, filters), ...rangeParams(range) })
}

/**
 * One value for a stat or table: `last` is the latest step that has ended
 * (the step holding the range's end reads low until it does), or the latest
 * value at all when no ended step has one; `sum` adds every step.
 */
export function reducedValue(result: MetricResult, index: number, reduce: Reduce): number | null {
  const values = result.series[index]?.values ?? []
  if (reduce === 'sum') {
    const present = values.filter((v): v is number => v !== null)
    return present.length ? present.reduce((a, b) => a + b, 0) : null
  }
  const end = Date.parse(result.range.to)
  const stepMs = result.step_seconds * 1000
  let latest: number | null = null
  for (let i = values.length - 1; i >= 0; i--) {
    const v = values[i]
    if (v === null) continue
    latest ??= v
    if (Date.parse(result.timestamps[i]) + stepMs <= end) return v
  }
  return latest
}

/** Panel types a query can take: a stat is one number, so never grouped. */
export function allowedTypes(metric: PanelMetric): PanelType[] {
  return metric.group_by ? ['timeseries', 'table'] : ['timeseries', 'stat', 'table']
}

/** Reductions a query can take: only increases add up to a total. */
export function allowedReduces(metric: PanelMetric): Reduce[] {
  return metric.aggregate === 'increase' ? ['last', 'sum'] : ['last']
}

// Literal class names, so Tailwind finds them in the source.
const spans: Record<number, string> = {
  1: 'md:col-span-1 xl:col-span-1',
  2: 'md:col-span-2 xl:col-span-2',
  3: 'md:col-span-2 xl:col-span-3',
  4: 'md:col-span-2 xl:col-span-4',
}

/** Grid classes for a width in quarters: two columns from md, four from xl. */
export function spanClass(width: number): string {
  return spans[width] ?? spans[2]
}

/** The panels with one moved by delta places, clamped at the ends. */
export function movePanel<T>(panels: readonly T[], index: number, delta: number): T[] {
  const to = Math.min(Math.max(index + delta, 0), panels.length - 1)
  const out = [...panels]
  if (index < 0 || index >= panels.length || to === index) return out
  const [moved] = out.splice(index, 1)
  out.splice(to, 0, moved)
  return out
}

/** A dashboard uid from its title: lower-case letters, digits, and hyphens. */
export function slugify(title: string): string {
  const slug = title
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+/, '')
    .slice(0, 64)
    .replace(/-+$/, '')
  return slug || 'dashboard'
}

/** The metric page's query as a panel's metric, without project, range, or dashboard keys. */
export function metricFromSearch(name: string, search: MetricSearch): PanelMetric {
  return compact({
    name,
    aggregate: search.aggregate,
    group_by: search.group_by,
    service: search.service,
    environment: search.environment,
    attr: search.attr?.length ? [...search.attr] : undefined,
    step: search.step,
  })
}

/** The metric page's search for a panel's metric, under a range, with extra keys. */
export function searchFromPanel(
  metric: PanelMetric,
  range: TimeRange,
  extra: Partial<MetricSearch> = {},
): MetricSearch {
  return compact({
    aggregate: metric.aggregate,
    group_by: metric.group_by,
    service: metric.service,
    environment: metric.environment,
    attr: metric.attr?.length ? [...metric.attr] : undefined,
    step: metric.step,
    ...rangeParams(range),
    ...extra,
  })
}

/** Whether two panel metrics are the same query; a missing field equals an empty one. */
export function sameMetric(a: PanelMetric, b: PanelMetric): boolean {
  const keys = ['name', 'aggregate', 'group_by', 'service', 'environment', 'step'] as const
  return (
    keys.every((k) => (a[k] ?? '') === (b[k] ?? '')) &&
    (a.attr ?? []).join('\u0000') === (b.attr ?? []).join('\u0000')
  )
}

function compact<T extends object>(o: T): T {
  return Object.fromEntries(Object.entries(o).filter(([, v]) => v !== undefined && v !== '')) as T
}
