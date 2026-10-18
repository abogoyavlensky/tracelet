// The project's dashboards; an admin can create one here.
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { useState } from 'react'
import { ApiError, listDashboards, putDashboard } from '../api'
import { icons } from '../components/icons'
import { Popover } from '../components/Popover'
import { EmptyState, ErrorState, Loading } from '../components/States'
import { useProject } from '../components/useProject'
import { slugify } from '../dashboards'
import { formatShort } from '../format'
import { sessionQuery } from '../session'

export function DashboardsPage() {
  const { slug: project } = useProject()
  const { data: session } = useQuery(sessionQuery)
  const admin = session?.scope === 'admin'
  const list = useQuery({
    queryKey: ['dashboards', project],
    queryFn: () => listDashboards({ project }),
    enabled: !!project,
  })
  const items = list.data?.items ?? []

  return (
    <>
      <div className="flex h-14 shrink-0 items-center gap-3 border-b border-line bg-surface px-3 md:h-13 md:px-5">
        <h1 className="text-[15px] font-medium">Dashboards</h1>
        {list.data && <span className="text-xs text-muted">{items.length} in this project</span>}
        {admin && (
          <div className="ml-auto">
            <NewDashboard project={project} />
          </div>
        )}
      </div>
      <section
        className="min-h-0 flex-1 overflow-y-auto bg-surface pb-tabbar md:pb-0"
        aria-label="Dashboards"
      >
        {list.isPending && project ? (
          <Loading label="Listing dashboards…" />
        ) : list.isError ? (
          <ErrorState error={list.error} onRetry={() => void list.refetch()} />
        ) : items.length === 0 ? (
          <EmptyState title="No dashboards yet">
            {admin ? 'Create one here, ' : 'An admin can create one here '}
            or with <span className="font-mono">tracelet dashboards apply</span>.
          </EmptyState>
        ) : (
          <ol>
            {items.map((d) => (
              <li key={d.uid}>
                <Link
                  to="/dashboards/$uid"
                  params={{ uid: d.uid }}
                  search={{ project }}
                  className="grid min-w-0 grid-cols-[1fr_auto] items-center gap-x-3 gap-y-1 border-b border-line/60 px-3 py-2 hover:bg-raised/60 md:h-11 md:grid-cols-[minmax(0,1fr)_minmax(0,30%)_96px_120px] md:px-5 md:py-0"
                >
                  <span className="truncate text-[13.5px] font-medium">{d.title}</span>
                  <span className="hidden truncate font-mono text-[12px] text-subtle md:inline">
                    {d.uid}
                  </span>
                  <span className="text-right text-[12px] text-muted tabular-nums md:text-left">
                    {d.panels} {d.panels === 1 ? 'panel' : 'panels'}
                  </span>
                  <span className="hidden text-right text-[12px] text-subtle tabular-nums md:inline">
                    {formatShort(d.updated_at)}
                  </span>
                </Link>
              </li>
            ))}
          </ol>
        )}
      </section>
    </>
  )
}

function NewDashboard({ project }: { project: string | undefined }) {
  return (
    <Popover
      align="end"
      className="flex h-9 items-center gap-1.5 rounded-md bg-accent px-3 text-sm font-medium text-accent-fg md:h-8"
      label={
        <>
          <icons.plus size={15} />
          New dashboard
        </>
      }
      panelClassName="w-80"
    >
      {(close) => <NewDashboardForm project={project} onDone={close} />}
    </Popover>
  )
}

function NewDashboardForm({
  project,
  onDone,
}: {
  project: string | undefined
  onDone: () => void
}) {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [title, setTitle] = useState('')
  const [error, setError] = useState<string>()
  const [saving, setSaving] = useState(false)
  const uid = slugify(title)
  return (
    <form
      onSubmit={async (e) => {
        e.preventDefault()
        if (!title.trim()) return setError('A dashboard needs a title.')
        setSaving(true)
        try {
          const d = await putDashboard(
            uid,
            { title: title.trim(), panels: [], revision: 0 },
            { project },
          )
          queryClient.setQueryData(['dashboard', project, d.uid], d)
          void queryClient.invalidateQueries({ queryKey: ['dashboards', project] })
          onDone()
          await navigate({ to: '/dashboards/$uid', params: { uid: d.uid }, search: { project } })
        } catch (err) {
          setError(
            err instanceof ApiError && err.status === 409
              ? `A dashboard with the address ${uid} exists; choose another title.`
              : err instanceof Error
                ? err.message
                : String(err),
          )
        } finally {
          setSaving(false)
        }
      }}
    >
      <label className="block text-xs text-muted">
        Title
        <input
          autoFocus
          value={title}
          maxLength={200}
          onChange={(e) => (setTitle(e.target.value), setError(undefined))}
          placeholder="Shop overview"
          className="mt-1 block h-9 w-full rounded-md border border-line bg-surface px-2 text-sm text-fg placeholder:text-subtle"
        />
      </label>
      {title.trim() && !error && (
        <p className="mt-1.5 text-xs text-muted">
          Address: <span className="font-mono">{uid}</span>
        </p>
      )}
      {error && <p className="mt-1.5 text-xs text-error">{error}</p>}
      <button
        type="submit"
        disabled={saving}
        className="mt-2.5 h-9 w-full rounded-md bg-accent text-sm font-medium text-accent-fg disabled:opacity-60"
      >
        {saving ? 'Creating…' : 'Create'}
      </button>
    </form>
  )
}
