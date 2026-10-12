import { describe, expect, it } from 'vitest'
import { formatDuration, nsSince, severityName, spanKindName, spanStatusName } from './format'

describe('formatDuration', () => {
  it.each([
    [0, '0 ms'],
    [850, '850 ns'],
    [1500, '1.5 µs'],
    [2_340_000, '2.34 ms'],
    [150_000_000, '150 ms'],
    [1_250_000_000, '1.25 s'],
    [61_000_000_000, '1m 1s'],
    [3_720_000_000_000, '1h 2m'],
  ])('%d ns is %s', (ns, want) => {
    expect(formatDuration(ns)).toBe(want)
  })
})

describe('severityName', () => {
  it.each([
    [0, 'unset'],
    [1, 'trace'],
    [4, 'trace'],
    [5, 'debug'],
    [9, 'info'],
    [12, 'info'],
    [13, 'warn'],
    [17, 'error'],
    [20, 'error'],
    [21, 'fatal'],
    [24, 'fatal'],
  ])('%d is %s', (n, want) => {
    expect(severityName(n)).toBe(want)
  })
})

describe('span names', () => {
  it('names kinds and statuses', () => {
    expect(spanKindName(2)).toBe('server')
    expect(spanKindName(3)).toBe('client')
    expect(spanKindName(5)).toBe('consumer')
    expect(spanKindName(9)).toBe('unspecified')
    expect(spanStatusName(0)).toBe('unset')
    expect(spanStatusName(1)).toBe('ok')
    expect(spanStatusName(2)).toBe('error')
  })
})

describe('nsSince', () => {
  it('keeps nanosecond precision from RFC 3339 strings', () => {
    expect(nsSince('2026-10-12T10:00:00.000000100Z', '2026-10-12T10:00:00.000000350Z')).toBe(250)
    expect(nsSince('2026-10-12T10:00:00Z', '2026-10-12T10:00:01.5Z')).toBe(1_500_000_000)
    expect(nsSince('2026-10-12T10:00:00.9Z', '2026-10-12T10:00:01.1Z')).toBe(200_000_000)
  })

  it('handles offsets other than Z', () => {
    expect(nsSince('2026-10-12T10:00:00Z', '2026-10-12T12:00:00+02:00')).toBe(0)
  })
})
