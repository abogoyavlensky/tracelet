// The signed-in frame: header, the page, and a bottom tab bar on phones.
// Navigation is one list of items, so the header can become an icon rail
// when more views arrive without touching the pages.
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, Outlet, useNavigate, useRouterState, useSearch } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import { type Session, getHealth, signOut } from '../api'
import { rememberProject } from '../project'
import { sessionQuery } from '../session'
import { useTheme } from '../theme'
import { normalizeRange, type TimeRange } from '../timeRange'
import { icons, Mark } from './icons'
import { Popover } from './Popover'
import { EmptyState, Notice } from './States'
import { TimeRangePicker } from './TimeRangePicker'
import { useProject } from './useProject'

const nav = [
  { to: '/logs', label: 'Logs', icon: icons.logs },
  { to: '/traces', label: 'Traces', icon: icons.trace },
  { to: '/metrics', label: 'Metrics', icon: icons.metrics },
] as const

export function AppShell() {
  const { data: session } = useQuery(sessionQuery)
  const project = useProject()
  const search = useSearch({ strict: false }) as { project?: string }
  const navigate = useNavigate()
  // Remember a refused URL project after the URL is corrected below.
  const [rejected, setRejected] = useState<string>()
  if (project.rejected && project.rejected !== rejected) setRejected(project.rejected)

  // Put the resolved project in the URL, so a copied link reopens this view.
  useEffect(() => {
    if (project.slug && search.project !== project.slug) {
      void navigate({
        to: '.',
        search: (prev: object) => ({ ...prev, project: project.slug }),
        replace: true,
      } as never)
    }
  }, [project.slug, search.project, navigate])

  if (!session) return null
  return (
    <div className="flex h-dvh flex-col bg-canvas font-sans text-fg">
      <Header session={session} project={project.slug} />
      {rejected && (
        <Notice>
          Project {rejected} is not visible to this token, so this shows {project.slug ?? 'nothing'}{' '}
          instead.
        </Notice>
      )}
      {session.projects.length === 0 ? <NoProjects /> : <Outlet />}
      <TabBar project={project.slug} />
    </div>
  )
}

function Header({ session, project }: { session: Session; project: string | undefined }) {
  const pathname = useRouterState({ select: (s) => s.location.pathname })
  // Pages whose time range the header holds on wide screens.
  const listPage = pathname === '/logs' || pathname === '/traces' || pathname.startsWith('/metrics')
  return (
    <header className="flex h-14 shrink-0 items-center gap-2 border-b border-line bg-surface px-3 md:gap-4 md:px-5">
      <Link
        to="/logs"
        search={{ project }}
        className="flex items-center gap-2"
        aria-label="Tracelet"
      >
        <Mark />
        <span className="hidden text-[15px] font-semibold sm:inline">Tracelet</span>
      </Link>
      <ProjectPicker session={session} project={project} />
      <nav className="hidden rounded-lg bg-raised p-0.5 md:flex">
        {nav.map((item) => (
          <Link
            key={item.to}
            to={item.to}
            search={{ project }}
            className={`rounded-md px-3.5 py-1 text-sm ${
              pathname.startsWith(item.to)
                ? 'bg-surface font-medium text-fg shadow-sm'
                : 'text-muted hover:text-fg'
            }`}
          >
            {item.label}
          </Link>
        ))}
      </nav>
      <div className="ml-auto flex items-center gap-1 md:gap-2">
        {listPage && <HeaderRange />}
        {listPage && <RefreshButton />}
        {listPage && <span className="mx-1 hidden h-6 w-px bg-line md:block" />}
        <HealthDot />
        <ThemeToggle />
        <AccountMenu session={session} />
      </div>
    </header>
  )
}

function ProjectPicker({ session, project }: { session: Session; project: string | undefined }) {
  const navigate = useNavigate()
  const pathname = useRouterState({ select: (s) => s.location.pathname })
  const current = session.projects.find((p) => p.slug === project)
  if (session.project || session.projects.length < 2) {
    return <span className="truncate px-1 text-sm font-medium">{current?.name ?? ''}</span>
  }
  return (
    <label className="relative flex h-9 min-w-0 items-center gap-1.5 rounded-md border border-line px-2.5 text-sm focus-within:ring-2 focus-within:ring-accent/40 hover:bg-raised">
      <span className="truncate">{current?.name ?? 'Choose a project'}</span>
      <icons.chevronDown size={14} className="shrink-0 text-muted" />
      <select
        aria-label="Project"
        className="absolute inset-0 cursor-pointer opacity-0"
        value={project ?? ''}
        onChange={(e) => {
          const slug = e.target.value
          rememberProject(slug)
          // Filters and traces belong to the old project, so start fresh.
          void navigate({
            to: pathname.startsWith('/traces')
              ? '/traces'
              : pathname.startsWith('/metrics')
                ? '/metrics'
                : '/logs',
            search: { project: slug },
          })
        }}
      >
        {session.projects.map((p) => (
          <option key={p.slug} value={p.slug}>
            {p.name}
          </option>
        ))}
      </select>
    </label>
  )
}

/** The time range of the list pages lives in the header on wide screens. */
function HeaderRange() {
  const search = useSearch({ strict: false }) as { last?: string; from?: string; to?: string }
  const navigate = useNavigate()
  return (
    <div className="hidden md:block">
      <TimeRangePicker
        value={normalizeRange(search)}
        onChange={(r) => void navigate(rangeNavigation(r))}
      />
    </div>
  )
}

/** A navigation that keeps the page's other search params and replaces the range. */
export function rangeNavigation(r: TimeRange) {
  return {
    to: '.',
    search: (prev: object) => ({ ...prev, last: undefined, from: undefined, to: undefined, ...r }),
  } as never
}

function RefreshButton() {
  const queryClient = useQueryClient()
  return (
    <button
      type="button"
      aria-label="Refresh"
      title="Run the search again"
      onClick={() =>
        void queryClient.invalidateQueries({
          predicate: (q) => q.queryKey[0] !== 'session' && q.queryKey[0] !== 'health',
        })
      }
      className="hidden size-9 place-items-center rounded-md text-muted hover:bg-raised hover:text-fg md:grid"
    >
      <icons.refresh size={16} />
    </button>
  )
}

function HealthDot() {
  const health = useQuery({
    queryKey: ['health'],
    queryFn: getHealth,
    refetchInterval: 10_000,
    retry: false,
  })
  const [tone, title] = health.isError
    ? ['bg-error ring-error/15', 'Server unreachable']
    : health.data?.status === 'degraded'
      ? [
          'bg-sev-warn ring-sev-warn/15',
          `Server degraded${health.data.storage_error ? `: ${health.data.storage_error}` : ''}`,
        ]
      : health.data
        ? ['bg-ok ring-ok/15', `Server healthy, version ${health.data.version}`]
        : ['bg-subtle ring-subtle/15', 'Checking the server']
  return (
    <span title={title} aria-label={title} role="img" className="grid size-9 place-items-center">
      <span className={`size-2.5 rounded-full ring-4 ${tone}`} />
    </span>
  )
}

function ThemeToggle() {
  const [theme, setTheme] = useTheme()
  const dark = theme === 'dark'
  return (
    <button
      type="button"
      aria-label={dark ? 'Use light theme' : 'Use dark theme'}
      title={dark ? 'Use light theme' : 'Use dark theme'}
      onClick={() => setTheme(dark ? 'light' : 'dark')}
      className="grid size-9 place-items-center rounded-md text-muted hover:bg-raised hover:text-fg"
    >
      {dark ? <icons.sun /> : <icons.moon />}
    </button>
  )
}

function AccountMenu({ session }: { session: Session }) {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const access =
    session.scope === 'admin'
      ? 'Admin access to every project'
      : session.project
        ? `Read access to ${session.project}`
        : 'Read access to every project'
  return (
    <Popover
      align="end"
      title={`Signed in as ${session.token_name}`}
      className="flex h-9 items-center gap-2 rounded-md px-1.5 text-sm text-muted hover:bg-raised hover:text-fg"
      label={
        <>
          <span className="grid size-6 place-items-center rounded-full bg-accent text-[11px] font-semibold text-accent-fg uppercase">
            {session.token_name.slice(0, 1) || '?'}
          </span>
          <span className="hidden max-w-32 truncate lg:inline">{session.token_name}</span>
        </>
      }
      panelClassName="w-64"
    >
      {() => (
        <div>
          <p className="text-sm">
            Signed in with the token <span className="font-medium">{session.token_name}</span>
          </p>
          <p className="mt-0.5 text-xs text-muted">{access}</p>
          <button
            type="button"
            onClick={async () => {
              await signOut()
              queryClient.clear()
              await navigate({ to: '/login' })
            }}
            className="mt-3 flex h-9 w-full items-center justify-center gap-2 rounded-md border border-line text-sm hover:bg-raised"
          >
            <icons.signOut size={15} />
            Sign out
          </button>
        </div>
      )}
    </Popover>
  )
}

function TabBar({ project }: { project: string | undefined }) {
  const pathname = useRouterState({ select: (s) => s.location.pathname })
  return (
    <nav className="fixed inset-x-0 bottom-0 z-30 grid grid-cols-3 border-t border-line bg-surface pb-[env(safe-area-inset-bottom)] md:hidden">
      {nav.map((item) => (
        <Link
          key={item.to}
          to={item.to}
          search={{ project }}
          className={`flex h-14 flex-col items-center justify-center gap-0.5 text-[11px] ${pathname.startsWith(item.to) ? 'text-accent' : 'text-muted'}`}
        >
          <item.icon size={18} />
          {item.label}
        </Link>
      ))}
    </nav>
  )
}

function NoProjects() {
  return (
    <EmptyState title="No projects yet">
      <p>
        Create one with the CLI, then point an OpenTelemetry SDK at this server with an ingest
        token:
      </p>
      <pre className="mt-3 overflow-x-auto rounded-md bg-raised p-3 text-left font-mono text-xs text-fg">
        {`tracelet projects create --name Shop shop
tracelet tokens create --project shop --scope ingest --name api`}
      </pre>
    </EmptyState>
  )
}
