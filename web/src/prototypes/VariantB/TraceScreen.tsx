// Variant B trace: the waterfall on the left, the selected span read on the right.
import { useMemo, useState } from 'react'
import type { Span } from '../../api'
import {
  formatClock,
  formatDuration,
  formatTimestamp,
  nsSince,
  severityName,
  spanKindName,
  spanStatusName,
} from '../../format'
import { buildWaterfall, type WaterfallRow } from '../../waterfall'
import { errorSpanId, trace, traceLogs } from '../fixtures'
import { entries, icons, seriesBg, severityBg } from '../shared'
import { Header, TabBar } from './Chrome'
import { Section } from './LogsScreen'
import './tokens.css'

export function TraceScreen() {
  const rows = useMemo(() => buildWaterfall(trace), [])
  const [selected, setSelected] = useState(errorSpanId)
  const span = rows.find((r) => r.span.span_id === selected)?.span ?? rows[0].span
  const root = rows[0].span
  const colorOf = (service: string) => rows.find((r) => r.span.service === service)?.colorIndex ?? 0

  return (
    <div className="proto-b flex h-dvh flex-col bg-canvas font-sans text-fg">
      <Header active="traces" />
      <section className="shrink-0 border-b border-line bg-surface px-4 py-3 md:px-5">
        <div className="flex items-center gap-2 text-sm text-muted">
          <a href="#" className="flex items-center gap-1 hover:text-fg">
            <icons.chevronLeft size={15} />
            Traces
          </a>
          <span className="text-subtle">/</span>
          <button className="flex items-center gap-1 font-mono text-xs hover:text-fg">
            {trace.trace_id.slice(0, 16)}…
            <icons.copy size={12} />
          </button>
        </div>
        <div className="mt-1.5 flex flex-wrap items-center gap-x-5 gap-y-2">
          <h1 className="text-lg font-semibold">{root.name}</h1>
          <Stat label="Duration" value={formatDuration(trace.duration_ns)} />
          <Stat label="Spans" value={String(trace.spans.length)} />
          <Stat label="Errors" value="2" tone="text-error" />
          <Stat label="Started" value={formatClock(trace.start)} />
          <div className="flex flex-wrap gap-1.5">
            {trace.services.map((s) => (
              <span
                key={s}
                className="flex items-center gap-1.5 rounded-full border border-line px-2 py-0.5 text-xs"
              >
                <span className={`size-2 rounded-full ${seriesBg[colorOf(s)]}`} />
                {s}
              </span>
            ))}
          </div>
        </div>
      </section>
      <div className="grid min-h-0 flex-1 grid-cols-1 md:grid-cols-[1fr_minmax(380px,40%)]">
        <div className="min-h-0 min-w-0 overflow-y-auto bg-surface pb-16 md:pb-0">
          <div className="flex items-center justify-between border-b border-line px-4 py-1.5 text-xs text-muted">
            <span>1 span is waiting for its parent; refresh to fill the trace in.</span>
            <button className="flex items-center gap-1 hover:text-fg">
              <icons.refresh size={13} />
              Refresh
            </button>
          </div>
          {rows.map((r) => (
            <Row
              key={r.span.span_id}
              row={r}
              on={r.span.span_id === span.span_id}
              onSelect={() => setSelected(r.span.span_id)}
            />
          ))}
        </div>
        <SpanPane span={span} />
      </div>
      <TabBar active="traces" />
    </div>
  )
}

function Stat({ label, value, tone }: { label: string; value: string; tone?: string }) {
  return (
    <span className="flex items-baseline gap-1.5 text-sm">
      <span className="text-muted">{label}</span>
      <span className={`font-mono font-medium tabular-nums ${tone ?? ''}`}>{value}</span>
    </span>
  )
}

function Row({ row: r, on, onSelect }: { row: WaterfallRow; on: boolean; onSelect: () => void }) {
  const failed = r.span.status_code === 2
  return (
    <button
      onClick={onSelect}
      className={`grid w-full grid-cols-1 gap-1 border-b border-line/50 px-4 py-1.5 text-left md:grid-cols-[minmax(220px,40%)_1fr] md:items-center md:gap-3 md:py-1 ${
        on ? 'bg-raised shadow-[inset_3px_0_0_var(--accent)]' : 'hover:bg-raised/60'
      }`}
    >
      <span
        className="flex min-w-0 items-center gap-1.5 text-[13px]"
        style={{ paddingLeft: r.depth * 14 }}
      >
        {r.hasChildren ? (
          <icons.chevronDown size={13} className="shrink-0 text-subtle" />
        ) : (
          <span className="w-[13px] shrink-0" />
        )}
        <span className={`truncate ${failed ? 'font-medium text-error' : ''}`}>{r.span.name}</span>
        <span className="ml-auto shrink-0 font-mono text-[11.5px] text-muted tabular-nums md:hidden">
          {formatDuration(r.span.duration_ns)}
        </span>
      </span>
      <span className="relative h-5 rounded bg-raised/70">
        <span
          className={`absolute inset-y-1 rounded-sm ${failed ? 'bg-error' : seriesBg[r.colorIndex]}`}
          style={{ left: `${r.offsetPct}%`, width: `${r.widthPct}%` }}
        />
        <span
          className={`absolute inset-y-0 hidden items-center whitespace-nowrap px-1.5 font-mono text-[11px] text-muted tabular-nums md:flex ${
            r.offsetPct + r.widthPct > 80 ? '-translate-x-full' : ''
          }`}
          style={{
            left:
              r.offsetPct + r.widthPct > 80 ? `${r.offsetPct}%` : `${r.offsetPct + r.widthPct}%`,
          }}
        >
          {formatDuration(r.span.duration_ns)}
        </span>
      </span>
    </button>
  )
}

function SpanPane({ span }: { span: Span }) {
  const [onlySpan, setOnlySpan] = useState(false)
  const failed = span.status_code === 2
  const logs = onlySpan ? traceLogs.filter((l) => l.span_id === span.span_id) : traceLogs
  return (
    <aside className="hidden min-h-0 overflow-y-auto border-l border-line bg-surface md:block">
      <div className="px-6 py-5">
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
        <h2 className="mt-2 font-mono text-[16px] font-medium">{span.name}</h2>
        <p className="mt-1 text-sm text-muted">
          {formatDuration(span.duration_ns)}, starting{' '}
          {formatDuration(nsSince(trace.start, span.ts))} into the trace at{' '}
          {formatTimestamp(span.ts)}
        </p>
        {span.status_message && (
          <p className="mt-3 rounded-md bg-error/10 px-3 py-2 font-mono text-[13px] text-error">
            {span.status_message}
          </p>
        )}
        {span.events?.map((e, i) => (
          <section key={i} className="mt-5">
            <h3 className="text-sm font-semibold">
              {e.name === 'exception' ? 'Exception' : e.name}
              <span className="ml-2 font-normal text-muted">
                {String(e.attributes?.['exception.type'] ?? '')}
              </span>
            </h3>
            <pre className="mt-2 overflow-x-auto rounded-lg bg-raised p-3 font-mono text-[12px] leading-5 text-muted">
              {String(e.attributes?.['exception.stacktrace'] ?? '')}
            </pre>
          </section>
        ))}
        <section className="mt-7">
          <div className="mb-2 flex items-center justify-between">
            <h3 className="text-sm font-semibold">Logs</h3>
            <div className="flex rounded-md bg-raised p-0.5 text-xs">
              <button
                onClick={() => setOnlySpan(false)}
                className={`rounded px-2 py-0.5 ${!onlySpan ? 'bg-surface shadow-sm' : 'text-muted'}`}
              >
                Whole trace
              </button>
              <button
                onClick={() => setOnlySpan(true)}
                className={`rounded px-2 py-0.5 ${onlySpan ? 'bg-surface shadow-sm' : 'text-muted'}`}
              >
                This span
              </button>
            </div>
          </div>
          <ul className="divide-y divide-line/70 rounded-lg border border-line">
            {logs.map((l, i) => (
              <li key={i} className="flex items-start gap-2 px-3 py-2 text-[13px]">
                <span
                  className={`mt-1.5 size-2 shrink-0 rounded-full ${severityBg[severityName(l.severity_number)]}`}
                />
                <span className="min-w-0 flex-1 font-mono text-[12.5px]">{l.body}</span>
                <span className="shrink-0 font-mono text-xs text-muted tabular-nums">
                  +{formatDuration(nsSince(trace.start, l.ts))}
                </span>
              </li>
            ))}
            {logs.length === 0 && (
              <li className="px-3 py-2 text-sm text-muted">No logs for this span.</li>
            )}
          </ul>
        </section>
        <Section title="Attributes" rows={entries(span.attributes)} mono />
        <Section title="Resource" rows={entries(span.resource)} mono />
      </div>
    </aside>
  )
}
