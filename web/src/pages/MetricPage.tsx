// One metric over time: aggregation, grouping, filters, and step live in
// the URL; the chart and its legend show the result.
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { useMemo, useState } from 'react'
import { ApiError, getMetricAttributes, queryMetric } from '../api'
import { AttrFilter } from '../components/AttrFilter'
import { rangeNavigation } from '../components/AppShell'
import { FilterBar } from '../components/FilterBar'
import { icons } from '../components/icons'
import { MetricChart } from '../components/MetricChart'
import { MetricLegend } from '../components/MetricLegend'
import { SelectFilter } from '../components/SelectFilter'
import { EmptyState, ErrorState, Loading, Notice } from '../components/States'
import { TextFilter } from '../components/TextFilter'
import { TimeRangePicker } from '../components/TimeRangePicker'
import { useProject } from '../components/useProject'
import { formatRange } from '../format'
import {
  aggregateOptions,
  groupByOptions,
  incompatibleSeries,
  kindLabel,
  lastValue,
  seriesLabel,
  stepOptions,
  toChartData,
} from '../metrics'
import { type MetricSearch, metricApiParams } from '../search'
import { normalizeRange, rangeParams } from '../timeRange'
import { axisFormatter, displayUnit, formatValue, unitHeading } from '../units'

const groupLabels: Record<string, string> = {
  '': 'None',
  service: 'Service',
  environment: 'Environment',
  version: 'Version',
}

/** A narrow-screen media query, for the chart height. */
function useNarrow(): boolean {
  const [narrow] = useState(() => window.matchMedia('(max-width: 767px)').matches)
  return narrow
}

export function MetricPage({ name, search }: { name: string; search: MetricSearch }) {
  const { slug: project } = useProject()
  const navigate = useNavigate()
  const filters: MetricSearch = { ...search, project }
  const range = normalizeRange(search)
  const params = metricApiParams(name, filters)
  const narrow = useNarrow()

  const result = useQuery({
    queryKey: ['metric', params],
    queryFn: () => queryMetric(params),
    enabled: !!project,
    placeholderData: keepPreviousData,
    retry: (count, error) => !(error instanceof ApiError && error.status < 500) && count < 2,
  })
  const attributes = useQuery({
    queryKey: [
      'metric-attributes',
      project,
      name,
      search.service,
      search.environment,
      rangeParams(range),
    ],
    queryFn: () =>
      getMetricAttributes({
        project,
        name,
        service: search.service,
        environment: search.environment,
        ...rangeParams(range),
      }),
    enabled: !!project,
  })

  const data = result.data
  const [hidden, setHidden] = useState<ReadonlySet<number>>(new Set())
  const [cursor, setCursor] = useState<number | null>(null)
  const labels = useMemo(
    () => data?.series.map((s) => seriesLabel(s.group, name)) ?? [],
    [data, name],
  )
  // A new set of series starts with every one visible.
  const shape = labels.join('\u0000')
  const [seenShape, setSeenShape] = useState(shape)
  if (shape !== seenShape) {
    setSeenShape(shape)
    setHidden(new Set())
  }
  const chartData = useMemo(() => (data ? toChartData(data) : undefined), [data])

  const set = (patch: Partial<MetricSearch>) =>
    void navigate({ to: '.', search: (prev: object) => ({ ...prev, ...patch }) } as never)
  const active = [
    search.aggregate,
    search.group_by,
    search.service,
    search.environment,
    search.step,
    ...(search.attr ?? []),
  ].filter(Boolean).length

  const display = data ? displayUnit(data, data.aggregate) : { unit: '', perSecond: false }
  const aggregates = data ? aggregateOptions(data) : undefined
  const notFound = result.error instanceof ApiError && result.error.status === 404
  const incompatible = data ? incompatibleSeries(data) : 0

  return (
    <>
      <FilterBar
        active={active}
        summary={
          data &&
          `${data.series.length} series, ${data.step_seconds}s steps, ${formatRange(data.range.from, data.range.to)}`
        }
        range={
          <TimeRangePicker
            compact
            value={range}
            resolved={data?.range}
            onChange={(r) => void navigate(rangeNavigation(r))}
          />
        }
      >
        {aggregates && (
          <SelectFilter
            label="Aggregate"
            value={search.aggregate ?? ''}
            options={[
              { value: '', label: aggregates.default },
              ...aggregates.options
                .filter((a) => a !== aggregates.default)
                .map((a) => ({ value: a, label: a })),
            ]}
            onChange={(v) => set({ aggregate: v || undefined })}
          />
        )}
        <SelectFilter
          label="Group by"
          value={search.group_by ?? ''}
          options={groupByOptions(
            attributes.data?.items.map((a) => a.key) ?? [],
            search.group_by,
          ).map((g) => ({
            value: g,
            label: groupLabels[g] ?? g,
          }))}
          onChange={(v) => set({ group_by: v || undefined })}
        />
        <TextFilter
          label="Service"
          value={search.service}
          placeholder="api"
          onChange={(v) => set({ service: v })}
        />
        <TextFilter
          label="Environment"
          value={search.environment}
          placeholder="production"
          onChange={(v) => set({ environment: v })}
        />
        <SelectFilter
          label="Step"
          value={search.step ?? ''}
          options={stepOptions.map((s) => ({ value: s, label: s || 'Auto' }))}
          onChange={(v) => set({ step: v || undefined })}
        />
        <AttrFilter
          value={search.attr ?? []}
          onChange={(attr) => set({ attr: attr.length ? attr : undefined })}
        />
      </FilterBar>
      {data?.truncated && (
        <Notice>
          Showing the {data.series.length} largest groups of more. Narrow them with filters.
        </Notice>
      )}
      {incompatible > 0 && (
        <Notice>
          {incompatible} {incompatible === 1 ? 'series has' : 'series have'} different bucket bounds
          and {incompatible === 1 ? 'is' : 'are'} not included.
        </Notice>
      )}
      <section
        className="min-h-0 flex-1 overflow-y-auto bg-surface pb-tabbar md:pb-0"
        aria-label={`Metric ${name}`}
      >
        <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1 px-3 pt-4 pb-2 md:px-5">
          <Link
            to="/metrics"
            search={{
              project,
              ...rangeParams(range),
              service: search.service,
              environment: search.environment,
            }}
            className="flex items-center gap-1 text-[13px] text-muted hover:text-fg"
          >
            <icons.chevronLeft size={14} />
            Metrics
          </Link>
          <h1 className="min-w-0 truncate font-mono text-[15px] font-medium">{name}</h1>
          {data && (
            <span className="flex items-center gap-2 text-[12.5px] text-muted">
              <span className="rounded-full bg-raised px-2 py-0.5 text-[11.5px]">
                {kindLabel(data)}
              </span>
              <span>{data.aggregate}</span>
              {unitHeading(display) && <span>in {unitHeading(display)}</span>}
            </span>
          )}
        </div>
        {result.isPending && project ? (
          <Loading label="Querying…" />
        ) : notFound ? (
          <EmptyState title="No points in this range">
            {name} has no points in the selected time range under these filters. Widen the range,
            clear the filters, or{' '}
            <Link to="/metrics" search={{ project, ...rangeParams(range) }} className="text-accent">
              go back to the catalog
            </Link>
            .
          </EmptyState>
        ) : result.isError ? (
          <ErrorState error={result.error} onRetry={() => void result.refetch()} />
        ) : data && chartData ? (
          <div className={`px-2 pb-4 md:px-4 ${result.isPlaceholderData ? 'opacity-60' : ''}`}>
            <MetricChart
              data={chartData}
              labels={labels}
              hidden={hidden}
              formatY={axisFormatter(display)}
              onCursor={setCursor}
              height={narrow ? 240 : 320}
            />
            <div className="mt-3 px-1 md:px-2">
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
          </div>
        ) : null}
      </section>
    </>
  )
}
