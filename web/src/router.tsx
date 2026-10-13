// The route tree, in code: no router plugin or codegen. Search params use
// plain query strings (search.ts) and are validated per route.
import type { QueryClient } from '@tanstack/react-query'
import {
  Outlet,
  createRootRouteWithContext,
  createRoute,
  createRouter,
  redirect,
} from '@tanstack/react-router'
import { ApiError } from './api'
import { AppShell } from './components/AppShell'
import { LoginPage } from './pages/LoginPage'
import { LogsPage } from './pages/LogsPage'
import { MetricPage } from './pages/MetricPage'
import { MetricsPage } from './pages/MetricsPage'
import { TracePage } from './pages/TracePage'
import { TracesPage } from './pages/TracesPage'
import {
  parseSearchString,
  parseTraceId,
  safeNext,
  stringifySearch,
  validateLogsSearch,
  validateMetricSearch,
  validateMetricsSearch,
  validateTraceSearch,
  validateTracesSearch,
} from './search'
import { sessionQuery } from './session'

export type RouterContext = { queryClient: QueryClient }

const rootRoute = createRootRouteWithContext<RouterContext>()({
  component: Outlet,
})

export const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/login',
  validateSearch: (raw: Record<string, unknown>): { next?: string } => {
    const next = safeNext(raw.next)
    return next ? { next } : {}
  },
  // Already signed in: go where the sign-in would have led. Ask the server
  // rather than trusting the cache, which may hold a revoked session.
  beforeLoad: async ({ context, search }) => {
    const signedIn = await context.queryClient.fetchQuery({ ...sessionQuery, staleTime: 0 }).then(
      () => true,
      () => false,
    )
    if (signedIn) throw redirect({ href: search.next ?? '/logs' })
  },
  component: function LoginRoute() {
    return <LoginPage next={loginRoute.useSearch().next} />
  },
})

/** Every route under this one needs a session; without one it redirects to sign-in. */
export const appRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: 'app',
  beforeLoad: async ({ context, location }) => {
    try {
      const session = await context.queryClient.ensureQueryData(sessionQuery)
      return { session }
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) {
        throw redirect({ to: '/login', search: { next: location.href } })
      }
      throw error
    }
  },
  component: AppShell,
})

const indexRoute = createRoute({
  getParentRoute: () => appRoute,
  path: '/',
  beforeLoad: () => {
    throw redirect({ to: '/logs' })
  },
})

export const logsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: 'logs',
  validateSearch: validateLogsSearch,
  component: function LogsRoute() {
    return <LogsPage search={logsRoute.useSearch()} />
  },
})

export const tracesRoute = createRoute({
  getParentRoute: () => appRoute,
  path: 'traces',
  validateSearch: validateTracesSearch,
  component: function TracesRoute() {
    return <TracesPage search={tracesRoute.useSearch()} />
  },
})

export const traceRoute = createRoute({
  getParentRoute: () => appRoute,
  path: 'traces/$traceId',
  params: {
    parse: ({ traceId }) => ({ traceId: parseTraceId(traceId) }),
    stringify: ({ traceId }) => ({ traceId }),
  },
  validateSearch: validateTraceSearch,
  component: function TraceRoute() {
    return <TracePage traceId={traceRoute.useParams().traceId} search={traceRoute.useSearch()} />
  },
})

export const metricsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: 'metrics',
  validateSearch: validateMetricsSearch,
  component: function MetricsRoute() {
    return <MetricsPage search={metricsRoute.useSearch()} />
  },
})

export const metricRoute = createRoute({
  getParentRoute: () => appRoute,
  path: 'metrics/$name',
  params: {
    parse: ({ name }) => {
      if (!name) throw new Error('A metric name is required')
      return { name }
    },
    stringify: ({ name }) => ({ name }),
  },
  validateSearch: validateMetricSearch,
  component: function MetricRoute() {
    return <MetricPage name={metricRoute.useParams().name} search={metricRoute.useSearch()} />
  },
})

const routeTree = rootRoute.addChildren([
  loginRoute,
  appRoute.addChildren([indexRoute, logsRoute, tracesRoute, traceRoute, metricsRoute, metricRoute]),
])

export function createAppRouter(queryClient: QueryClient) {
  return createRouter({
    routeTree,
    context: { queryClient },
    parseSearch: parseSearchString,
    stringifySearch,
    defaultPreload: false,
  })
}

declare module '@tanstack/react-router' {
  interface Register {
    router: ReturnType<typeof createAppRouter>
  }
}
