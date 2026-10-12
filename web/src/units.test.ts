import { describe, expect, it } from 'vitest'
import { displayUnit, formatNumber, formatValue } from './units'

const plain = { unit: '', perSecond: false }

describe('formatValue', () => {
  it('scales seconds and milliseconds', () => {
    expect(formatValue(0.000_000_5, { unit: 's', perSecond: false })).toBe('500 ns')
    expect(formatValue(0.000_25, { unit: 's', perSecond: false })).toBe('250 µs')
    expect(formatValue(0.1234, { unit: 's', perSecond: false })).toBe('123 ms')
    expect(formatValue(2.5, { unit: 's', perSecond: false })).toBe('2.5 s')
    expect(formatValue(90, { unit: 's', perSecond: false })).toBe('1.5 min')
    expect(formatValue(7200, { unit: 's', perSecond: false })).toBe('2 h')
    expect(formatValue(1500, { unit: 'ms', perSecond: false })).toBe('1.5 s')
    expect(formatValue(0, { unit: 'ms', perSecond: false })).toBe('0 s')
  })

  it('uses binary prefixes for bytes', () => {
    expect(formatValue(512, { unit: 'By', perSecond: false })).toBe('512 B')
    expect(formatValue(1536, { unit: 'By', perSecond: false })).toBe('1.5 KiB')
    expect(formatValue(3 * 1024 ** 3, { unit: 'By', perSecond: false })).toBe('3 GiB')
  })

  it('prints counts with the braces word, percentages, and unknown units', () => {
    expect(formatValue(4, { unit: '{order}', perSecond: false })).toBe('4 order')
    expect(formatValue(12_345, { unit: '1', perSecond: false })).toBe('12.3k')
    expect(formatValue(42.123, { unit: '%', perSecond: false })).toBe('42.1%')
    expect(formatValue(3, { unit: 'Cel', perSecond: false })).toBe('3 Cel')
    expect(formatValue(0.000123, plain)).toBe('0.000123')
  })

  it('appends /s for rates and a dash for missing values', () => {
    expect(formatValue(4, { unit: '{order}', perSecond: true })).toBe('4 order/s')
    expect(formatValue(2048, { unit: 'By', perSecond: true })).toBe('2 KiB/s')
    expect(formatValue(null, plain)).toBe('–')
    expect(formatValue(undefined, plain)).toBe('–')
  })
})

describe('displayUnit', () => {
  it('tells a counter rate from a histogram rate with the same unit', () => {
    const counter = displayUnit({ type: 'sum', monotonic: true, unit: 's' }, 'rate')
    const histogram = displayUnit({ type: 'histogram', monotonic: false, unit: 's' }, 'rate')
    expect(formatValue(0.5, counter)).toBe('500 ms/s')
    expect(formatValue(0.5, histogram)).toBe('0.5/s')
  })

  it('keeps the unit for quantiles, averages, increases, and gauges', () => {
    const hist = { type: 'histogram' as const, monotonic: false, unit: 's' }
    expect(displayUnit(hist, 'p95')).toEqual({ unit: 's', perSecond: false })
    expect(displayUnit(hist, 'avg')).toEqual({ unit: 's', perSecond: false })
    expect(displayUnit({ type: 'sum', monotonic: true, unit: '{order}' }, 'increase')).toEqual({
      unit: '{order}',
      perSecond: false,
    })
    expect(displayUnit({ type: 'gauge', monotonic: false }, 'avg')).toEqual(plain)
  })
})

describe('formatNumber', () => {
  it('uses decimal suffixes past a thousand', () => {
    expect(formatNumber(999)).toBe('999')
    expect(formatNumber(1500)).toBe('1.5k')
    expect(formatNumber(2_500_000)).toBe('2.5M')
    expect(formatNumber(-4200)).toBe('-4.2k')
  })
})
