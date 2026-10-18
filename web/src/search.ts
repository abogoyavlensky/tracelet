// URL search params for each route: how they are read from and written to
// the URL, validated, and turned into API params. The URL format is plain
// query strings (repeated keys for lists), not TanStack's JSON default, so
// links stay readable: ?project=shop&attr=user.id:42&last=1h.
import type { LogsParams, MetricQueryParams, MetricsParams, Refresh, SpansParams } from './api'
import { refreshOptions } from './dashboards'
import { type Level, levels } from './format'
import { isGoDuration, normalizeRange, rangeParams, type TimeRange } from './timeRange'

type Raw = Record<string, unknown>

export function parseSearchString(search: string): Raw {
  const out: Raw = {}
  for (const [key, value] of new URLSearchParams(search)) {
    const prev = out[key]
    if (prev === undefined) out[key] = value
    else out[key] = Array.isArray(prev) ? [...prev, value] : [prev, value]
  }
  return out
}

export function stringifySearch(search: Raw): string {
  const q = new URLSearchParams()
  for (const [key, value] of Object.entries(search)) {
    if (value === undefined || value === null || value === '') continue
    if (Array.isArray(value)) {
      for (const v of value) q.append(key, String(v))
    } else {
      q.append(key, String(value))
    }
  }
  const s = q.toString()
  return s ? `?${s}` : ''
}

function str(v: unknown): string | undefined {
  if (typeof v === 'number' || typeof v === 'boolean') return String(v)
  return typeof v === 'string' && v !== '' ? v : undefined
}

function oneOf<T extends string>(v: unknown, allowed: readonly T[]): T | undefined {
  const s = str(v)
  return allowed.find((a) => a === s)
}

/** A boolean flag in the URL: `live=1` or `live=true`; anything else is off. */
function flag(v: unknown): true | undefined {
  return v === true || v === '1' || v === 'true' || v === 1 ? true : undefined
}

/** Attribute filters are `key:value` with a non-empty key. */
function attrs(v: unknown): string[] | undefined {
  const list = (Array.isArray(v) ? v : [v])
    .map(str)
    .filter((s): s is string => !!s && /^[^:]+:/.test(s))
  return list.length > 0 ? list : undefined
}

/**
 * The router spreads the raw URL search under each validator's result, so a
 * validator returns every key it owns, undefined included: that is what
 * overrides an invalid raw value instead of letting it through.
 */
function rangeSearch(raw: Raw): RangeSearch {
  return { last: undefined, from: undefined, to: undefined, ...normalizeRange(raw) }
}

/** Drops undefined values so they never reach the API. */
function compact<T extends object>(o: T): T {
  return Object.fromEntries(Object.entries(o).filter(([, v]) => v !== undefined)) as T
}

export type RangeSearch = { last?: string; from?: string; to?: string }

export type LogsSearch = RangeSearch & {
  project?: string
  service?: string
  environment?: string
  level?: Level
  q?: string
  attr?: string[]
  /** Follow new logs; only takes effect on a relative range. */
  live?: true
}

export const spanStatuses = ['ok', 'error', 'unset'] as const
export type SpanStatusFilter = (typeof spanStatuses)[number]

export type TracesSearch = RangeSearch & {
  project?: string
  service?: string
  environment?: string
  name?: string
  status?: SpanStatusFilter
  min_duration?: string
  /** Unset means root spans only; false lists every span. */
  root?: boolean
  attr?: string[]
}

export type TraceSearch = {
  project?: string
  last?: undefined
  from?: string
  to?: string
  span?: string
}

export type MetricsSearch = RangeSearch & {
  project?: string
  service?: string
  environment?: string
  q?: string
}

export type MetricSearch = RangeSearch & {
  project?: string
  aggregate?: string
  group_by?: string
  service?: string
  environment?: string
  attr?: string[]
  step?: string
  /** Editing a dashboard panel's query: the dashboard's uid and the panel's id. */
  dashboard?: string
  panel?: string
}

export type DashboardsSearch = { project?: string }

export type DashboardSearch = RangeSearch & {
  project?: string
  service?: string
  environment?: string
  /** Overrides the dashboard's own auto-refresh for this view. */
  refresh?: Refresh
}

const dashboardUID = /^[a-z0-9][a-z0-9-]{0,63}$/
const panelID = /^[a-z0-9][a-z0-9-]{0,39}$/

const aggregatePattern = /^(rate|increase|avg|min|max|sum|p[0-9]{1,2}(\.[0-9])?)$/

export function validateLogsSearch(raw: Raw): LogsSearch {
  return {
    project: str(raw.project),
    service: str(raw.service),
    environment: str(raw.environment),
    level: oneOf(raw.level, levels),
    q: str(raw.q),
    attr: attrs(raw.attr),
    live: flag(raw.live),
    ...rangeSearch(raw),
  }
}

export function validateTracesSearch(raw: Raw): TracesSearch {
  const minDuration = str(raw.min_duration)
  return {
    project: str(raw.project),
    service: str(raw.service),
    environment: str(raw.environment),
    name: str(raw.name),
    status: oneOf(raw.status, spanStatuses),
    min_duration: minDuration && isGoDuration(minDuration) ? minDuration : undefined,
    root: str(raw.root) === 'false' ? false : undefined,
    attr: attrs(raw.attr),
    ...rangeSearch(raw),
  }
}

export function validateMetricsSearch(raw: Raw): MetricsSearch {
  return {
    project: str(raw.project),
    service: str(raw.service),
    environment: str(raw.environment),
    q: str(raw.q),
    ...rangeSearch(raw),
  }
}

export function validateMetricSearch(raw: Raw): MetricSearch {
  const aggregate = str(raw.aggregate)
  const step = str(raw.step)
  return {
    project: str(raw.project),
    aggregate: aggregate && aggregatePattern.test(aggregate) ? aggregate : undefined,
    group_by: str(raw.group_by),
    service: str(raw.service),
    environment: str(raw.environment),
    attr: attrs(raw.attr),
    step: step && isGoDuration(step) ? step : undefined,
    dashboard: matching(raw.dashboard, dashboardUID),
    panel: matching(raw.panel, panelID),
    ...rangeSearch(raw),
  }
}

export function validateDashboardsSearch(raw: Raw): DashboardsSearch {
  return { project: str(raw.project) }
}

/** A missing range stays missing, so the page can fall back to the dashboard's own. */
export function validateDashboardSearch(raw: Raw): DashboardSearch {
  return {
    project: str(raw.project),
    service: str(raw.service),
    environment: str(raw.environment),
    refresh: oneOf(raw.refresh, refreshOptions),
    ...rangeSearch(raw),
  }
}

function matching(v: unknown, pattern: RegExp): string | undefined {
  const s = str(v)
  return s && pattern.test(s) ? s : undefined
}

export function validateTraceSearch(raw: Raw): TraceSearch {
  const span = str(raw.span)
  // Links into a trace are always absolute, so a relative `last` is dropped.
  const range = normalizeRange({ from: raw.from, to: raw.to })
  return {
    project: str(raw.project),
    last: undefined,
    from: undefined,
    to: undefined,
    ...('from' in range ? range : {}),
    span: span && /^[0-9a-f]{16}$/i.test(span) ? span.toLowerCase() : undefined,
  }
}

/** A trace ID path param: 32 hex characters, lower-cased. */
export function parseTraceId(s: string): string {
  if (!/^[0-9a-f]{32}$/i.test(s)) {
    throw new Error(`"${s}" is not a trace ID: expected 32 hex characters`)
  }
  return s.toLowerCase()
}

/** The range a search sends, or nothing when a cursor carries it. */
function rangeOrCursor(
  s: RangeSearch,
  cursor: string | undefined,
): RangeSearch & { cursor?: string } {
  return cursor ? { cursor } : rangeParams(normalizeRange(s) as TimeRange)
}

export function logsApiParams(s: LogsSearch, cursor?: string): LogsParams {
  return compact({
    project: s.project,
    service: s.service,
    environment: s.environment,
    level: s.level,
    q: s.q,
    attr: s.attr,
    ...rangeOrCursor(s, cursor),
  })
}

export function spansApiParams(s: TracesSearch, cursor?: string): SpansParams {
  return compact({
    project: s.project,
    service: s.service,
    environment: s.environment,
    name: s.name,
    status: s.status,
    min_duration: s.min_duration,
    root: s.root === false ? undefined : true,
    attr: s.attr,
    ...rangeOrCursor(s, cursor),
  })
}

export function metricsApiParams(s: MetricsSearch): MetricsParams {
  return compact({
    project: s.project,
    service: s.service,
    environment: s.environment,
    q: s.q,
    ...rangeParams(normalizeRange(s) as TimeRange),
  })
}

export function metricApiParams(name: string, s: MetricSearch): MetricQueryParams {
  return compact({
    project: s.project,
    name,
    aggregate: s.aggregate,
    group_by: s.group_by,
    service: s.service,
    environment: s.environment,
    attr: s.attr,
    step: s.step,
    ...rangeParams(normalizeRange(s) as TimeRange),
  })
}

/** A post-sign-in destination, only when it is a path on this site. */
export function safeNext(next: unknown): string | undefined {
  if (typeof next !== 'string' || !next.startsWith('/')) return undefined
  if (next.startsWith('//') || next.startsWith('/\\')) return undefined
  return next
}
