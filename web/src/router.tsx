// The route tree, in code: no router plugin or codegen. Search params use
// plain query strings (search.ts) and are validated per route.
import type { QueryClient } from '@tanstack/react-query'
import {
  Outlet,
  createRootRouteWithContext,
  createRoute,
  createRouter,
  lazyRouteComponent,
  redirect,
} from '@tanstack/react-router'
import { ApiError } from './api'
import { Placeholder } from './pages/Placeholder'
import {
  parseSearchString,
  parseTraceId,
  safeNext,
  stringifySearch,
  validateLogsSearch,
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
  component: function LoginRoute() {
    return <Placeholder title="Sign in" data={loginRoute.useSearch()} />
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
  component: Outlet,
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
    return <Placeholder title="Logs" data={logsRoute.useSearch()} />
  },
})

export const tracesRoute = createRoute({
  getParentRoute: () => appRoute,
  path: 'traces',
  validateSearch: validateTracesSearch,
  component: function TracesRoute() {
    return <Placeholder title="Traces" data={tracesRoute.useSearch()} />
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
    return (
      <Placeholder title="Trace" data={{ ...traceRoute.useParams(), ...traceRoute.useSearch() }} />
    )
  },
})

// Design prototypes on fixtures, in dev builds only (deleted after the design is chosen).
const protoRoutes = import.meta.env.DEV
  ? [
      createRoute({
        getParentRoute: () => rootRoute,
        path: '_proto',
        component: lazyRouteComponent(() => import('./prototypes/ProtoRoutes'), 'ProtoIndex'),
      }),
      createRoute({
        getParentRoute: () => rootRoute,
        path: '_proto/$variant/$screen',
        component: lazyRouteComponent(() => import('./prototypes/ProtoRoutes'), 'ProtoScreen'),
      }),
    ]
  : []

const routeTree = rootRoute.addChildren([
  loginRoute,
  appRoute.addChildren([indexRoute, logsRoute, tracesRoute, traceRoute]),
  ...protoRoutes,
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
