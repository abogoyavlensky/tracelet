// URL search params for each route: how they are read from and written to
// the URL, validated, and turned into API params. The URL format is plain
// query strings (repeated keys for lists), not TanStack's JSON default, so
// links stay readable: ?project=shop&attr=user.id:42&last=1h.
import type { LogsParams, SpansParams } from './api'
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

export function validateLogsSearch(raw: Raw): LogsSearch {
  return {
    project: str(raw.project),
    service: str(raw.service),
    environment: str(raw.environment),
    level: oneOf(raw.level, levels),
    q: str(raw.q),
    attr: attrs(raw.attr),
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

/** A post-sign-in destination, only when it is a path on this site. */
export function safeNext(next: unknown): string | undefined {
  if (typeof next !== 'string' || !next.startsWith('/')) return undefined
  if (next.startsWith('//') || next.startsWith('/\\')) return undefined
  return next
}
