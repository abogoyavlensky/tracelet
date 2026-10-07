import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError, fetchJSON } from './api'

function respond(status: number, body: string, statusText = ''): void {
  vi.stubGlobal(
    'fetch',
    vi.fn(async () => new Response(body, { status, statusText })),
  )
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('fetchJSON', () => {
  it('returns the decoded body on success', async () => {
    respond(200, '{"status":"ok"}')

    await expect(fetchJSON<{ status: string }>('/api/v1/health')).resolves.toEqual({
      status: 'ok',
    })
  })

  it('uses the server error message when the body is JSON', async () => {
    respond(404, '{"error":"not found"}')

    await expect(fetchJSON('/api/v1/nope')).rejects.toMatchObject({
      name: 'ApiError',
      status: 404,
      message: 'not found',
    })
  })

  it('falls back to the status text when the body is not JSON', async () => {
    respond(502, 'upstream down', 'Bad Gateway')

    const error = await fetchJSON('/api/v1/health').catch((e: unknown) => e)

    expect(error).toBeInstanceOf(ApiError)
    expect((error as ApiError).message).toBe('Bad Gateway')
  })
})
