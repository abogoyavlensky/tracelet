// One trace: a summary, the waterfall, and the selected span in the pane.
// `span` in the URL is the selection; a trace with errors opens on its
// first failed span.
import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { useCallback, useMemo, useState } from 'react'
import { ApiError, getTrace, searchLogs } from '../api'
import { CopyButton } from '../components/LogDetail'
import { icons } from '../components/icons'
import { Pane, useListKeys } from '../components/Pane'
import type { TraceLogs } from '../components/SpanDetail'
import { SpanDetail } from '../components/SpanDetail'
import { EmptyState, ErrorState, Loading, Notice } from '../components/States'
import { seriesBg } from '../components/tones'
import { useProject } from '../components/useProject'
import { Waterfall } from '../components/Waterfall'
import { formatClock, formatDuration, formatRange, formatTimestamp } from '../format'
import type { TraceSearch } from '../search'
import { traceLogsRange } from '../timeRange'
import { buildWaterfall, visibleRows } from '../waterfall'

const relatedLogsPage = 500

export function TracePage({ traceId, search }: { traceId: string; search: TraceSearch }) {
  const { slug: project } = useProject()
  const navigate = useNavigate()
  const range = search.from && search.to ? { from: search.from, to: search.to } : {}

  const trace = useQuery({
    queryKey: ['trace', project, traceId, range],
    queryFn: () => getTrace(traceId, { project, ...range }),
    enabled: !!project,
    retry: false,
  })
  const logsRange = trace.data ? traceLogsRange(trace.data) : undefined
  const logs = useInfiniteQuery({
    queryKey: ['trace-logs', project, traceId, logsRange],
    queryFn: ({ pageParam }) =>
      searchLogs(
        pageParam
          ? { project, trace_id: traceId, cursor: pageParam, limit: relatedLogsPage }
          : { project, trace_id: traceId, ...logsRange, limit: relatedLogsPage },
      ),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next_cursor,
    enabled: !!project && !!logsRange,
  })

  const rows = useMemo(() => (trace.data ? buildWaterfall(trace.data) : []), [trace.data])
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set())
  const shown = useMemo(() => visibleRows(rows, collapsed), [rows, collapsed])
  const firstError = rows.find((r) => r.span.status_code === 2)?.span.span_id

  // Wide screens open the pane on the first failure; phones start on the waterfall.
  const [paneOpen, setPaneOpen] = useState(() => !!search.span || window.innerWidth >= 768)
  const selectedId = search.span ?? firstError
  const selectedRow = rows.find((r) => r.span.span_id === selectedId)
  const open = paneOpen && !!selectedRow

  const select = useCallback(
    (spanId: string) => {
      setPaneOpen(true)
      void navigate({
        to: '.',
        search: (prev: object) => ({ ...prev, span: spanId }),
        replace: true,
      } as never)
    },
    [navigate, setPaneOpen],
  )
  const close = useCallback(() => setPaneOpen(false), [setPaneOpen])
  const index = shown.findIndex((r) => r.span.span_id === selectedId)
  const selectIndex = useCallback((i: number) => select(shown[i].span.span_id), [select, shown])
  useListKeys(shown.length, index >= 0 ? index : null, selectIndex)

  const related: TraceLogs = {
    logs: useMemo(() => (logs.data?.pages.flatMap((p) => p.items) ?? []).toReversed(), [logs.data]),
    hasMore: !!logs.hasNextPage,
    loading: logs.isFetching,
    error: logs.error,
    loadMore: () => void logs.fetchNextPage(),
  }

  if (trace.isPending && project) return <Loading label="Loading the trace…" />
  if (trace.error instanceof ApiError && trace.error.status === 404) {
    return (
      <EmptyState title="Trace not found">
        <p>
          No spans of trace <span className="font-mono break-all">{traceId}</span>{' '}
          {search.from && search.to
            ? `from ${formatRange(search.from, search.to)}`
            : 'in the last 7 days'}
          . It may be older, or in another project.
        </p>
        {search.from && (
          <Link
            to="/traces/$traceId"
            params={{ traceId }}
            search={{ project }}
            className="mt-3 inline-block text-accent hover:underline"
          >
            Search the last 7 days
          </Link>
        )}
      </EmptyState>
    )
  }
  if (trace.isError) return <ErrorState error={trace.error} onRetry={() => void trace.refetch()} />
  if (!trace.data) return null

  const t = trace.data
  const root = rows.find((r) => !r.span.parent_span_id)?.span
  const errors = rows.filter((r) => r.span.status_code === 2).length
  const orphans = rows.filter((r) => r.orphan).length
  const colors = new Map(rows.map((r) => [r.span.service, r.colorIndex]))
  const refresh = () => {
    void trace.refetch()
    void logs.refetch()
  }

  return (
    <>
      <section className="shrink-0 border-b border-line bg-surface px-4 py-3 md:px-5">
        <div className="flex items-center gap-2 text-sm text-muted">
          <Link
            to="/traces"
            search={{ project }}
            className="flex shrink-0 items-center gap-1 hover:text-fg"
          >
            <icons.chevronLeft size={15} />
            Traces
          </Link>
          <span className="text-subtle">/</span>
          <span className="min-w-0 truncate font-mono text-xs" title={t.trace_id}>
            {t.trace_id}
          </span>
          <div className="ml-auto flex shrink-0 items-center gap-1">
            <span className="hidden sm:block">
              <CopyButton text={t.trace_id} label="Copy ID" />
            </span>
            <button
              type="button"
              onClick={refresh}
              className="flex h-9 items-center gap-1.5 rounded-md px-2.5 text-sm hover:bg-raised hover:text-fg"
            >
              <icons.refresh size={14} className={trace.isFetching ? 'animate-spin' : ''} />
              Refresh
            </button>
          </div>
        </div>
        <div className="mt-1.5 flex flex-wrap items-center gap-x-5 gap-y-2">
          <h1 className="font-mono text-[17px] font-semibold break-all">
            {root?.name ?? `Trace ${t.trace_id.slice(0, 8)}`}
          </h1>
          <Stat label="Duration" value={formatDuration(t.duration_ns)} />
          <Stat label="Spans" value={String(t.spans.length)} />
          <Stat label="Errors" value={String(errors)} tone={errors ? 'text-error' : undefined} />
          <Stat label="Started" value={formatClock(t.start)} title={formatTimestamp(t.start)} />
          <div className="flex flex-wrap gap-1.5">
            {t.services.map((s) => (
              <span
                key={s}
                className="flex items-center gap-1.5 rounded-full border border-line px-2 py-0.5 text-xs"
              >
                <span className={`size-2 rounded-full ${seriesBg[colors.get(s) ?? 0]}`} />
                {s}
              </span>
            ))}
          </div>
        </div>
      </section>
      {t.truncated && (
        <Notice>Showing the first 5,000 spans; the rest of this trace is not loaded.</Notice>
      )}
      {orphans > 0 && (
        <Notice>
          {orphans === 1 ? 'One span is' : `${orphans} spans are`} waiting for a parent that is not
          here yet; the trace may still be arriving. Refresh in a moment.
        </Notice>
      )}
      <div className="flex min-h-0 flex-1">
        <section
          className="min-w-0 flex-1 overflow-y-auto bg-surface pb-tabbar md:pb-0"
          aria-label="Waterfall"
        >
          <Waterfall
            rows={rows}
            durationNs={t.duration_ns}
            selected={selectedId}
            narrow={open}
            collapsed={collapsed}
            onToggle={(id) => {
              const next = new Set(collapsed)
              if (!next.delete(id)) next.add(id)
              setCollapsed(next)
            }}
            onSelect={select}
          />
        </section>
        {open && selectedRow && (
          <Pane onClose={close} back="Waterfall">
            <SpanDetail span={selectedRow.span} trace={t} project={project} related={related} />
          </Pane>
        )}
      </div>
    </>
  )
}

function Stat({
  label,
  value,
  tone,
  title,
}: {
  label: string
  value: string
  tone?: string
  title?: string
}) {
  return (
    <span className="flex items-baseline gap-1.5 text-sm" title={title}>
      <span className="text-muted">{label}</span>
      <span className={`font-mono font-medium tabular-nums ${tone ?? ''}`}>{value}</span>
    </span>
  )
}
