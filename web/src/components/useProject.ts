import { useQuery } from '@tanstack/react-query'
import { useSearch } from '@tanstack/react-router'
import { useMemo } from 'react'
import { type ResolvedProject, rememberedProject, resolveProject } from '../project'
import { sessionQuery } from '../session'

/** The project the current page shows, resolved from the URL and the session. */
export function useProject(): ResolvedProject {
  const { data: session } = useQuery(sessionQuery)
  const { project } = useSearch({ strict: false }) as { project?: string }
  return useMemo(
    () => (session ? resolveProject(session, project, rememberedProject()) : { slug: undefined }),
    [session, project],
  )
}
