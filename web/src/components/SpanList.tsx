// Spans, one per row, each a link to its trace. Durations are drawn
// against the slowest span on the page, so outliers stand out.
import { Link } from '@tanstack/react-router'
import type { Span } from '../api'
import { formatClock, formatDuration } from '../format'
import { rangeAround } from '../timeRange'
import { serviceColorIndex } from '../waterfall'
import { seriesBg } from './tones'

export function SpanList({
  spans,
  project,
  linkToSpan,
}: {
  spans: Span[]
  project: string | undefined
  linkToSpan: boolean
}) {
  const slowest = Math.max(1, ...spans.map((s) => s.duration_ns))
  return (
    <ol>
      {spans.map((span) => {
        const failed = span.status_code === 2
        return (
          <li key={`${span.trace_id}-${span.span_id}`}>
            <Link
              to="/traces/$traceId"
              params={{ traceId: span.trace_id }}
              search={{
                project,
                ...rangeAround(span.ts),
                span: linkToSpan ? span.span_id : undefined,
              }}
              className="grid grid-cols-[3px_1fr] border-b border-line/60 hover:bg-raised/60"
            >
              <span className={failed ? 'bg-sev-error' : ''} />
              <span className="grid min-w-0 grid-cols-[auto_1fr_auto] items-center gap-x-3 gap-y-1 px-3 py-2 md:h-9 md:grid-cols-[88px_136px_minmax(0,1fr)_minmax(140px,24%)_52px] md:py-0">
                <span className="font-mono text-[12px] text-muted tabular-nums">
                  {formatClock(span.ts)}
                </span>
                <span className="flex min-w-0 items-center gap-1.5 text-[12.5px] text-muted">
                  <span
                    className={`size-2 shrink-0 rounded-full ${seriesBg[serviceColorIndex(span.service)]}`}
                  />
                  <span className="truncate">{span.service}</span>
                </span>
                <span className="font-mono text-[12px] text-muted tabular-nums md:hidden">
                  {formatDuration(span.duration_ns)}
                </span>
                <span
                  className={`col-span-full truncate text-[13px] md:col-span-1 ${failed ? 'font-medium text-error' : ''}`}
                >
                  {span.name}
                </span>
                <span className="col-span-full flex items-center gap-2 md:col-span-1">
                  <span className="relative h-1.5 flex-1 rounded-full bg-raised">
                    <span
                      className={`absolute inset-y-0 left-0 rounded-full ${failed ? 'bg-error' : 'bg-accent/70'}`}
                      style={{ width: `${Math.max(1, (span.duration_ns / slowest) * 100)}%` }}
                    />
                  </span>
                  <span className="hidden w-16 shrink-0 text-right font-mono text-[12px] tabular-nums md:inline">
                    {formatDuration(span.duration_ns)}
                  </span>
                </span>
                <span
                  className={`hidden text-right text-[12px] md:inline ${failed ? 'text-error' : 'text-subtle'}`}
                >
                  {failed ? 'error' : span.status_code === 1 ? 'ok' : ''}
                </span>
              </span>
            </Link>
          </li>
        )
      })}
    </ol>
  )
}
