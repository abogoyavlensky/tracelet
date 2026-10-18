// Adds the chart on the metric page to a dashboard, new or existing, as a
// panel at the end. The dashboard is read fresh and written back at that
// revision, so a concurrent edit is never overwritten.
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { useState } from 'react'
import {
  ApiError,
  type Dashboard,
  type DashboardPanel,
  getDashboard,
  listDashboards,
  type PanelType,
  putDashboard,
} from '../api'
import { allowedTypes, dashboardWrite, metricFromSearch, slugify } from '../dashboards'
import type { MetricSearch } from '../search'
import { icons } from './icons'
import { Popover } from './Popover'

const typeLabels: Record<PanelType, string> = {
  timeseries: 'Time series',
  stat: 'Stat',
  table: 'Table',
}
const newDashboard = '\u0000new'

export function AddToDashboard({
  name,
  search,
  project,
}: {
  name: string
  search: MetricSearch
  project: string | undefined
}) {
  return (
    <Popover
      align="end"
      className="flex h-8 items-center gap-1.5 rounded-md border border-line px-2.5 text-[13px] text-muted hover:bg-raised hover:text-fg"
      label={
        <>
          <icons.plus size={14} />
          Add to dashboard
        </>
      }
      panelClassName="w-80"
    >
      {() => <AddForm name={name} search={search} project={project} />}
    </Popover>
  )
}

function AddForm({
  name,
  search,
  project,
}: {
  name: string
  search: MetricSearch
  project: string | undefined
}) {
  const queryClient = useQueryClient()
  const list = useQuery({
    queryKey: ['dashboards', project],
    queryFn: () => listDashboards({ project }),
    enabled: !!project,
  })
  const dashboards = list.data?.items ?? []
  const metric = metricFromSearch(name, search)
  const types = allowedTypes(metric)
  const [target, setTarget] = useState<string>()
  const [newTitle, setNewTitle] = useState('')
  const [title, setTitle] = useState(name)
  const [type, setType] = useState<PanelType>('timeseries')
  const [error, setError] = useState<string>()
  const [saving, setSaving] = useState(false)
  const [added, setAdded] = useState<Dashboard>()
  const chosen = target ?? dashboards[0]?.uid ?? newDashboard

  if (added) {
    return (
      <p className="text-sm">
        Added to{' '}
        <Link
          to="/dashboards/$uid"
          params={{ uid: added.uid }}
          search={{ project }}
          className="font-medium text-accent"
        >
          {added.title}
        </Link>
        .
      </p>
    )
  }

  const add = async () => {
    const panel: DashboardPanel = {
      title: title.trim() && title.trim() !== name ? title.trim() : undefined,
      type: types.includes(type) ? type : 'timeseries',
      metric,
    }
    if (chosen === newDashboard) {
      if (!newTitle.trim()) throw new Error('The new dashboard needs a title.')
      const uid = slugify(newTitle)
      try {
        return await putDashboard(
          uid,
          { title: newTitle.trim(), panels: [panel], revision: 0 },
          { project },
        )
      } catch (err) {
        if (err instanceof ApiError && err.status === 409) {
          throw new Error(`A dashboard with the address ${uid} exists; choose another title.`, {
            cause: err,
          })
        }
        throw err
      }
    }
    // One retry: a 409 means someone saved in between, so read it again.
    for (let attempt = 0; ; attempt++) {
      const fresh = await getDashboard(chosen, { project })
      try {
        return await putDashboard(chosen, dashboardWrite(fresh, [...fresh.panels, panel]), {
          project,
        })
      } catch (err) {
        if (!(err instanceof ApiError && err.status === 409) || attempt > 0) throw err
      }
    }
  }

  const field =
    'mt-1 block h-9 w-full rounded-md border border-line bg-surface px-2 text-sm text-fg'
  return (
    <form
      onSubmit={async (e) => {
        e.preventDefault()
        setSaving(true)
        setError(undefined)
        try {
          const d = await add()
          queryClient.setQueryData(['dashboard', project, d.uid], d)
          void queryClient.invalidateQueries({ queryKey: ['dashboards', project] })
          setAdded(d)
        } catch (err) {
          setError(err instanceof Error ? err.message : String(err))
        } finally {
          setSaving(false)
        }
      }}
      className="space-y-2.5"
    >
      <label className="block text-xs text-muted">
        Dashboard
        <select value={chosen} onChange={(e) => setTarget(e.target.value)} className={field}>
          {dashboards.map((d) => (
            <option key={d.uid} value={d.uid}>
              {d.title}
            </option>
          ))}
          <option value={newDashboard}>New dashboard…</option>
        </select>
      </label>
      {chosen === newDashboard && (
        <label className="block text-xs text-muted">
          New dashboard title
          <input
            value={newTitle}
            maxLength={200}
            onChange={(e) => setNewTitle(e.target.value)}
            placeholder="Shop overview"
            className={`${field} placeholder:text-subtle`}
          />
        </label>
      )}
      <label className="block text-xs text-muted">
        Panel title
        <input
          value={title}
          maxLength={200}
          onChange={(e) => setTitle(e.target.value)}
          className={field}
        />
      </label>
      <label className="block text-xs text-muted">
        Show as
        <select
          value={type}
          onChange={(e) => setType(e.target.value as PanelType)}
          className={field}
        >
          {(['timeseries', 'stat', 'table'] as const).map((t) => (
            <option key={t} value={t} disabled={!types.includes(t)}>
              {typeLabels[t]}
              {types.includes(t) ? '' : ' (not when grouped)'}
            </option>
          ))}
        </select>
      </label>
      {error && <p className="text-xs text-error">{error}</p>}
      <button
        type="submit"
        disabled={saving}
        className="h-9 w-full rounded-md bg-accent text-sm font-medium text-accent-fg disabled:opacity-60"
      >
        {saving ? 'Adding…' : 'Add'}
      </button>
    </form>
  )
}
