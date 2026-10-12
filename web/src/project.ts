// Which project the UI shows. The URL wins when the token can read that
// project; otherwise a project-scoped token's own project, the last one
// chosen in this browser, then the first one. A URL project the token
// cannot read is reported so the UI can say so instead of silently
// switching.
import type { Session } from './api'

export const lastProjectKey = 'tracelet.project'

export type ResolvedProject = { slug: string | undefined; rejected?: string }

export function resolveProject(
  session: Session,
  fromUrl: string | undefined,
  remembered: string | undefined,
): ResolvedProject {
  const readable = (slug: string | undefined) =>
    !!slug && session.projects.some((p) => p.slug === slug)
  const fallback =
    session.project ?? (readable(remembered) ? remembered : session.projects[0]?.slug)
  if (fromUrl && !readable(fromUrl)) return { slug: fallback, rejected: fromUrl }
  return { slug: fromUrl ?? fallback }
}

export function rememberedProject(): string | undefined {
  try {
    return localStorage.getItem(lastProjectKey) ?? undefined
  } catch {
    return undefined
  }
}

export function rememberProject(slug: string): void {
  try {
    localStorage.setItem(lastProjectKey, slug)
  } catch {
    // Storage may be unavailable; the URL still carries the project.
  }
}
