// Thin client for the Tracelet HTTP API. Every call goes through fetchJSON,
// which turns non-2xx responses into errors carrying the server's message.
// Types mirror api/openapi.yaml.

export class ApiError extends Error {
  readonly status: number
  readonly retryAfter: number | undefined

  constructor(status: number, message: string, retryAfter?: number) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.retryAfter = retryAfter
  }
}

export async function fetchJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    credentials: 'same-origin',
    ...init,
    headers: {
      Accept: 'application/json',
      ...(init?.body ? { 'Content-Type': 'application/json' } : {}),
      ...init?.headers,
    },
  })

  if (!response.ok) {
    const retry = Number(response.headers.get('Retry-After'))
    throw new ApiError(
      response.status,
      await errorMessage(response),
      Number.isFinite(retry) && retry > 0 ? retry : undefined,
    )
  }
  if (response.status === 204) {
    return undefined as T
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

/** Query parameters; undefined and empty values are left out, arrays repeat the key. */
export type QueryParams = Record<string, string | string[] | number | boolean | undefined>

export function toQuery(params: QueryParams): string {
  const q = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === '') continue
    if (Array.isArray(value)) {
      for (const v of value) q.append(key, v)
    } else {
      q.append(key, String(value))
    }
  }
  const s = q.toString()
  return s ? `?${s}` : ''
}

export type Attributes = Record<string, unknown>

export type Range = { from: string; to: string }

export type ProjectRef = { slug: string; name: string }

export type Session = {
  scope: 'admin' | 'read'
  token_name: string
  project?: string
  projects: ProjectRef[]
}

export type Page<T> = {
  items: T[]
  next_cursor?: string
  range: Range
  limit: number
  truncated: boolean
}

export type Log = {
  ts: string
  observed_ts?: string
  service: string
  environment?: string
  version?: string
  severity_number: number
  severity_text?: string
  body: string
  trace_id?: string
  span_id?: string
  scope?: string
  resource?: Attributes
  attributes?: Attributes
}

export type LogsPage = Page<Log>

export type SpanEvent = { ts?: string; name: string; attributes?: Attributes }

export type SpanLink = { trace_id: string; span_id: string; attributes?: Attributes }

export type Span = {
  ts: string
  end_ts: string
  duration_ns: number
  service: string
  environment?: string
  version?: string
  trace_id: string
  span_id: string
  parent_span_id?: string
  name: string
  kind: number
  status_code: number
  status_message?: string
  scope?: string
  resource?: Attributes
  attributes?: Attributes
  events?: SpanEvent[]
  links?: SpanLink[]
}

export type SpansPage = Page<Span>

export type Trace = {
  trace_id: string
  start: string
  end: string
  duration_ns: number
  services: string[]
  spans: Span[]
  range: Range
  truncated: boolean
}

export type ServiceCount = { service: string; logs: number; spans: number }

export type Services = { items: ServiceCount[]; range: Range }

export type Health = {
  status: 'ok' | 'degraded'
  version: string
  ready: boolean
  storage_error?: string
}

/** Range parameters every search takes; a cursor excludes all three. */
export type RangeParams = { last?: string; from?: string; to?: string }

export type LogsParams = RangeParams & {
  project?: string
  service?: string
  environment?: string
  level?: string
  q?: string
  trace_id?: string
  attr?: string[]
  limit?: number
  cursor?: string
}

export type SpansParams = RangeParams & {
  project?: string
  service?: string
  environment?: string
  name?: string
  status?: string
  min_duration?: string
  root?: boolean
  trace_id?: string
  attr?: string[]
  limit?: number
  cursor?: string
}

export type TraceParams = RangeParams & { project?: string }

export type ServicesParams = RangeParams & { project?: string }

export function getHealth(): Promise<Health> {
  return fetchJSON<Health>('/api/v1/health')
}

export function getSession(): Promise<Session> {
  return fetchJSON<Session>('/api/v1/session')
}

export function signIn(token: string): Promise<Session> {
  return fetchJSON<Session>('/api/v1/session', {
    method: 'POST',
    body: JSON.stringify({ token }),
  })
}

export function signOut(): Promise<void> {
  return fetchJSON<void>('/api/v1/session', { method: 'DELETE' })
}

export function searchLogs(params: LogsParams): Promise<LogsPage> {
  return fetchJSON<LogsPage>(`/api/v1/logs${toQuery(params)}`)
}

export function searchSpans(params: SpansParams): Promise<SpansPage> {
  return fetchJSON<SpansPage>(`/api/v1/spans${toQuery(params)}`)
}

export function getTrace(traceId: string, params: TraceParams): Promise<Trace> {
  return fetchJSON<Trace>(`/api/v1/traces/${encodeURIComponent(traceId)}${toQuery(params)}`)
}

export function listServices(params: ServicesParams): Promise<Services> {
  return fetchJSON<Services>(`/api/v1/services${toQuery(params)}`)
}
