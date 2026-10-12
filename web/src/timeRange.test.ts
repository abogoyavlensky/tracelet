import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import {
  fromLocalInput,
  isGoDuration,
  normalizeRange,
  presets,
  rangeAround,
  rangeParams,
  toLocalInput,
  traceLogsRange,
} from './timeRange'

beforeAll(() => {
  vi.stubEnv('TZ', 'Europe/London')
})

afterAll(() => {
  vi.unstubAllEnvs()
})

describe('normalizeRange', () => {
  it('keeps a valid last', () => {
    expect(normalizeRange({ last: '1h' })).toEqual({ last: '1h' })
  })

  it('keeps a valid absolute range', () => {
    expect(normalizeRange({ from: '2026-10-12T10:00:00Z', to: '2026-10-12T11:00:00Z' })).toEqual({
      from: '2026-10-12T10:00:00Z',
      to: '2026-10-12T11:00:00Z',
    })
  })

  it('lets last win over from and to', () => {
    expect(
      normalizeRange({ last: '15m', from: '2026-10-12T10:00:00Z', to: '2026-10-12T11:00:00Z' }),
    ).toEqual({
      last: '15m',
    })
  })

  it('drops what the API would refuse', () => {
    expect(normalizeRange({ last: '7d' })).toEqual({})
    expect(normalizeRange({ from: '2026-10-12T10:00:00Z' })).toEqual({})
    expect(normalizeRange({ from: 'yesterday', to: '2026-10-12T11:00:00Z' })).toEqual({})
    expect(normalizeRange({ from: '2026-10-12T11:00:00Z', to: '2026-10-12T10:00:00Z' })).toEqual({})
    expect(normalizeRange({})).toEqual({})
  })
})

describe('rangeParams', () => {
  it('maps each shape to API params', () => {
    expect(rangeParams({ last: '1h' })).toEqual({ last: '1h' })
    expect(rangeParams({ from: 'a', to: 'b' })).toEqual({ from: 'a', to: 'b' })
    expect(rangeParams({})).toEqual({})
  })
})

describe('presets', () => {
  it('sends Go durations, so 7d is 168h', () => {
    expect(presets.find((p) => p.label === '7d')?.last).toBe('168h')
    for (const p of presets) {
      expect(p.last).toMatch(/^\d+(h|m)$/)
      expect(isGoDuration(p.last)).toBe(true)
    }
  })
})

describe('isGoDuration', () => {
  it.each([
    ['500ms', true],
    ['1.5s', true],
    ['1h30m', true],
    ['2us', true],
    ['2µs', true],
    ['7d', false],
    ['', false],
    ['ms', false],
    ['-1h', false],
  ])('%s is %s', (s, want) => {
    expect(isGoDuration(s)).toBe(want)
  })
})

describe('link ranges', () => {
  it('pads a timestamp by a day either side', () => {
    expect(rangeAround('2026-10-12T10:00:00.123456789Z')).toEqual({
      from: '2026-10-11T10:00:00.123Z',
      to: '2026-10-13T10:00:00.123Z',
    })
  })

  it('ends a trace logs range one second after the trace', () => {
    expect(
      traceLogsRange({ start: '2026-10-12T10:00:00.5Z', end: '2026-10-12T10:00:02.25Z' }),
    ).toEqual({
      from: '2026-10-12T10:00:00.5Z',
      to: '2026-10-12T10:00:03.250Z',
    })
  })
})

describe('datetime-local inputs', () => {
  it('round-trips in the browser zone', () => {
    // 10:30 UTC is 11:30 in London during BST.
    expect(toLocalInput('2026-10-12T10:30:00Z')).toBe('2026-10-12T11:30')
    expect(fromLocalInput('2026-10-12T11:30')).toBe('2026-10-12T10:30:00.000Z')
  })

  it('rejects an empty input', () => {
    expect(fromLocalInput('')).toBeUndefined()
  })
})
