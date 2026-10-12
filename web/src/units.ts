// Value formatting by OpenTelemetry unit (UCUM-style: s, ms, By, 1, {thing},
// %). A display unit says what a charted number means after aggregation: a
// counter's rate is its unit per second, while a histogram's rate counts
// observations, whatever unit the observations had.
import type { MetricKind } from './api'

export type DisplayUnit = { unit: string; perSecond: boolean }

export function displayUnit(kind: MetricKind, aggregate: string): DisplayUnit {
  const unit = kind.unit ?? ''
  if (aggregate === 'rate') {
    return kind.type === 'histogram' ? { unit: '', perSecond: true } : { unit, perSecond: true }
  }
  return { unit, perSecond: false }
}

/** Three significant digits, trailing zeros dropped. */
function sig(x: number): string {
  return String(Number.parseFloat(x.toPrecision(3)))
}

const timeUnits: [number, string][] = [
  [3600, 'h'],
  [60, 'min'],
  [1, 's'],
  [1e-3, 'ms'],
  [1e-6, 'µs'],
  [1e-9, 'ns'],
]

const secondsPer: Record<string, number> = { h: 3600, min: 60, s: 1, ms: 1e-3, us: 1e-6, ns: 1e-9 }

function formatSeconds(seconds: number): string {
  if (seconds === 0) return '0 s'
  const abs = Math.abs(seconds)
  for (const [scale, name] of timeUnits) {
    if (abs >= scale) return `${sig(seconds / scale)} ${name}`
  }
  return `${sig(seconds / 1e-9)} ns`
}

const binary = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB']

function formatBytes(bytes: number): string {
  let v = bytes
  let i = 0
  while (Math.abs(v) >= 1024 && i < binary.length - 1) {
    v /= 1024
    i++
  }
  return `${sig(v)} ${binary[i]}`
}

const decimal: [number, string][] = [
  [1e12, 'T'],
  [1e9, 'G'],
  [1e6, 'M'],
  [1e3, 'k'],
]

/** A plain number: three significant digits, with k, M, G, T past a thousand. */
export function formatNumber(x: number): string {
  const abs = Math.abs(x)
  for (const [scale, suffix] of decimal) {
    if (abs >= scale) return `${sig(x / scale)}${suffix}`
  }
  return sig(x)
}

function formatUnit(value: number, unit: string): string {
  if (unit in secondsPer) return formatSeconds(value * secondsPer[unit])
  if (unit === 'By') return formatBytes(value)
  if (unit === '%') return `${sig(value)}%`
  if (unit === '' || unit === '1') return formatNumber(value)
  const word = /^\{(.+)\}$/.exec(unit)
  if (word) return `${formatNumber(value)} ${word[1]}`
  return `${formatNumber(value)} ${unit}`
}

export function formatValue(value: number | null | undefined, display: DisplayUnit): string {
  if (value === null || value === undefined || Number.isNaN(value)) return '–'
  const s = formatUnit(value, display.unit)
  return display.perSecond ? `${s}/s` : s
}

/** A y-axis tick formatter for a display unit. */
export function axisFormatter(display: DisplayUnit): (value: number) => string {
  return (value) => formatValue(value, display)
}
