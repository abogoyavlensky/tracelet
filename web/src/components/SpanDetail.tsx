// The selected span in the detail pane, with the trace's logs.
import { Link } from '@tanstack/react-router'
import { useState } from 'react'
import type { Log, Span, Trace } from '../api'
import { entries } from '../attributes'
import {
  formatDuration,
  formatTimestamp,
  nsSince,
  severityName,
  spanKindName,
  spanStatusName,
} from '../format'
import { rangeAround } from '../timeRange'
import { KeyValues } from './KeyValues'
import { LogDetail } from './LogDetail'
import { LoadMore } from './States'
import { severityBg } from './tones'

export type TraceLogs = {
  /** Loaded so far, oldest first. */
  logs: Log[]
  hasMore: boolean
  loading: boolean
  error: unknown
  loadMore: () => void
}

export function SpanDetail({
  span,
  trace,
  project,
  related,
}: {
  span: Span
  trace: Trace
  project: string | undefined
  related: TraceLogs
}) {
  const failed = span.status_code === 2
  return (
    <div className="px-4 py-5 md:px-6">
      <div className="flex flex-wrap items-center gap-2 text-xs">
        <span
          className={`rounded-full px-2 py-0.5 font-medium ${failed ? 'bg-error/10 text-error' : 'bg-raised text-muted'}`}
        >
          {spanStatusName(span.status_code)}
        </span>
        <span className="text-muted">
          {span.service}, {spanKindName(span.kind)} span
        </span>
      </div>
      <h2 className="mt-2 font-mono text-[16px] font-medium break-words">{span.name}</h2>
      <p className="mt-1 text-sm text-muted">
        {formatDuration(span.duration_ns)}, starting {formatDuration(nsSince(trace.start, span.ts))}{' '}
        into the trace at {formatTimestamp(span.ts)}
      </p>
      {span.status_message && (
        <p className="mt-3 rounded-md bg-error/10 px-3 py-2 font-mono text-[13px] break-words text-error">
          {span.status_message}
        </p>
      )}
      {span.events?.map((e, i) => (
        <Event key={i} event={e} spanStart={span.ts} />
      ))}
      <RelatedLogs span={span} project={project} related={related} traceStart={trace.start} />
      <KeyValues title="Attributes" rows={entries(span.attributes)} monoKeys />
      {span.links && span.links.length > 0 && (
        <section className="mt-7">
          <h2 className="mb-2 text-sm font-semibold">Links</h2>
          <ul className="divide-y divide-line/70 rounded-lg border border-line">
            {span.links.map((l) => (
              <li key={`${l.trace_id}-${l.span_id}`} className="px-3 py-2 font-mono text-[12.5px]">
                {l.trace_id === trace.trace_id ? (
                  <span>span {l.span_id} in this trace</span>
                ) : (
                  <Link
                    to="/traces/$traceId"
                    params={{ traceId: l.trace_id }}
                    search={{ project, ...rangeAround(span.ts), span: l.span_id }}
                    className="text-accent hover:underline"
                  >
                    trace {l.trace_id}
                  </Link>
                )}
              </li>
            ))}
          </ul>
        </section>
      )}
      <KeyValues
        title="Identifiers"
        rows={(
          [
            ['Span ID', span.span_id],
            ['Parent span ID', span.parent_span_id ?? 'none (root span)'],
            ['Scope', span.scope ?? ''],
            ['Environment', span.environment ?? ''],
            ['Version', span.version ?? ''],
          ] as [string, string][]
        ).filter(([, v]) => v !== '')}
      />
      <KeyValues title="Resource" rows={entries(span.resource)} monoKeys />
    </div>
  )
}

function Event({
  event: e,
  spanStart,
}: {
  event: NonNullable<Span['events']>[number]
  spanStart: string
}) {
  const offset = e.ts ? ` at +${formatDuration(nsSince(spanStart, e.ts))}` : ''
  if (e.name === 'exception') {
    const stack = e.attributes?.['exception.stacktrace']
    return (
      <section className="mt-5">
        <h3 className="text-sm font-semibold">
          Exception
          <span className="ml-2 font-normal text-muted">
            {String(e.attributes?.['exception.type'] ?? '')}
            {offset}
          </span>
        </h3>
        {e.attributes?.['exception.message'] !== undefined && (
          <p className="mt-1 font-mono text-[12.5px] break-words">
            {String(e.attributes['exception.message'])}
          </p>
        )}
        {typeof stack === 'string' && (
          <pre className="mt-2 overflow-x-auto rounded-lg bg-raised p-3 font-mono text-[12px] leading-5 text-muted">
            {stack}
          </pre>
        )}
      </section>
    )
  }
  return (
    <section className="mt-5">
      <h3 className="text-sm font-semibold">
        Event <span className="font-mono font-normal">{e.name}</span>
        <span className="ml-1 font-normal text-muted">{offset}</span>
      </h3>
      {entries(e.attributes).length > 0 && (
        <dl className="mt-1 grid grid-cols-[minmax(8rem,auto)_1fr] gap-x-3 font-mono text-[12px] leading-5">
          {entries(e.attributes).map(([k, v]) => (
            <div key={k} className="contents">
              <dt className="text-muted">{k}</dt>
              <dd className="break-all">{v}</dd>
            </div>
          ))}
        </dl>
      )}
    </section>
  )
}

function RelatedLogs({
  span,
  project,
  related,
  traceStart,
}: {
  span: Span
  project: string | undefined
  related: TraceLogs
  traceStart: string
}) {
  const [onlySpan, setOnlySpan] = useState(false)
  const [open, setOpen] = useState<number | null>(null)
  const shown = onlySpan ? related.logs.filter((l) => l.span_id === span.span_id) : related.logs
  return (
    <section className="mt-7">
      <div className="mb-2 flex items-center justify-between gap-2">
        <h3 className="text-sm font-semibold">Logs</h3>
        <div
          className="flex rounded-md bg-raised p-0.5 text-xs"
          role="group"
          aria-label="Which logs"
        >
          <button
            type="button"
            aria-pressed={!onlySpan}
            onClick={() => setOnlySpan(false)}
            className={`rounded px-2 py-1 ${!onlySpan ? 'bg-surface shadow-sm' : 'text-muted'}`}
          >
            Whole trace
          </button>
          <button
            type="button"
            aria-pressed={onlySpan}
            onClick={() => setOnlySpan(true)}
            className={`rounded px-2 py-1 ${onlySpan ? 'bg-surface shadow-sm' : 'text-muted'}`}
          >
            This span
          </button>
        </div>
      </div>
      {related.hasMore && (
        <p className="mb-2 text-xs text-sev-warn">
          Showing the newest {related.logs.length} logs of this trace; older ones are not loaded
          yet.
        </p>
      )}
      {related.error ? (
        <p className="text-sm text-error">
          Loading the logs failed:{' '}
          {related.error instanceof Error ? related.error.message : String(related.error)}
        </p>
      ) : related.loading && related.logs.length === 0 ? (
        <p className="text-sm text-muted">Loading logs…</p>
      ) : shown.length === 0 ? (
        <p className="rounded-lg border border-line px-3 py-2.5 text-sm text-muted">
          {onlySpan
            ? related.hasMore
              ? `No logs for this span among the ${related.logs.length} loaded.`
              : 'No logs for this span.'
            : 'No logs carry this trace ID.'}
        </p>
      ) : (
        <ul className="divide-y divide-line/70 overflow-hidden rounded-lg border border-line">
          {shown.map((l, i) => (
            <li key={`${l.ts}-${i}`}>
              <button
                type="button"
                aria-expanded={open === i}
                onClick={() => setOpen(open === i ? null : i)}
                className="flex w-full items-start gap-2 px-3 py-2 text-left hover:bg-raised/60"
              >
                <span
                  className={`mt-1.5 size-2 shrink-0 rounded-full ${severityBg[severityName(l.severity_number)]}`}
                />
                <span
                  className={`min-w-0 flex-1 font-mono text-[12.5px] break-words ${l.span_id === span.span_id ? 'font-semibold' : ''}`}
                >
                  {l.body}
                </span>
                <span className="shrink-0 font-mono text-xs text-muted tabular-nums">
                  +{formatDuration(Math.max(0, nsSince(traceStart, l.ts)))}
                </span>
              </button>
              {open === i && (
                <div className="border-t border-line/70 bg-canvas/40">
                  <LogDetail log={l} project={project} hideTraceLink />
                </div>
              )}
            </li>
          ))}
        </ul>
      )}
      <LoadMore
        hasMore={related.hasMore}
        loading={related.loading}
        onLoad={related.loadMore}
        label="Load older logs"
      />
    </section>
  )
}
