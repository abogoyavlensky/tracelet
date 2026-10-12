import { describe, expect, it } from 'vitest'
import {
  logsApiParams,
  metricApiParams,
  metricsApiParams,
  parseSearchString,
  parseTraceId,
  safeNext,
  spansApiParams,
  stringifySearch,
  validateLogsSearch,
  validateMetricSearch,
  validateMetricsSearch,
  validateTraceSearch,
  validateTracesSearch,
} from './search'

describe('URL search strings', () => {
  it('parses repeated keys into arrays and keeps everything else a string', () => {
    expect(parseSearchString('?project=shop&attr=a:1&attr=b:2&root=false&q=42')).toEqual({
      project: 'shop',
      attr: ['a:1', 'b:2'],
      root: 'false',
      q: '42',
    })
    expect(parseSearchString('')).toEqual({})
  })

  it('writes arrays as repeated keys and drops empty values', () => {
    expect(
      stringifySearch({
        project: 'shop',
        attr: ['a:1', 'b:2'],
        root: false,
        q: '',
        service: undefined,
      }),
    ).toBe('?project=shop&attr=a%3A1&attr=b%3A2&root=false')
    expect(stringifySearch({})).toBe('')
  })
})

describe('validateLogsSearch', () => {
  it('keeps known keys and drops unknown ones', () => {
    expect(
      validateLogsSearch({
        project: 'shop',
        service: 'api',
        level: 'warn',
        q: 'boom',
        last: '1h',
        junk: 'x',
      }),
    ).toEqual({ project: 'shop', service: 'api', level: 'warn', q: 'boom', last: '1h' })
  })

  it('normalises attr into a string array', () => {
    expect(validateLogsSearch({ attr: 'a:1' }).attr).toEqual(['a:1'])
    expect(validateLogsSearch({ attr: ['a:1', 'b:2'] }).attr).toEqual(['a:1', 'b:2'])
    expect(validateLogsSearch({ attr: 'no-colon' }).attr).toBeUndefined()
  })

  it('owns its keys, so an invalid raw value is overridden, not merged through', () => {
    const v = validateLogsSearch({ last: '7d', level: 'loud' })
    expect(Object.hasOwn(v, 'last')).toBe(true)
    expect(v.last).toBeUndefined()
    expect(Object.hasOwn(v, 'level')).toBe(true)
    expect(Object.hasOwn(validateTraceSearch({ last: '1h' }), 'last')).toBe(true)
  })

  it('drops a level outside the enum and an invalid range', () => {
    expect(validateLogsSearch({ level: 'loud', last: '7d' })).toEqual({})
  })
})

describe('validateTracesSearch', () => {
  it('leaves root unset by default, which means root spans only', () => {
    expect(validateTracesSearch({}).root).toBeUndefined()
    expect(spansApiParams({}).root).toBe(true)
  })

  it('parses root=false', () => {
    expect(validateTracesSearch({ root: 'false' }).root).toBe(false)
    expect(spansApiParams({ root: false }).root).toBeUndefined()
  })

  it('keeps a valid status and min_duration only', () => {
    expect(validateTracesSearch({ status: 'error', min_duration: '500ms' })).toEqual({
      status: 'error',
      min_duration: '500ms',
    })
    expect(validateTracesSearch({ status: 'bad', min_duration: '5d' })).toEqual({})
  })
})

describe('validateTraceSearch', () => {
  it('keeps project, an absolute range, and the selected span', () => {
    expect(
      validateTraceSearch({
        project: 'shop',
        from: '2026-10-12T10:00:00Z',
        to: '2026-10-12T11:00:00Z',
        span: 'ABCDEF0123456789',
      }),
    ).toEqual({
      project: 'shop',
      from: '2026-10-12T10:00:00Z',
      to: '2026-10-12T11:00:00Z',
      span: 'abcdef0123456789',
    })
    expect(validateTraceSearch({ span: 'nope', last: '1h' })).toEqual({})
  })
})

describe('parseTraceId', () => {
  it('lower-cases a valid ID and rejects anything else', () => {
    expect(parseTraceId('4BF92F3577B34DA6A3CE929D0E0E4736')).toBe(
      '4bf92f3577b34da6a3ce929d0e0e4736',
    )
    expect(() => parseTraceId('xyz')).toThrow()
    expect(() => parseTraceId('4bf92f3577b34da6a3ce929d0e0e47')).toThrow()
  })
})

describe('API params', () => {
  it('builds the first page from filters and range', () => {
    expect(logsApiParams({ project: 'shop', level: 'error', attr: ['a:1'], last: '1h' })).toEqual({
      project: 'shop',
      level: 'error',
      attr: ['a:1'],
      last: '1h',
    })
  })

  it('sends the filters plus cursor, and no range, for later pages', () => {
    const params = logsApiParams(
      { project: 'shop', q: 'boom', from: '2026-10-12T10:00:00Z', to: '2026-10-12T11:00:00Z' },
      'c1',
    )
    expect(params).toEqual({ project: 'shop', q: 'boom', cursor: 'c1' })
    expect(spansApiParams({ service: 'api', last: '1h' }, 'c2')).toEqual({
      service: 'api',
      root: true,
      cursor: 'c2',
    })
  })
})

describe('safeNext', () => {
  it('accepts only same-site paths', () => {
    expect(safeNext('/traces?project=shop')).toBe('/traces?project=shop')
    expect(safeNext('//evil.example')).toBeUndefined()
    expect(safeNext('https://evil.example')).toBeUndefined()
    expect(safeNext('/\\evil.example')).toBeUndefined()
    expect(safeNext(undefined)).toBeUndefined()
  })
})

describe('metric searches', () => {
  it('validates the catalog search', () => {
    expect(validateMetricsSearch({ project: 'shop', q: 'orders', last: '6h', junk: 'x' })).toEqual({
      project: 'shop',
      service: undefined,
      environment: undefined,
      q: 'orders',
      last: '6h',
      from: undefined,
      to: undefined,
    })
    expect(metricsApiParams({ project: 'shop', q: 'orders', last: '6h' })).toEqual({
      project: 'shop',
      q: 'orders',
      last: '6h',
    })
  })

  it('drops an invalid step and aggregate, keeping every key it owns', () => {
    const s = validateMetricSearch({ step: 'often', aggregate: 'median', group_by: 'http.route' })
    expect(s).toHaveProperty('step', undefined)
    expect(s).toHaveProperty('aggregate', undefined)
    expect(s.group_by).toBe('http.route')
    expect(validateMetricSearch({ step: '5m', aggregate: 'p99.9' })).toMatchObject({
      step: '5m',
      aggregate: 'p99.9',
    })
  })

  it('builds query params with the name', () => {
    expect(
      metricApiParams('orders.created', {
        project: 'shop',
        aggregate: 'increase',
        group_by: 'service',
        attr: ['plan:pro'],
        from: '2026-10-12T10:00:00Z',
        to: '2026-10-12T11:00:00Z',
      }),
    ).toEqual({
      project: 'shop',
      name: 'orders.created',
      aggregate: 'increase',
      group_by: 'service',
      attr: ['plan:pro'],
      from: '2026-10-12T10:00:00Z',
      to: '2026-10-12T11:00:00Z',
    })
  })
})
