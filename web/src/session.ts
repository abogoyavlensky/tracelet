import { queryOptions } from '@tanstack/react-query'
import { getHealth, getSession } from './api'

export const sessionKey = ['session'] as const

/** The signed-in token and its projects; a 401 means signed out. */
export const sessionQuery = queryOptions({
  queryKey: sessionKey,
  queryFn: getSession,
  staleTime: 60_000,
  retry: false,
})

/** Server status and version, polled; the shell reads it for the health dot and a stale-frontend check. */
export const healthQuery = queryOptions({
  queryKey: ['health'],
  queryFn: getHealth,
  refetchInterval: 10_000,
  retry: false,
})
