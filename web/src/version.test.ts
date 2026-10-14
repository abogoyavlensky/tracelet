import { describe, expect, it } from 'vitest'
import { versionWatch } from './version'

describe('versionWatch', () => {
  it('is not stale before a version is known', () => {
    const watch = versionWatch()
    expect(watch(undefined)).toBe(false)
  })

  it('remembers the first version and is not stale for it', () => {
    const watch = versionWatch()
    expect(watch('v1')).toBe(false)
    expect(watch('v1')).toBe(false)
  })

  it('is stale once a different version shows up', () => {
    const watch = versionWatch()
    watch('v1')
    expect(watch('v2')).toBe(true)
  })

  it('is not stale when the health request failed after a version', () => {
    const watch = versionWatch()
    watch('v1')
    expect(watch(undefined)).toBe(false)
  })

  it('keeps the first version as the reference after a change', () => {
    const watch = versionWatch()
    watch('v1')
    expect(watch('v2')).toBe(true)
    expect(watch('v2')).toBe(true)
    expect(watch('v1')).toBe(false)
  })
})
