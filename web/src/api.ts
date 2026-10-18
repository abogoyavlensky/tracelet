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
  /** When the server stored the record; with ts it identifies the record. */
  ingest_ts: string
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

export type LogsPage = Page<Log> & {
  /** Pass as `since` to fetch only newer logs. */
  tail_cursor?: string
}

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

export type ServiceCount = { service: string; logs: number; spans: number; points: number }

export type Services = { items: ServiceCount[]; range: Range }

export type MetricType = 'gauge' | 'sum' | 'histogram'

export type MetricKind = { type: MetricType; monotonic: boolean; unit?: string }

export type MetricInfo = MetricKind & {
  name: string
  services: string[]
  series: number
  points: number
  last_ts: string
}

export type MetricsList = { items: MetricInfo[]; range: Range; truncated: boolean }

export type AttributeInfo = { key: string; distinct: number; values: string[] }

export type MetricAttributes = { name: string; items: AttributeInfo[]; range: Range }

export type MetricSeries = {
  group: Record<string, string>
  /** One value per timestamp; null where the step has no data. */
  values: (number | null)[]
  incompatible_series?: number
}

export type MetricResult = MetricKind & {
  name: string
  aggregate: string
  group_by?: string
  step_seconds: number
  range: Range
  timestamps: string[]
  series: MetricSeries[]
  truncated: boolean
}

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
  since?: string
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

export type MetricsParams = RangeParams & {
  project?: string
  service?: string
  environment?: string
  q?: string
}

export type MetricAttributesParams = RangeParams & {
  project?: string
  name: string
  service?: string
  environment?: string
}

export type MetricQueryParams = RangeParams & {
  project?: string
  name: string
  aggregate?: string
  group_by?: string
  service?: string
  environment?: string
  attr?: string[]
  step?: string
}

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

export function listMetrics(params: MetricsParams): Promise<MetricsList> {
  return fetchJSON<MetricsList>(`/api/v1/metrics${toQuery(params)}`)
}

export function getMetricAttributes(params: MetricAttributesParams): Promise<MetricAttributes> {
  return fetchJSON<MetricAttributes>(`/api/v1/metrics/attributes${toQuery(params)}`)
}

export function queryMetric(params: MetricQueryParams): Promise<MetricResult> {
  return fetchJSON<MetricResult>(`/api/v1/metrics/query${toQuery(params)}`)
}

export type PanelType = 'timeseries' | 'stat' | 'table'

export type Reduce = 'last' | 'sum'

export type Refresh = 'off' | '10s' | '30s' | '1m' | '5m' | '15m'

/** A panel's metric query: the metric query's parameters without project and range. */
export type PanelMetric = {
  name: string
  aggregate?: string
  group_by?: string
  service?: string
  environment?: string
  attr?: string[]
  step?: string
}

/** A panel as written; the server assigns id, width, and reduce when left out. */
export type DashboardPanel = {
  id?: string
  title?: string
  type: PanelType
  width?: number
  reduce?: Reduce
  metric: PanelMetric
}

/** A panel as stored, with every default filled in. */
export type StoredPanel = DashboardPanel & { id: string; width: number }

export type DashboardDefinition = {
  title: string
  schema_version?: number
  last?: string
  refresh?: Refresh
  panels: DashboardPanel[]
}

export type Dashboard = {
  uid: string
  revision: number
  created_at: string
  updated_at: string
  title: string
  schema_version: number
  last: string
  refresh: Refresh
  panels: StoredPanel[]
}

/** A write: revision absent overwrites, 0 creates only, N updates only from N. */
export type DashboardWrite = DashboardDefinition & { revision?: number }

export type DashboardSummary = {
  uid: string
  title: string
  panels: number
  revision: number
  updated_at: string
}

export type ProjectParams = { project?: string }

export function listDashboards(params: ProjectParams): Promise<{ items: DashboardSummary[] }> {
  return fetchJSON<{ items: DashboardSummary[] }>(`/api/v1/dashboards${toQuery(params)}`)
}

export function getDashboard(uid: string, params: ProjectParams): Promise<Dashboard> {
  return fetchJSON<Dashboard>(`/api/v1/dashboards/${encodeURIComponent(uid)}${toQuery(params)}`)
}

export function putDashboard(
  uid: string,
  body: DashboardWrite,
  params: ProjectParams,
): Promise<Dashboard> {
  return fetchJSON<Dashboard>(`/api/v1/dashboards/${encodeURIComponent(uid)}${toQuery(params)}`, {
    method: 'PUT',
    body: JSON.stringify(body),
  })
}

export function deleteDashboard(uid: string, params: ProjectParams): Promise<void> {
  return fetchJSON<void>(`/api/v1/dashboards/${encodeURIComponent(uid)}${toQuery(params)}`, {
    method: 'DELETE',
  })
}
