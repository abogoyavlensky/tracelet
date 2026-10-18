// Shown on the metric page while it edits a dashboard panel's query. Save
// refuses when the panel's query changed elsewhere since editing began, and
// otherwise writes only that panel's query into a fresh copy of the
// dashboard at its current revision.
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { useState } from 'react'
import { getDashboard, type PanelMetric, putDashboard } from '../api'
import { dashboardWrite, fitPanel, metricFromSearch, sameMetric } from '../dashboards'
import type { MetricSearch } from '../search'
import { normalizeRange, rangeParams } from '../timeRange'

export function EditQueryBar({
  name,
  search,
  project,
  dashboard: uid,
  panel: panelID,
}: {
  name: string
  search: MetricSearch
  project: string | undefined
  dashboard: string
  panel: string
}) {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const key = ['dashboard', project, uid]
  const dashboard = useQuery({
    queryKey: key,
    queryFn: () => getDashboard(uid, { project }),
    enabled: !!project,
  })
  const panel = dashboard.data?.panels.find((p) => p.id === panelID)
  // The query as it was when editing began: Save compares against it.
  const [kept, setKept] = useState<PanelMetric>()
  if (panel && !kept) setKept(panel.metric)
  const [error, setError] = useState<string>()
  const [saving, setSaving] = useState(false)

  const back = () =>
    navigate({
      to: '/dashboards/$uid',
      params: { uid },
      search: { project, ...rangeParams(normalizeRange(search)) },
    })

  const save = async () => {
    if (!kept) return
    setSaving(true)
    setError(undefined)
    try {
      const fresh = await queryClient.fetchQuery({
        queryKey: key,
        queryFn: () => getDashboard(uid, { project }),
        staleTime: 0,
      })
      const current = fresh.panels.find((p) => p.id === panelID)
      if (!current) throw new Error('This panel no longer exists on the dashboard')
      if (!sameMetric(current.metric, kept)) {
        throw new Error("This panel's query changed elsewhere since you started editing; reload")
      }
      const metric = metricFromSearch(name, search)
      const saved = await putDashboard(
        uid,
        dashboardWrite(
          fresh,
          fresh.panels.map((p) => (p.id === panelID ? fitPanel(p, metric).panel : p)),
        ),
        { project },
      )
      queryClient.setQueryData(key, saved)
      await back()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }

  const missing = dashboard.isSuccess && !panel
  const changes = panel ? fitPanel(panel, metricFromSearch(name, search)).changes : []
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5 border-b border-accent/25 bg-accent/8 px-3 py-2 text-[13px] md:px-5">
      <p className="min-w-0 flex-1">
        {missing ? (
          'This panel no longer exists on the dashboard.'
        ) : (
          <>
            Editing the query of{' '}
            <span className="font-medium">{panel ? panel.title || panel.metric.name : '…'}</span> on{' '}
            <span className="font-medium">{dashboard.data?.title ?? uid}</span>
          </>
        )}
        {changes.length > 0 && (
          <span className="block text-muted">Saving changes the panel: {changes.join('; ')}.</span>
        )}
        {error && <span className="block text-error">{error}</span>}
      </p>
      <span className="flex gap-2">
        <button
          type="button"
          onClick={() => void back()}
          className="h-8 rounded-md border border-line bg-surface px-3 hover:bg-raised"
        >
          Cancel
        </button>
        {!missing && (
          <button
            type="button"
            disabled={saving || !kept}
            onClick={() => void save()}
            className="h-8 rounded-md bg-accent px-3 font-medium text-accent-fg disabled:opacity-60"
          >
            {saving ? 'Saving…' : 'Save to panel'}
          </button>
        )}
      </span>
    </div>
  )
}
