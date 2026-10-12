// Time ranges as the URL and the API carry them: a relative `last` (a Go
// duration ending now) or an absolute from/to, or neither for the API's
// default. Links between views always use absolute ranges.
import { epochMs, formatShort, isRFC3339, nsSince } from './format'

export type TimeRange = { last: string } | { from: string; to: string } | Record<string, never>

export const presets = [
  { label: '15m', last: '15m' },
  { label: '1h', last: '1h' },
  { label: '6h', last: '6h' },
  { label: '24h', last: '24h' },
  // The API parses `last` with Go's time.ParseDuration, which has no days.
  { label: '7d', last: '168h' },
] as const

const goDurationPart = /(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/g
const unitNs: Record<string, number> = {
  ns: 1,
  us: 1e3,
  µs: 1e3,
  ms: 1e6,
  s: 1e9,
  m: 6e10,
  h: 3.6e12,
}
// Go durations are int64 nanoseconds.
const durationLimitNs = 2 ** 63

/** A positive Go duration (time.ParseDuration syntax) that fits in int64 nanoseconds. */
export function isGoDuration(s: string): boolean {
  if (!/^(\d+(\.\d+)?(ns|us|µs|ms|s|m|h))+$/.test(s)) return false
  let ns = 0
  for (const [, n, unit] of s.matchAll(goDurationPart)) ns += Number(n) * unitNs[unit]
  return ns > 0 && ns < durationLimitNs
}

function validTime(v: unknown): v is string {
  return typeof v === 'string' && isRFC3339(v)
}

/** Keeps only a range the API accepts; `last` wins over from/to. */
export function normalizeRange(r: { last?: unknown; from?: unknown; to?: unknown }): TimeRange {
  if (typeof r.last === 'string' && isGoDuration(r.last)) {
    return { last: r.last }
  }
  if (validTime(r.from) && validTime(r.to) && nsSince(r.from, r.to) > 0) {
    return { from: r.from, to: r.to }
  }
  return {}
}

export function rangeParams(r: TimeRange): { last?: string; from?: string; to?: string } {
  if ('last' in r) return { last: r.last }
  if ('from' in r) return { from: r.from, to: r.to }
  return {}
}

const day = 24 * 60 * 60 * 1000

/** One day either side of ts: wide enough to hold a whole trace, narrow enough to bound the scan. */
export function rangeAround(ts: string): { from: string; to: string } {
  const ms = epochMs(ts)
  return { from: new Date(ms - day).toISOString(), to: new Date(ms + day).toISOString() }
}

/** The range for a trace's logs: from its start to a second past its end, since `to` is exclusive. */
export function traceLogsRange(trace: { start: string; end: string }): {
  from: string
  to: string
} {
  return { from: trace.start, to: new Date(epochMs(trace.end) + 1000).toISOString() }
}

function pad(n: number): string {
  return String(n).padStart(2, '0')
}

/** An instant as a datetime-local input value in the browser's zone. */
export function toLocalInput(ts: string): string {
  const d = new Date(epochMs(ts))
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

/** A datetime-local input value, read in the browser's zone, as an RFC 3339 instant. */
export function fromLocalInput(value: string): string | undefined {
  if (!value) return undefined
  const ms = new Date(value).getTime()
  return Number.isNaN(ms) ? undefined : new Date(ms).toISOString()
}

/** A short label for the range picker. */
export function rangeLabel(r: TimeRange, fallback: string): string {
  if ('last' in r) {
    const preset = presets.find((p) => p.last === r.last)
    return `Last ${preset?.label ?? r.last}`
  }
  if ('from' in r) return `${formatShort(r.from)} – ${formatShort(r.to)}`
  return fallback
}
