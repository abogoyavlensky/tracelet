// The logs explorer: filters in the URL, pages from the API's cursor, and
// the selected record in a pane beside the list. With Live on, new records
// are merged into the list as they arrive.
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
import { EmptyState, ErrorState, LoadMore, Loading, Notice } from '../components/States'
import { TextFilter } from '../components/TextFilter'
import { TimeRangePicker } from '../components/TimeRangePicker'
import { useLiveTail } from '../components/useLiveTail'
import { useProject } from '../components/useProject'
import { formatClock, formatRange, levels } from '../format'
import { liveEligible, logKey, mergeLive } from '../live'
import { type LogsSearch, logsApiParams } from '../search'
import { normalizeRange, rangeParams } from '../timeRange'

export function LogsPage({ search }: { search: LogsSearch }) {
  const { slug: project } = useProject()
  const navigate = useNavigate()
  const filters: LogsSearch = { ...search, project }
  const range = normalizeRange(search)
  const canLive = liveEligible(range)
  const live = !!search.live && canLive && !!project

  const queryKey = ['logs', logsApiParams(filters)]
  const logs = useInfiniteQuery({
    queryKey,
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

  const { arrivals, from, skippedAt, dismiss, error } = useLiveTail({
    enabled: live,
    search: filters,
    firstPage: logs.data?.pages[0],
    queryKey,
  })

  const base = useMemo(() => logs.data?.pages.flatMap((p) => p.items) ?? [], [logs.data])
  // `from` is set only while live runs and has a first page to follow.
  const items = useMemo(
    () => (from === undefined ? base : mergeLive(arrivals, base, from)),
    [arrivals, from, base],
  )
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
          (live
            ? `${items.length} records, live, newest first`
            : `${items.length}${logs.hasNextPage ? '+' : ''} records, ${formatRange(resolved.from, resolved.to)}, newest first`)
        }
        actions={
          <LiveToggle
            on={live}
            available={canLive}
            error={error}
            onToggle={() => set({ live: live ? undefined : true })}
          />
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
          className="min-w-0 flex-1 overflow-y-auto bg-surface pb-tabbar md:pb-0"
          aria-label="Logs"
        >
          {skippedAt && (
            <Notice>
              More records arrived at {formatClock(skippedAt).slice(0, 8)} than live could show;
              some were skipped. Turn Live off to search them.{' '}
              <button type="button" onClick={dismiss} className="underline">
                Dismiss
              </button>
            </Notice>
          )}
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
                hasMore={!live && !!logs.hasNextPage}
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

/** Follows new logs. Off and disabled on an absolute range, which has no "now". */
function LiveToggle({
  on,
  available,
  error,
  onToggle,
}: {
  on: boolean
  available: boolean
  /** The last poll's error; it turns the dot to the warning tone. */
  error: string | undefined
  onToggle: () => void
}) {
  const title = !available
    ? 'Live needs a relative time range'
    : error
      ? `Live updates failed: ${error}`
      : on
        ? 'Following new logs; click to stop'
        : 'Follow new logs'
  return (
    <button
      type="button"
      aria-pressed={on}
      disabled={!available}
      title={title}
      onClick={onToggle}
      className={`flex h-10 items-center gap-2 rounded-md border px-3 text-sm disabled:opacity-50 md:h-8 md:text-[13px] ${
        on ? 'border-accent/50 bg-raised' : 'border-line hover:bg-raised'
      } ${error && on ? 'text-sev-warn' : ''}`}
    >
      {on && (
        <span className="relative flex size-2">
          <span
            className={`absolute inline-flex size-full animate-ping rounded-full opacity-60 motion-reduce:animate-none ${error ? 'bg-sev-warn' : 'bg-accent'}`}
          />
          <span
            className={`relative inline-flex size-2 rounded-full ${error ? 'bg-sev-warn' : 'bg-accent'}`}
          />
        </span>
      )}
      Live
    </button>
  )
}
