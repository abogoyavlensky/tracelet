// Traces: span search, root spans only by default, which lists traces.
import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { useMemo } from 'react'
import { listServices, searchSpans } from '../api'
import { rangeNavigation } from '../components/AppShell'
import { AttrFilter } from '../components/AttrFilter'
import { FilterBar } from '../components/FilterBar'
import { SelectFilter } from '../components/SelectFilter'
import { SpanList } from '../components/SpanList'
import { EmptyState, ErrorState, LoadMore, Loading } from '../components/States'
import { TextFilter } from '../components/TextFilter'
import { TimeRangePicker } from '../components/TimeRangePicker'
import { useProject } from '../components/useProject'
import { formatRange } from '../format'
import { type TracesSearch, spansApiParams } from '../search'
import { isGoDuration, normalizeRange, rangeParams } from '../timeRange'

export function TracesPage({ search }: { search: TracesSearch }) {
  const { slug: project } = useProject()
  const navigate = useNavigate()
  const filters: TracesSearch = { ...search, project }
  const range = normalizeRange(search)
  const rootOnly = search.root !== false

  const spans = useInfiniteQuery({
    queryKey: ['spans', spansApiParams(filters)],
    queryFn: ({ pageParam }) => searchSpans({ ...spansApiParams(filters, pageParam), limit: 100 }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next_cursor,
    enabled: !!project,
  })
  const services = useQuery({
    queryKey: ['services', project, rangeParams(range)],
    queryFn: () => listServices({ project, ...rangeParams(range) }),
    enabled: !!project,
  })

  const items = useMemo(() => spans.data?.pages.flatMap((p) => p.items) ?? [], [spans.data])
  const resolved = spans.data?.pages[0]?.range
  const set = (patch: Partial<TracesSearch>) =>
    void navigate({ to: '.', search: (prev: object) => ({ ...prev, ...patch }) } as never)
  const active = [
    search.service,
    search.environment,
    search.status,
    search.min_duration,
    search.root === false ? 'all' : undefined,
    ...(search.attr ?? []),
  ].filter(Boolean).length
  const noun = rootOnly ? 'traces' : 'spans'

  return (
    <>
      <FilterBar
        query={search.name ?? ''}
        onQuery={(name) => set({ name: name || undefined })}
        placeholder="Span name, exact"
        active={active}
        summary={
          resolved &&
          `${items.length}${spans.hasNextPage ? '+' : ''} ${noun}, ${formatRange(resolved.from, resolved.to)}, newest first`
        }
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
              .filter((s) => s.spans > 0)
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
        <SelectFilter
          label="Status"
          value={search.status ?? ''}
          options={[
            { value: '', label: 'Any' },
            { value: 'error', label: 'Error' },
            { value: 'ok', label: 'OK' },
            { value: 'unset', label: 'Unset' },
          ]}
          onChange={(v) => set({ status: (v || undefined) as TracesSearch['status'] })}
        />
        <TextFilter
          label="Slower than"
          value={search.min_duration}
          placeholder="500ms"
          hint="A duration such as 250ms, 1.5s, or 2m."
          mono
          validate={(t) =>
            isGoDuration(t) ? undefined : 'Use a duration such as 250ms, 1.5s, or 2m.'
          }
          onChange={(v) => set({ min_duration: v })}
        />
        <label className="flex h-9 shrink-0 cursor-pointer items-center gap-2 rounded-md px-2 text-[13px] hover:bg-raised md:h-8">
          <input
            type="checkbox"
            checked={rootOnly}
            onChange={(e) => set({ root: e.target.checked ? undefined : false })}
            className="size-4 accent-[var(--accent)]"
          />
          Root spans only
        </label>
        <AttrFilter
          value={search.attr ?? []}
          onChange={(attr) => set({ attr: attr.length ? attr : undefined })}
        />
      </FilterBar>
      <section
        className="min-h-0 flex-1 overflow-y-auto bg-surface pb-16 md:pb-0"
        aria-label={rootOnly ? 'Traces' : 'Spans'}
      >
        {spans.isPending && project ? (
          <Loading label={`Searching ${noun}…`} />
        ) : spans.isError ? (
          <ErrorState error={spans.error} onRetry={() => void spans.refetch()} />
        ) : items.length === 0 ? (
          <EmptyState title={`No ${noun} match`}>
            {resolved && `Nothing from ${formatRange(resolved.from, resolved.to)}. `}
            Widen the time range or clear some filters.
            {rootOnly &&
              ' A trace whose root span has not arrived is not listed here; turn off "Root spans only" to search every span.'}
          </EmptyState>
        ) : (
          <>
            <SpanList spans={items} project={project} linkToSpan={!rootOnly} />
            <LoadMore
              hasMore={!!spans.hasNextPage}
              loading={spans.isFetchingNextPage}
              onLoad={() => void spans.fetchNextPage()}
              label="Load older"
            />
          </>
        )}
      </section>
    </>
  )
}
