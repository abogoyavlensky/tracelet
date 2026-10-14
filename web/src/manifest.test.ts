import { existsSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

// A renamed or resized icon would silently break installing the app, so the
// manifest, the icon files and the links in index.html are checked together.
const webDir = resolve(import.meta.dirname, '..')
const publicDir = resolve(webDir, 'public')

type Icon = { src: string; sizes: string; type: string; purpose?: string }
const manifest = JSON.parse(readFileSync(resolve(publicDir, 'manifest.json'), 'utf8')) as {
  name?: string
  short_name?: string
  start_url?: string
  display?: string
  icons: Icon[]
}

/** Width and height from a PNG's IHDR chunk. */
function pngSize(path: string): string {
  const buf = readFileSync(path)
  return `${buf.readUInt32BE(16)}x${buf.readUInt32BE(20)}`
}

describe('manifest.json', () => {
  it('has the fields an installable app needs', () => {
    expect(manifest.name).toBeTruthy()
    expect(manifest.short_name).toBeTruthy()
    expect(manifest.start_url).toBeTruthy()
    expect(manifest.display).toBe('standalone')
  })

  it('lists any and maskable icons at 192 and 512', () => {
    const any = manifest.icons.filter((i) => !i.purpose || i.purpose === 'any')
    expect(any.map((i) => i.sizes)).toEqual(expect.arrayContaining(['192x192', '512x512']))
    expect(manifest.icons.some((i) => i.purpose === 'maskable')).toBe(true)
  })

  it.each(manifest.icons.map((i) => [i.src, i.sizes] as const))(
    'icon %s exists and is %s',
    (src, sizes) => {
      const path = resolve(publicDir, src.replace(/^\//, ''))
      expect(existsSync(path)).toBe(true)
      expect(pngSize(path)).toBe(sizes)
    },
  )
})

describe('index.html', () => {
  const html = readFileSync(resolve(webDir, 'index.html'), 'utf8')

  it('links the manifest and the apple touch icon', () => {
    expect(html).toContain('rel="manifest" href="/manifest.json"')
    expect(html).toContain('rel="apple-touch-icon" href="/apple-touch-icon.png"')
  })

  it('has a 180 px apple touch icon', () => {
    expect(pngSize(resolve(publicDir, 'apple-touch-icon.png'))).toBe('180x180')
  })
})
