import { describe, expect, it } from 'vitest'
import type { Session } from './api'
import { resolveProject } from './project'

const admin: Session = {
  scope: 'admin',
  token_name: 'laptop',
  projects: [
    { slug: 'quickmeet', name: 'Quickmeet' },
    { slug: 'tracelet', name: 'Tracelet' },
  ],
}

describe('resolveProject', () => {
  it('uses the URL project when the token can read it', () => {
    expect(resolveProject(admin, 'tracelet', 'quickmeet')).toEqual({ slug: 'tracelet' })
  })

  it('falls back to the remembered project, then the first one', () => {
    expect(resolveProject(admin, undefined, 'tracelet')).toEqual({ slug: 'tracelet' })
    expect(resolveProject(admin, undefined, undefined)).toEqual({ slug: 'quickmeet' })
    expect(resolveProject(admin, undefined, 'gone')).toEqual({ slug: 'quickmeet' })
  })

  it('reports a URL project the token cannot read', () => {
    expect(resolveProject(admin, 'secret', undefined)).toEqual({
      slug: 'quickmeet',
      rejected: 'secret',
    })
  })

  it('always uses a scoped token its own project', () => {
    const scoped: Session = {
      ...admin,
      scope: 'read',
      project: 'tracelet',
      projects: [admin.projects[1]],
    }
    expect(resolveProject(scoped, undefined, 'quickmeet')).toEqual({ slug: 'tracelet' })
    expect(resolveProject(scoped, 'quickmeet', undefined)).toEqual({
      slug: 'tracelet',
      rejected: 'quickmeet',
    })
  })

  it('has nothing to choose without projects', () => {
    expect(resolveProject({ ...admin, projects: [] }, 'x', undefined)).toEqual({
      slug: undefined,
      rejected: 'x',
    })
    expect(resolveProject({ ...admin, projects: [] }, undefined, undefined)).toEqual({
      slug: undefined,
    })
  })
})
