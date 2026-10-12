// The metric catalog: every metric with points in the range.
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { listMetrics, listServices } from '../api'
import { rangeNavigation } from '../components/AppShell'
import { FilterBar } from '../components/FilterBar'
import { MetricList } from '../components/MetricList'
import { SelectFilter } from '../components/SelectFilter'
import { EmptyState, ErrorState, Loading, Notice } from '../components/States'
import { TextFilter } from '../components/TextFilter'
import { TimeRangePicker } from '../components/TimeRangePicker'
import { useProject } from '../components/useProject'
import { formatRange } from '../format'
import { type MetricsSearch, metricsApiParams } from '../search'
import { normalizeRange, rangeParams } from '../timeRange'

export function MetricsPage({ search }: { search: MetricsSearch }) {
  const { slug: project } = useProject()
  const navigate = useNavigate()
  const filters: MetricsSearch = { ...search, project }
  const range = normalizeRange(search)

  const metrics = useQuery({
    queryKey: ['metrics', metricsApiParams(filters)],
    queryFn: () => listMetrics(metricsApiParams(filters)),
    enabled: !!project,
  })
  const services = useQuery({
    queryKey: ['services', project, rangeParams(range)],
    queryFn: () => listServices({ project, ...rangeParams(range) }),
    enabled: !!project,
  })

  const items = metrics.data?.items ?? []
  const resolved = metrics.data?.range
  const set = (patch: Partial<MetricsSearch>) =>
    void navigate({ to: '.', search: (prev: object) => ({ ...prev, ...patch }) } as never)
  const active = [search.service, search.environment].filter(Boolean).length

  return (
    <>
      <FilterBar
        query={search.q ?? ''}
        onQuery={(q) => set({ q: q || undefined })}
        placeholder="Metric name contains"
        active={active}
        summary={resolved && `${items.length} metrics, ${formatRange(resolved.from, resolved.to)}`}
        range={
          <TimeRangePicker
            compact
            value={range}
            resolved={resolved}
            onChange={(r) => void navigate(rangeNavigation(r))}
          />
        }
      >
        <SelectFilter
          label="Service"
          value={search.service ?? ''}
          options={[
            { value: '', label: 'All' },
            ...(services.data?.items ?? [])
              .filter((s) => s.points > 0)
              .map((s) => ({ value: s.service, label: s.service })),
          ]}
          onChange={(v) => set({ service: v || undefined })}
        />
        <TextFilter
          label="Environment"
          value={search.environment}
          placeholder="production"
          onChange={(v) => set({ environment: v })}
        />
      </FilterBar>
      {metrics.data?.truncated && (
        <Notice>
          Showing the first {items.length} metrics. Narrow the list with the name filter or a
          service.
        </Notice>
      )}
      <section
        className="min-h-0 flex-1 overflow-y-auto bg-surface pb-16 md:pb-0"
        aria-label="Metrics"
      >
        {metrics.isPending && project ? (
          <Loading label="Listing metrics…" />
        ) : metrics.isError ? (
          <ErrorState error={metrics.error} onRetry={() => void metrics.refetch()} />
        ) : items.length === 0 ? (
          <EmptyState title="No metrics match">
            {resolved && `Nothing from ${formatRange(resolved.from, resolved.to)}. `}
            Widen the time range or clear the filters. An OpenTelemetry SDK exports metrics to this
            server&apos;s <span className="font-mono">/v1/metrics</span> with an ingest token, as it
            does logs and traces.
          </EmptyState>
        ) : (
          <MetricList
            metrics={items}
            carry={{
              project,
              ...rangeParams(range),
              service: search.service,
              environment: search.environment,
            }}
          />
        )}
      </section>
    </>
  )
}
