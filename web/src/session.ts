import { queryOptions } from '@tanstack/react-query'
import { getSession } from './api'

export const sessionKey = ['session'] as const

/** The signed-in token and its projects; a 401 means signed out. */
export const sessionQuery = queryOptions({
  queryKey: sessionKey,
  queryFn: getSession,
  staleTime: 60_000,
  retry: false,
})
