// Formatting for telemetry values. Timestamps arrive as RFC 3339 strings with
// up to nanosecond precision; Date keeps only milliseconds, so differences
// that matter at span scale go through nsSince.

/** Three significant digits, trailing zeros dropped. */
function sig(x: number): string {
  return String(Number.parseFloat(x.toPrecision(3)))
}

export function formatDuration(ns: number): string {
  if (ns <= 0) return '0 ms'
  if (ns < 1e3) return `${Math.round(ns)} ns`
  if (ns < 1e6) return `${sig(ns / 1e3)} µs`
  if (ns < 1e9) return `${sig(ns / 1e6)} ms`
  if (ns < 60e9) return `${sig(ns / 1e9)} s`
  const totalSeconds = Math.floor(ns / 1e9)
  if (totalSeconds < 3600) return `${Math.floor(totalSeconds / 60)}m ${totalSeconds % 60}s`
  return `${Math.floor(totalSeconds / 3600)}h ${Math.floor((totalSeconds % 3600) / 60)}m`
}

const rfc3339 = /^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(?:\.(\d+))?(Z|[+-]\d{2}:\d{2})$/i

/** An RFC 3339 timestamp with a zone, the only form the API accepts for from and to. */
export function isRFC3339(ts: string): boolean {
  return rfc3339.test(ts) && !Number.isNaN(Date.parse(ts.replace(/\.\d+/, '')))
}

/** Splits a timestamp into whole-second epoch milliseconds and the nanoseconds past that second. */
function parts(ts: string): [number, number] {
  const m = rfc3339.exec(ts)
  if (!m) {
    const ms = Date.parse(ts)
    return [ms - (ms % 1000), (ms % 1000) * 1e6]
  }
  const frac = (m[2] ?? '').slice(0, 9).padEnd(9, '0')
  return [Date.parse(m[1] + m[3]), Number(frac)]
}

/** Nanoseconds from a to b, exact for the sub-second part. */
export function nsSince(a: string, b: string): number {
  const [aSec, aNs] = parts(a)
  const [bSec, bNs] = parts(b)
  return (bSec - aSec) * 1e6 + (bNs - aNs)
}

/** Epoch milliseconds, accepting any number of fraction digits. */
export function epochMs(ts: string): number {
  const [sec, ns] = parts(ts)
  return sec + Math.floor(ns / 1e6)
}

function pad(n: number, width = 2): string {
  return String(n).padStart(width, '0')
}

/** Local wall-clock time with milliseconds: 14:03:07.120. */
export function formatClock(ts: string): string {
  const d = new Date(epochMs(ts))
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}.${pad(d.getMilliseconds(), 3)}`
}

/** Local date and time with milliseconds: 2026-10-12 14:03:07.120. */
export function formatTimestamp(ts: string): string {
  const d = new Date(epochMs(ts))
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${formatClock(ts)}`
}

/** Short local date and time for range labels: Oct 12, 14:03. */
export function formatShort(ts: string): string {
  return new Date(epochMs(ts)).toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

export const levels = ['trace', 'debug', 'info', 'warn', 'error', 'fatal'] as const
export type Level = (typeof levels)[number]
export type Severity = Level | 'unset'

/** OTLP severity numbers come in bands of four, from TRACE (1-4) to FATAL (21-24). */
export function severityName(n: number): Severity {
  if (n < 1 || n > 24) return 'unset'
  return levels[Math.floor((n - 1) / 4)]
}

const kinds = ['unspecified', 'internal', 'server', 'client', 'producer', 'consumer'] as const
export type SpanKind = (typeof kinds)[number]

export function spanKindName(kind: number): SpanKind {
  return kinds[kind] ?? 'unspecified'
}

const statuses = ['unset', 'ok', 'error'] as const
export type SpanStatus = (typeof statuses)[number]

export function spanStatusName(code: number): SpanStatus {
  return statuses[code] ?? 'unset'
}
