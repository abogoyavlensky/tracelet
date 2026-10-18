// One dashboard panel: runs its metric query under the dashboard's range and
// filters and draws it as a chart, one number, or a table. In edit mode it
// also shows the controls that change it in the draft.
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { useMemo, useState } from 'react'
import {
  ApiError,
  type MetricResult,
  type PanelType,
  queryMetric,
  type Refresh,
  type StoredPanel,
} from '../api'
import {
  allowedReduces,
  allowedTypes,
  type DashboardFilters,
  effectiveMetric,
  panelQueryParams,
  reducedValue,
  refreshInterval,
  searchFromPanel,
} from '../dashboards'
import { incompatibleSeries, lastValue, seriesLabel, toChartData } from '../metrics'
import type { TimeRange } from '../timeRange'
import { axisFormatter, displayUnit, formatValue } from '../units'
import { icons } from './icons'
import { MetricChart } from './MetricChart'
import { MetricLegend } from './MetricLegend'
import { Popover } from './Popover'
import { SelectFilter } from './SelectFilter'

const typeLabels: Record<PanelType, string> = {
  timeseries: 'Time series',
  stat: 'Stat',
  table: 'Table',
}

/** What edit mode lets a panel change; absent outside edit mode. */
export type PanelEditing = {
  onChange: (panel: StoredPanel) => void
  onMove: (delta: number) => void
  onRemove: () => void
  first: boolean
  last: boolean
}

export function DashboardPanel({
  panel,
  dashboard,
  filters,
  range,
  project,
  refresh,
  canEditQuery,
  editing,
}: {
  panel: StoredPanel
  /** The dashboard's uid, for Edit query. */
  dashboard: string
  filters: DashboardFilters
  range: TimeRange
  project: string | undefined
  refresh: Refresh
  /** Whether the menu offers Edit query: an admin, outside edit mode. */
  canEditQuery: boolean
  editing?: PanelEditing
}) {
  const params = panelQueryParams(panel, filters, range, project)
  const result = useQuery({
    queryKey: ['metric', params],
    queryFn: () => queryMetric(params),
    enabled: !!project,
    placeholderData: keepPreviousData,
    refetchInterval: refreshInterval(refresh),
    retry: (count, error) => !(error instanceof ApiError && error.status < 500) && count < 2,
  })
  const title = panel.title || panel.metric.name
  const data = result.data
  const notFound = result.error instanceof ApiError && result.error.status === 404

  return (
    <article className="flex min-w-0 flex-col rounded-lg border border-line bg-surface">
      <header className="flex min-h-10 items-center gap-2 border-b border-line/60 px-3 py-1.5">
        <h2 className="min-w-0 flex-1 truncate text-[13.5px] font-medium" title={title}>
          {title}
        </h2>
        {data && (
          <span className="hidden shrink-0 text-[12px] text-muted sm:inline">{data.aggregate}</span>
        )}
        <PanelMenu
          panel={panel}
          dashboard={dashboard}
          filters={filters}
          range={range}
          project={project}
          canEditQuery={canEditQuery}
        />
      </header>
      {editing && <PanelControls panel={panel} editing={editing} />}
      <div className={`min-h-0 flex-1 p-3 ${result.isPlaceholderData ? 'opacity-60' : ''}`}>
        {result.isPending && project ? (
          <p className="py-8 text-center text-[13px] text-muted">Querying…</p>
        ) : notFound ? (
          <p className="py-8 text-center text-[13px] text-muted">No points in this range</p>
        ) : result.isError ? (
          <div className="py-4 text-[13px]">
            <p className="break-words text-error">{result.error.message}</p>
            <button
              type="button"
              onClick={() => void result.refetch()}
              className="mt-2 h-8 rounded-md border border-line px-3 text-[13px] hover:bg-raised"
            >
              Retry
            </button>
          </div>
        ) : data ? (
          <PanelBody panel={panel} data={data} />
        ) : null}
      </div>
      {data && <PanelNotes data={data} />}
    </article>
  )
}

function PanelBody({ panel, data }: { panel: StoredPanel; data: MetricResult }) {
  const display = displayUnit(data, data.aggregate)
  const labels = useMemo(() => data.series.map((s) => seriesLabel(s.group, data.name)), [data])
  if (data.series.length === 0) {
    return <p className="py-8 text-center text-[13px] text-muted">No series</p>
  }
  if (panel.type === 'stat') {
    const value = reducedValue(data, 0, panel.reduce ?? 'last')
    return (
      <div className="flex h-full flex-col justify-center py-2">
        <p className="font-mono text-[30px] leading-tight font-medium tabular-nums">
          {formatValue(value, display)}
        </p>
        <p className="mt-1 text-[12px] text-muted">
          {panel.reduce === 'sum' ? 'Total over the range' : 'Latest'}
        </p>
      </div>
    )
  }
  if (panel.type === 'table') {
    return (
      <table className="w-full text-[13px]">
        <thead>
          <tr className="text-left text-[12px] text-muted">
            <th className="pb-1.5 font-normal">{data.group_by || 'Series'}</th>
            <th className="pb-1.5 text-right font-normal">
              {panel.reduce === 'sum' ? 'Total' : 'Latest'}
            </th>
          </tr>
        </thead>
        <tbody>
          {data.series.map((_, i) => (
            <tr key={`${i}-${labels[i]}`} className="border-t border-line/60">
              <td className="max-w-0 truncate py-1.5 pr-3" title={labels[i]}>
                {labels[i]}
              </td>
              <td className="py-1.5 text-right font-mono text-[12.5px] tabular-nums">
                {formatValue(reducedValue(data, i, panel.reduce ?? 'last'), display)}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    )
  }
  return <TimeSeries data={data} labels={labels} />
}

function TimeSeries({ data, labels }: { data: MetricResult; labels: string[] }) {
  const display = displayUnit(data, data.aggregate)
  const chartData = useMemo(() => toChartData(data), [data])
  const [hidden, setHidden] = useState<ReadonlySet<number>>(new Set())
  const [cursor, setCursor] = useState<number | null>(null)
  // A new set of series starts with every one visible.
  const shape = labels.join('\u0000')
  const [seenShape, setSeenShape] = useState(shape)
  if (shape !== seenShape) {
    setSeenShape(shape)
    setHidden(new Set())
  }
  return (
    <>
      <MetricChart
        data={chartData}
        labels={labels}
        hidden={hidden}
        formatY={axisFormatter(display)}
        onCursor={setCursor}
        height={200}
      />
      <div className="mt-2">
        <MetricLegend
          items={data.series.map((s, i) => ({
            label: labels[i],
            value: formatValue(cursor === null ? lastValue(s) : s.values[cursor], display),
          }))}
          hidden={hidden}
          onToggle={(i) => {
            const next = new Set(hidden)
            if (next.has(i)) next.delete(i)
            else next.add(i)
            setHidden(next)
          }}
        />
      </div>
    </>
  )
}

/** The metric page's two warnings, so no panel hides that it shows part of the data. */
function PanelNotes({ data }: { data: MetricResult }) {
  const incompatible = incompatibleSeries(data)
  if (!data.truncated && incompatible === 0) return null
  return (
    <div className="space-y-0.5 border-t border-sev-warn/25 bg-sev-warn/8 px-3 py-1.5 text-[12px] text-sev-warn">
      {data.truncated && <p>The {data.series.length} largest groups are shown.</p>}
      {incompatible > 0 && <p>{incompatible} series with different bucket bounds left out.</p>}
    </div>
  )
}

function PanelMenu({
  panel,
  dashboard,
  filters,
  range,
  project,
  canEditQuery,
}: {
  panel: StoredPanel
  dashboard: string
  filters: DashboardFilters
  range: TimeRange
  project: string | undefined
  canEditQuery: boolean
}) {
  const item = 'flex h-9 w-full items-center rounded-md px-2 text-left text-sm hover:bg-raised'
  return (
    <Popover
      align="end"
      title="Panel menu"
      className="grid size-8 shrink-0 place-items-center rounded-md text-muted hover:bg-raised hover:text-fg"
      label={<icons.more size={16} />}
      panelClassName="w-52 p-1.5"
    >
      {() => (
        <div>
          <Link
            to="/metrics/$name"
            params={{ name: panel.metric.name }}
            search={{ project, ...searchFromPanel(effectiveMetric(panel.metric, filters), range) }}
            className={item}
          >
            Open in Metrics
          </Link>
          {canEditQuery && (
            <Link
              to="/metrics/$name"
              params={{ name: panel.metric.name }}
              search={{
                project,
                ...searchFromPanel(panel.metric, range, { dashboard, panel: panel.id }),
              }}
              className={item}
            >
              Edit query
            </Link>
          )}
        </div>
      )}
    </Popover>
  )
}

function PanelControls({ panel, editing }: { panel: StoredPanel; editing: PanelEditing }) {
  const types = allowedTypes(panel.metric)
  const reduces = allowedReduces(panel.metric)
  const set = (patch: Partial<StoredPanel>) => editing.onChange({ ...panel, ...patch })
  const button =
    'grid size-8 shrink-0 place-items-center rounded-md border border-line text-muted hover:bg-raised hover:text-fg disabled:opacity-40'
  return (
    <div className="flex flex-wrap items-center gap-1.5 border-b border-line/60 bg-raised/40 px-3 py-2">
      <input
        aria-label="Panel title"
        value={panel.title ?? ''}
        placeholder={panel.metric.name}
        maxLength={200}
        onChange={(e) => set({ title: e.target.value || undefined })}
        className="h-8 min-w-0 flex-1 basis-40 rounded-md border border-line bg-surface px-2 text-[13px] placeholder:text-subtle"
      />
      <SelectFilter
        neutral
        label="Type"
        value={panel.type}
        options={types.map((t) => ({ value: t, label: typeLabels[t] }))}
        onChange={(v) => {
          const type = v as PanelType
          // Only stat and table reduce; the server refuses reduce elsewhere.
          set({ type, reduce: type === 'timeseries' ? undefined : (panel.reduce ?? 'last') })
        }}
      />
      <SelectFilter
        neutral
        label="Width"
        value={String(panel.width)}
        options={[1, 2, 3, 4].map((w) => ({ value: String(w), label: `${w}/4` }))}
        onChange={(v) => set({ width: Number(v) })}
      />
      {panel.type !== 'timeseries' && (
        <SelectFilter
          neutral
          label="Value"
          value={panel.reduce ?? 'last'}
          options={reduces.map((r) => ({ value: r, label: r === 'sum' ? 'Total' : 'Latest' }))}
          onChange={(v) => set({ reduce: v as StoredPanel['reduce'] })}
        />
      )}
      <span className="ml-auto flex gap-1.5">
        <button
          type="button"
          aria-label="Move earlier"
          title="Move earlier"
          disabled={editing.first}
          onClick={() => editing.onMove(-1)}
          className={button}
        >
          <icons.chevronLeft size={15} />
        </button>
        <button
          type="button"
          aria-label="Move later"
          title="Move later"
          disabled={editing.last}
          onClick={() => editing.onMove(1)}
          className={button}
        >
          <icons.chevronRight size={15} />
        </button>
        <button
          type="button"
          aria-label="Remove panel"
          title="Remove panel"
          onClick={editing.onRemove}
          className={`${button} hover:text-error`}
        >
          <icons.trash size={15} />
        </button>
      </span>
    </div>
  )
}
