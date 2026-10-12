// Variant C trace: a summary in words, a soft waterfall card, and the
// selected span in a drawer whose logs read as a timeline.
import { useMemo, useState } from 'react'
import type { Span } from '../../api'
import { formatDuration, formatTimestamp, nsSince, severityName, spanKindName } from '../../format'
import { buildWaterfall } from '../../waterfall'
import { errorSpanId, trace, traceLogs } from '../fixtures'
import { entries, icons, seriesBg, severityBg } from '../shared'
import { Drawer, Fields, Shell } from './Chrome'
import './tokens.css'

export function TraceScreen() {
  const rows = useMemo(() => buildWaterfall(trace), [])
  const [selected, setSelected] = useState<string | null>(() =>
    window.innerWidth >= 768 ? errorSpanId : null,
  )
  const span = rows.find((r) => r.span.span_id === selected)?.span
  const root = rows[0].span
  const failed = rows.filter((r) => r.span.status_code === 2).length
  const colorOf = (service: string) => rows.find((r) => r.span.service === service)?.colorIndex ?? 0

  return (
    <Shell active="traces">
      <div className="px-4 py-5 md:px-8 md:py-7">
        <a href="#" className="inline-flex items-center gap-1 text-sm text-muted hover:text-fg">
          <icons.chevronLeft size={15} />
          All traces
        </a>
        <h1 className="mt-2 text-xl font-semibold tracking-tight md:text-2xl">{root.name}</h1>
        <p className="mt-1 text-sm text-muted">
          {formatDuration(trace.duration_ns)} across {trace.services.length} services and{' '}
          {trace.spans.length} spans; <span className="text-error">{failed} failed</span>. Started{' '}
          {formatTimestamp(trace.start)}.
        </p>
        <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-2 text-sm">
          {trace.services.map((s) => (
            <span key={s} className="flex items-center gap-1.5 text-muted">
              <span className={`h-2 w-4 rounded-full ${seriesBg[colorOf(s)]}`} />
              {s}
            </span>
          ))}
          <button className="flex items-center gap-1.5 font-mono text-xs text-muted hover:text-fg">
            {trace.trace_id}
            <icons.copy size={12} />
          </button>
        </div>
        <p className="mt-4 rounded-xl bg-sev-warn/10 px-4 py-2.5 text-sm text-sev-warn">
          One span is waiting for its parent. The trace may still be arriving; refresh in a moment.
        </p>
        <div className="mt-4 overflow-hidden rounded-2xl border border-line bg-surface">
          {rows.map((r) => (
            <button
              key={r.span.span_id}
              onClick={() => setSelected(r.span.span_id)}
              className={`grid w-full grid-cols-1 gap-1.5 border-b border-line/50 px-4 py-2 text-left last:border-b-0 md:grid-cols-[minmax(240px,36%)_1fr] md:items-center md:gap-4 md:py-0 ${
                r.span.span_id === selected ? 'bg-accent/8' : 'hover:bg-raised/50'
              }`}
            >
              <span
                className="flex min-w-0 items-center gap-2 text-sm md:h-9"
                style={{ paddingLeft: r.depth * 16 }}
              >
                <span
                  className={`size-2 shrink-0 rounded-full ${r.span.status_code === 2 ? 'bg-error' : seriesBg[r.colorIndex]}`}
                />
                <span
                  className={`truncate ${r.span.status_code === 2 ? 'font-medium text-error' : ''}`}
                >
                  {r.span.name}
                </span>
                <span className="ml-auto shrink-0 text-xs text-muted tabular-nums md:hidden">
                  {formatDuration(r.span.duration_ns)}
                </span>
              </span>
              <span className="flex items-center gap-2">
                <span className="relative h-2 flex-1 rounded-full bg-raised">
                  <span
                    className={`absolute inset-y-0 rounded-full ${r.span.status_code === 2 ? 'bg-error' : seriesBg[r.colorIndex]}`}
                    style={{ left: `${r.offsetPct}%`, width: `${r.widthPct}%` }}
                  />
                </span>
                <span className="hidden w-14 shrink-0 text-right text-xs text-muted tabular-nums md:block">
                  {formatDuration(r.span.duration_ns)}
                </span>
              </span>
            </button>
          ))}
        </div>
      </div>
      {span && <SpanDrawer span={span} onClose={() => setSelected(null)} />}
    </Shell>
  )
}

function SpanDrawer({ span, onClose }: { span: Span; onClose: () => void }) {
  const failed = span.status_code === 2
  return (
    <Drawer
      onClose={onClose}
      title={
        <p className="text-sm text-muted">
          {span.service}, {spanKindName(span.kind)} span
        </p>
      }
    >
      <h2 className={`text-lg font-semibold ${failed ? 'text-error' : ''}`}>{span.name}</h2>
      <p className="mt-1 text-sm text-muted">
        Took {formatDuration(span.duration_ns)}, starting{' '}
        {formatDuration(nsSince(trace.start, span.ts))} into the trace.
      </p>
      {span.status_message && (
        <p className="mt-4 rounded-xl bg-error/10 px-4 py-3 text-sm text-error">
          {span.status_message}
        </p>
      )}
      <section className="mt-6">
        <h3 className="text-sm font-semibold">What happened</h3>
        <ol className="relative mt-3 ml-1.5 border-l border-line">
          {traceLogs.map((l, i) => {
            const mine = l.span_id === span.span_id
            return (
              <li key={i} className="relative pb-3 pl-5 last:pb-0">
                <span
                  className={`absolute top-1.5 -left-[5px] size-2.5 rounded-full ring-4 ring-surface ${severityBg[severityName(l.severity_number)]}`}
                />
                <span className="block text-xs text-muted tabular-nums">
                  +{formatDuration(nsSince(trace.start, l.ts))}
                </span>
                <span className={`block text-sm ${mine ? 'font-medium' : 'text-muted'}`}>
                  {l.body}
                </span>
              </li>
            )
          })}
        </ol>
      </section>
      {span.events?.map((e, i) => (
        <section key={i} className="mt-6">
          <h3 className="text-sm font-semibold">Stack trace</h3>
          <pre className="mt-2 overflow-x-auto rounded-xl bg-raised p-3.5 font-mono text-[12px] leading-5 text-muted">
            {String(e.attributes?.['exception.stacktrace'] ?? '')}
          </pre>
        </section>
      ))}
      <Fields title="Attributes" rows={entries(span.attributes)} />
      <Fields title="Resource" rows={entries(span.resource)} />
    </Drawer>
  )
}
