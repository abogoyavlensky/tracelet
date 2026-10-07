// Thin client for the Tracelet HTTP API. Every call goes through fetchJSON,
// which turns non-2xx responses into errors carrying the server's message.

export class ApiError extends Error {
  readonly status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

export async function fetchJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    ...init,
    headers: { Accept: 'application/json', ...init?.headers },
  })

  if (!response.ok) {
    throw new ApiError(response.status, await errorMessage(response))
  }

  return (await response.json()) as T
}

async function errorMessage(response: Response): Promise<string> {
  try {
    const body = (await response.json()) as { error?: unknown }
    if (typeof body.error === 'string') {
      return body.error
    }
  } catch {
    // Not JSON; fall through to the status text.
  }
  return response.statusText || `request failed with status ${response.status}`
}

export type Health = {
  status: string
  version: string
}

export function getHealth(): Promise<Health> {
  return fetchJSON<Health>('/api/v1/health')
}
