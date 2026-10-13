// The logs explorer: filters in the URL, pages from the API's cursor, and
// the selected record in a pane beside the list.
import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { useCallback, useMemo, useState } from 'react'
import { listServices, searchLogs } from '../api'
import { rangeNavigation } from '../components/AppShell'
import { AttrFilter } from '../components/AttrFilter'
import { FilterBar } from '../components/FilterBar'
import { LogDetail } from '../components/LogDetail'
import { LogList } from '../components/LogList'
import { Pane, useListKeys } from '../components/Pane'
import { SelectFilter } from '../components/SelectFilter'
import { EmptyState, ErrorState, LoadMore, Loading } from '../components/States'
import { TextFilter } from '../components/TextFilter'
import { TimeRangePicker } from '../components/TimeRangePicker'
import { useProject } from '../components/useProject'
import { formatRange, levels } from '../format'
import { logKey } from '../live'
import { type LogsSearch, logsApiParams } from '../search'
import { normalizeRange, rangeParams } from '../timeRange'

export function LogsPage({ search }: { search: LogsSearch }) {
  const { slug: project } = useProject()
  const navigate = useNavigate()
  const filters: LogsSearch = { ...search, project }
  const range = normalizeRange(search)

  const logs = useInfiniteQuery({
    queryKey: ['logs', logsApiParams(filters)],
    queryFn: ({ pageParam }) => searchLogs({ ...logsApiParams(filters, pageParam), limit: 100 }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next_cursor,
    enabled: !!project,
  })
  const services = useQuery({
    queryKey: ['services', project, rangeParams(range)],
    queryFn: () => listServices({ project, ...rangeParams(range) }),
    enabled: !!project,
  })

  const items = useMemo(() => logs.data?.pages.flatMap((p) => p.items) ?? [], [logs.data])
  const resolved = logs.data?.pages[0]?.range
  // The selection follows the record, not its position, so a refresh that
  // brings in newer logs keeps the same record open (or closes the pane
  // when it is gone).
  const [selectedKey, setSelectedKey] = useState<string | null>(null)
  const found = selectedKey === null ? -1 : items.findIndex((l) => logKey(l) === selectedKey)
  const selected = found >= 0 ? found : null
  const select = useCallback(
    (i: number) => setSelectedKey(logKey(items[i])),
    [items, setSelectedKey],
  )
  // A new search starts with nothing selected.
  const key = JSON.stringify(logsApiParams(filters))
  const [seen, setSeen] = useState(key)
  if (key !== seen) {
    setSeen(key)
    setSelectedKey(null)
  }
  useListKeys(items.length, selected, select)
  const close = useCallback(() => setSelectedKey(null), [setSelectedKey])

  const set = (patch: Partial<LogsSearch>) =>
    void navigate({ to: '.', search: (prev: object) => ({ ...prev, ...patch }) } as never)
  const active = [search.service, search.environment, search.level, ...(search.attr ?? [])].filter(
    Boolean,
  ).length
  const open = selected !== null

  return (
    <>
      <FilterBar
        query={search.q ?? ''}
        onQuery={(q) => set({ q: q || undefined })}
        placeholder="Search messages"
        active={active}
        summary={
          resolved &&
          `${items.length}${logs.hasNextPage ? '+' : ''} records, ${formatRange(resolved.from, resolved.to)}, newest first`
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
            ...(services.data?.items ?? []).map((s) => ({ value: s.service, label: s.service })),
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
          label="Level"
          value={search.level ?? ''}
          options={[
            { value: '', label: 'Any' },
            ...levels.map((l) => ({ value: l, label: `${l} and up` })),
          ]}
          onChange={(v) => set({ level: (v || undefined) as LogsSearch['level'] })}
        />
        <AttrFilter
          value={search.attr ?? []}
          onChange={(attr) => set({ attr: attr.length ? attr : undefined })}
        />
      </FilterBar>
      <div className="flex min-h-0 flex-1">
        <section
          className="min-w-0 flex-1 overflow-y-auto bg-surface pb-16 md:pb-0"
          aria-label="Logs"
        >
          {logs.isPending && project ? (
            <Loading label="Searching logs…" />
          ) : logs.isError ? (
            <ErrorState error={logs.error} onRetry={() => void logs.refetch()} />
          ) : items.length === 0 ? (
            <EmptyState title="No logs match">
              {resolved && `Nothing from ${formatRange(resolved.from, resolved.to)}. `}
              Widen the time range or clear some filters.
            </EmptyState>
          ) : (
            <>
              <LogList logs={items} selected={selected} narrow={open} onSelect={select} />
              <LoadMore
                hasMore={!!logs.hasNextPage}
                loading={logs.isFetchingNextPage}
                onLoad={() => void logs.fetchNextPage()}
                label="Load older"
              />
            </>
          )}
        </section>
        {open && (
          <Pane onClose={close} back="Logs">
            <LogDetail log={items[selected]} project={project} />
          </Pane>
        )}
      </div>
    </>
  )
}
