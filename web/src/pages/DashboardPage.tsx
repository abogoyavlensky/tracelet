// One dashboard: its panels under a shared range, filters, and refresh. An
// admin edits a draft of it and saves the draft with the revision it was
// loaded at, so an edit made elsewhere in between is never overwritten.
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import {
  ApiError,
  type Dashboard,
  deleteDashboard,
  getDashboard,
  putDashboard,
  type Refresh,
  type StoredPanel,
} from '../api'
import { rangeNavigation } from '../components/AppShell'
import { DashboardGrid } from '../components/DashboardGrid'
import { DashboardPanel } from '../components/DashboardPanel'
import { FilterBar } from '../components/FilterBar'
import { icons } from '../components/icons'
import { Popover } from '../components/Popover'
import { SelectFilter } from '../components/SelectFilter'
import { EmptyState, ErrorState, Loading, Notice } from '../components/States'
import { TextFilter } from '../components/TextFilter'
import { TimeRangePicker } from '../components/TimeRangePicker'
import { useProject } from '../components/useProject'
import { dashboardWrite, movePanel, refreshOptions } from '../dashboards'
import type { DashboardSearch } from '../search'
import { sessionQuery } from '../session'
import { normalizeRange, presets, type TimeRange } from '../timeRange'

const refreshLabel = (r: Refresh) => (r === 'off' ? 'Off' : r)

export function DashboardPage({ uid, search }: { uid: string; search: DashboardSearch }) {
  const { slug: project } = useProject()
  const { data: session } = useQuery(sessionQuery)
  const admin = session?.scope === 'admin'
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const key = ['dashboard', project, uid]
  const dashboard = useQuery({
    queryKey: key,
    queryFn: () => getDashboard(uid, { project }),
    enabled: !!project,
    retry: (count, error) => !(error instanceof ApiError && error.status < 500) && count < 2,
  })
  const [draft, setDraft] = useState<Dashboard | null>(null)
  const [saveError, setSaveError] = useState<ApiError | Error>()
  const [saving, setSaving] = useState(false)

  // Without a range in the URL the dashboard's own applies; writing it there
  // keeps the header's picker and a copied link in step with what is shown.
  const urlRange = normalizeRange(search)
  const hasRange = 'last' in urlRange || 'from' in urlRange
  const defaultLast = dashboard.data?.last
  useEffect(() => {
    if (!hasRange && defaultLast) {
      void navigate({
        to: '.',
        search: (prev: object) => ({ ...prev, last: defaultLast }),
        replace: true,
      } as never)
    }
  }, [hasRange, defaultLast, navigate])

  if (dashboard.isPending && project) return <Loading label="Loading the dashboard…" />
  if (dashboard.isError) {
    if (dashboard.error instanceof ApiError && dashboard.error.status === 404) {
      return (
        <EmptyState title="No such dashboard">
          This project has no dashboard {uid}.{' '}
          <Link to="/dashboards" search={{ project }} className="text-accent">
            See all dashboards
          </Link>
          .
        </EmptyState>
      )
    }
    return <ErrorState error={dashboard.error} onRetry={() => void dashboard.refetch()} />
  }
  const stored = dashboard.data
  if (!stored) return null

  const shown = draft ?? stored
  const range: TimeRange = hasRange ? urlRange : { last: stored.last }
  const refresh = search.refresh ?? stored.refresh
  const filters = { service: search.service, environment: search.environment }
  const set = (patch: Partial<DashboardSearch>) =>
    void navigate({ to: '.', search: (prev: object) => ({ ...prev, ...patch }) } as never)
  const edit = (patch: Partial<Dashboard>) => setDraft({ ...shown, ...patch })
  const conflict = saveError instanceof ApiError && saveError.status === 409

  const save = async () => {
    if (!draft) return
    setSaving(true)
    try {
      const saved = await putDashboard(uid, dashboardWrite(draft), { project })
      queryClient.setQueryData(key, saved)
      void queryClient.invalidateQueries({ queryKey: ['dashboards', project] })
      setDraft(null)
      setSaveError(undefined)
    } catch (err) {
      setSaveError(err instanceof Error ? err : new Error(String(err)))
    } finally {
      setSaving(false)
    }
  }
  const cancel = () => (setDraft(null), setSaveError(undefined))
  const reload = async () => {
    cancel()
    await dashboard.refetch()
  }

  const actionButton = 'flex h-9 items-center gap-1.5 rounded-md px-3 text-sm md:h-8'
  const actions = !admin ? undefined : draft ? (
    <>
      <button
        type="button"
        onClick={cancel}
        className={`${actionButton} border border-line hover:bg-raised`}
      >
        Cancel
      </button>
      <button
        type="button"
        disabled={saving}
        onClick={() => void save()}
        className={`${actionButton} bg-accent font-medium text-accent-fg disabled:opacity-60`}
      >
        {saving ? 'Saving…' : 'Save'}
      </button>
    </>
  ) : (
    <button
      type="button"
      onClick={() => setDraft(stored)}
      className={`${actionButton} border border-line hover:bg-raised`}
    >
      Edit
    </button>
  )

  return (
    <>
      <FilterBar
        active={[search.service, search.environment, search.refresh].filter(Boolean).length}
        summary={`${shown.panels.length} ${shown.panels.length === 1 ? 'panel' : 'panels'}`}
        actions={actions}
        range={
          <TimeRangePicker
            compact
            value={range}
            onChange={(r) => void navigate(rangeNavigation(r))}
          />
        }
      >
        <TextFilter
          label="Service"
          value={search.service}
          placeholder="api"
          hint="Applies to panels that do not name a service themselves."
          onChange={(v) => set({ service: v })}
        />
        <TextFilter
          label="Environment"
          value={search.environment}
          placeholder="production"
          hint="Applies to panels that do not name an environment themselves."
          onChange={(v) => set({ environment: v })}
        />
        <SelectFilter
          label="Refresh"
          value={refresh}
          options={[
            { value: stored.refresh, label: refreshLabel(stored.refresh) },
            ...refreshOptions
              .filter((r) => r !== stored.refresh)
              .map((r) => ({ value: r, label: refreshLabel(r) })),
          ]}
          onChange={(v) => set({ refresh: v === stored.refresh ? undefined : (v as Refresh) })}
        />
      </FilterBar>
      {saveError && (
        <Notice>
          {conflict
            ? `${saveError.message}. Your changes are not saved.`
            : `Not saved: ${saveError.message}`}
          {conflict && (
            <button
              type="button"
              onClick={() => void reload()}
              className="ml-2 font-medium underline"
            >
              Reload
            </button>
          )}
        </Notice>
      )}
      <section
        className="min-h-0 flex-1 overflow-y-auto bg-canvas px-3 pt-3 pb-tabbar md:p-5"
        aria-label={`Dashboard ${shown.title}`}
      >
        <div className="mb-3 flex flex-wrap items-baseline gap-x-3 gap-y-1">
          <Link
            to="/dashboards"
            search={{ project }}
            className="flex items-center gap-1 text-[13px] text-muted hover:text-fg"
          >
            <icons.chevronLeft size={14} />
            Dashboards
          </Link>
          <h1 className="min-w-0 truncate text-[15px] font-medium">{shown.title}</h1>
        </div>
        {draft && <DashboardSettings draft={draft} onChange={edit} uid={uid} project={project} />}
        {shown.panels.length === 0 ? (
          <EmptyState title="No panels yet">
            {admin
              ? 'Open a metric and use Add to dashboard, or write the panels in a file and apply it with tracelet dashboards apply.'
              : 'An admin adds panels from the metric pages or with tracelet dashboards apply.'}
          </EmptyState>
        ) : (
          <DashboardGrid
            items={shown.panels.map((panel, i) => ({
              key: panel.id,
              width: panel.width,
              node: (
                <DashboardPanel
                  panel={panel}
                  dashboard={uid}
                  filters={filters}
                  range={range}
                  project={project}
                  refresh={refresh}
                  canEditQuery={admin && !draft}
                  editing={
                    draft
                      ? {
                          first: i === 0,
                          last: i === shown.panels.length - 1,
                          onChange: (p: StoredPanel) =>
                            edit({ panels: shown.panels.map((q, j) => (j === i ? p : q)) }),
                          onMove: (delta) => edit({ panels: movePanel(shown.panels, i, delta) }),
                          onRemove: () => edit({ panels: shown.panels.filter((_, j) => j !== i) }),
                        }
                      : undefined
                  }
                />
              ),
            }))}
          />
        )}
      </section>
    </>
  )
}

function DashboardSettings({
  draft,
  onChange,
  uid,
  project,
}: {
  draft: Dashboard
  onChange: (patch: Partial<Dashboard>) => void
  uid: string
  project: string | undefined
}) {
  const known = presets.some((p) => p.last === draft.last)
  return (
    <div className="mb-3 flex flex-wrap items-center gap-2 rounded-lg border border-line bg-surface p-3">
      <input
        aria-label="Dashboard title"
        value={draft.title}
        maxLength={200}
        onChange={(e) => onChange({ title: e.target.value })}
        className="h-9 min-w-0 flex-1 basis-56 rounded-md border border-line bg-surface px-2 text-sm md:h-8"
      />
      <SelectFilter
        label="Default range"
        value={draft.last}
        options={[
          ...presets.map((p) => ({ value: p.last, label: p.label })),
          ...(known ? [] : [{ value: draft.last, label: draft.last }]),
        ]}
        onChange={(v) => onChange({ last: v })}
      />
      <SelectFilter
        label="Default refresh"
        value={draft.refresh}
        options={refreshOptions.map((r) => ({ value: r, label: refreshLabel(r) }))}
        onChange={(v) => onChange({ refresh: v as Refresh })}
      />
      <DeleteDashboard uid={uid} project={project} />
    </div>
  )
}

function DeleteDashboard({ uid, project }: { uid: string; project: string | undefined }) {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [error, setError] = useState<string>()
  return (
    <div className="ml-auto">
      <Popover
        align="end"
        className="flex h-9 items-center gap-1.5 rounded-md border border-line px-3 text-sm text-error hover:bg-raised md:h-8"
        label={
          <>
            <icons.trash size={15} />
            Delete
          </>
        }
        panelClassName="w-72"
      >
        {(close) => (
          <div>
            <p className="text-sm">Delete this dashboard? This cannot be undone.</p>
            {error && <p className="mt-1.5 text-xs text-error">{error}</p>}
            <div className="mt-2.5 flex gap-2">
              <button
                type="button"
                onClick={async () => {
                  try {
                    await deleteDashboard(uid, { project })
                    queryClient.removeQueries({ queryKey: ['dashboard', project, uid] })
                    void queryClient.invalidateQueries({ queryKey: ['dashboards', project] })
                    close()
                    await navigate({ to: '/dashboards', search: { project } })
                  } catch (err) {
                    setError(err instanceof Error ? err.message : String(err))
                  }
                }}
                className="h-9 flex-1 rounded-md bg-error text-sm font-medium text-accent-fg"
              >
                Delete
              </button>
              <button
                type="button"
                onClick={close}
                className="h-9 rounded-md border border-line px-3 text-sm hover:bg-raised"
              >
                Keep
              </button>
            </div>
          </div>
        )}
      </Popover>
    </div>
  )
}
