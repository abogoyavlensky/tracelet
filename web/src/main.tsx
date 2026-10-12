import { QueryCache, QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider } from '@tanstack/react-router'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { ApiError } from './api'
import './index.css'
import { createAppRouter } from './router'
import { sessionKey } from './session'

const queryClient: QueryClient = new QueryClient({
  queryCache: new QueryCache({
    // A 401 from any query means the session ended (signed out elsewhere, or
    // the token was revoked). Drop the cached session first, or the route
    // guard would find it and let the user straight back in.
    onError: (error, query) => {
      if (!(error instanceof ApiError) || error.status !== 401) return
      queryClient.removeQueries({ queryKey: sessionKey })
      if (query.queryKey[0] === sessionKey[0]) return // the route guard redirects for this one
      if (router.state.location.pathname !== '/login') {
        void router.navigate({ to: '/login', search: { next: router.state.location.href } })
      }
    },
  }),
  defaultOptions: {
    queries: {
      retry: (failures, error) =>
        !(error instanceof ApiError && error.status < 500) && failures < 1,
      refetchOnWindowFocus: false,
    },
  },
})

const router = createAppRouter(queryClient)

const root = document.getElementById('root')
if (!root) {
  throw new Error('missing #root element')
}

createRoot(root).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </StrictMode>,
)
