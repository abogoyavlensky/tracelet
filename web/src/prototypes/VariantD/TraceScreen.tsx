// Variant D trace: A's hairline time grid across the full width; the selected
// span opens in the pane. A trace with errors opens on its first failed span.
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
import { Header, TabBar } from '../VariantB/Chrome'
import { Section } from '../VariantB/LogsScreen'
import '../VariantB/tokens.css'
import { trace, traceLogs } from '../fixtures'
import { entries, icons, seriesBg, severityBg } from '../shared'
import { Pane, useListKeys } from './Chrome'
import './tokens.css'

const ticks = [0, 0.25, 0.5, 0.75, 1]

export function TraceScreen() {
  const rows = useMemo(() => buildWaterfall(trace), [])
  const firstError = rows.findIndex((r) => r.span.status_code === 2)
  const [selected, setSelected] = useState<number | null>(firstError >= 0 ? firstError : null)
  // Phones start on the waterfall; wider screens open the pane on the failure.
  const [paneOpen, setPaneOpen] = useState(() => window.innerWidth >= 768 && firstError >= 0)
  const select = (i: number) => {
    setSelected(i)
    setPaneOpen(true)
  }
  useListKeys(rows.length, selected, select)
  const root = rows[0].span
  const errors = rows.filter((r) => r.span.status_code === 2).length
  const orphans = rows.filter((r) => r.orphan).length
  const colorOf = (service: string) => rows.find((r) => r.span.service === service)?.colorIndex ?? 0
  const open = paneOpen && selected !== null

  return (
    <div className="proto-b proto-d flex h-dvh flex-col bg-canvas font-sans text-fg">
      <Header active="traces" />
      <section className="shrink-0 border-b border-line bg-surface px-4 py-3 md:px-5">
        <div className="flex items-center gap-2 text-sm text-muted">
          <a href="#" className="flex items-center gap-1 hover:text-fg">
            <icons.chevronLeft size={15} />
            Traces
          </a>
          <span className="text-subtle">/</span>
          <button className="flex min-w-0 items-center gap-1 font-mono text-xs hover:text-fg">
            <span className="truncate">{trace.trace_id}</span>
            <icons.copy size={12} className="shrink-0" />
          </button>
          <button className="ml-auto flex shrink-0 items-center gap-1 text-xs hover:text-fg">
            <icons.refresh size={13} />
            Refresh
          </button>
        </div>
        <div className="mt-1.5 flex flex-wrap items-center gap-x-5 gap-y-2">
          <h1 className="font-mono text-[17px] font-semibold">{root.name}</h1>
          <Stat label="Duration" value={formatDuration(trace.duration_ns)} />
          <Stat label="Spans" value={String(trace.spans.length)} />
          <Stat label="Errors" value={String(errors)} tone={errors ? 'text-error' : undefined} />
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
        {orphans > 0 && (
          <p className="mt-2 text-xs text-sev-warn">
            {orphans === 1 ? 'One span is' : `${orphans} spans are`} waiting for a parent that has
            not arrived; refresh to fill the trace in.
          </p>
        )}
      </section>
      <div className="flex min-h-0 flex-1">
        <section className="min-w-0 flex-1 overflow-y-auto bg-surface pb-16 md:pb-0">
          <Waterfall rows={rows} selected={selected} narrow={open} onSelect={select} />
        </section>
        <Pane open={open} onClose={() => setPaneOpen(false)} back="Waterfall">
          {selected !== null && <SpanDetail span={rows[selected].span} />}
        </Pane>
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

function Waterfall({
  rows,
  selected,
  narrow,
  onSelect,
}: {
  rows: WaterfallRow[]
  selected: number | null
  narrow: boolean
  onSelect: (i: number) => void
}) {
  const nameCol = narrow ? 'minmax(200px,40%)' : 'minmax(260px,32%)'
  return (
    <>
      {/* Wide screens: name tree beside a hairline time grid. */}
      <div className="relative hidden md:block">
        <div
          className="sticky top-0 z-[1] grid border-b border-line bg-surface text-[11px] text-muted"
          style={{ gridTemplateColumns: `${nameCol} 1fr` }}
        >
          <span className="px-5 py-1.5">Span</span>
          <span className="relative mr-16">
            {ticks.map((t) => (
              <span
                key={t}
                className="absolute top-1.5 -translate-x-1/2 whitespace-nowrap font-mono tabular-nums first:translate-x-0 last:-translate-x-full"
                style={{ left: `${t * 100}%` }}
              >
                {formatDuration(t * trace.duration_ns)}
              </span>
            ))}
          </span>
        </div>
        <div className="relative">
          <div
            className="pointer-events-none absolute inset-0 grid"
            style={{ gridTemplateColumns: `${nameCol} 1fr` }}
          >
            <span />
            <span className="relative mr-16">
              {ticks.map((t) => (
                <span
                  key={t}
                  className="absolute inset-y-0 w-px bg-line/70"
                  style={{ left: `${t * 100}%` }}
                />
              ))}
            </span>
          </div>
          {rows.map((r, i) => {
            const failed = r.span.status_code === 2
            return (
              <button
                key={r.span.span_id}
                onClick={() => onSelect(i)}
                className={`relative grid h-7 w-full items-center text-left ${i === selected ? 'bg-raised shadow-[inset_3px_0_0_var(--accent)]' : 'hover:bg-raised/60'}`}
                style={{ gridTemplateColumns: `${nameCol} 1fr` }}
              >
                <span
                  className="flex min-w-0 items-center gap-1.5 pr-3 text-[13px]"
                  style={{ paddingLeft: 20 + r.depth * 14 }}
                >
                  <span className={`size-2 shrink-0 rounded-full ${seriesBg[r.colorIndex]}`} />
                  <span className={`truncate ${failed ? 'font-medium text-error' : ''}`}>
                    {r.span.name}
                  </span>
                  {r.orphan && (
                    <span className="shrink-0 text-[11px] text-sev-warn">no parent</span>
                  )}
                </span>
                <span className="relative mr-16 h-full">
                  <span
                    className={`absolute top-1/2 h-2.5 -translate-y-1/2 rounded-sm ${failed ? 'error-hatch' : seriesBg[r.colorIndex]}`}
                    style={{ left: `${r.offsetPct}%`, width: `${r.widthPct}%` }}
                  />
                  <span
                    className="absolute top-1/2 -translate-y-1/2 whitespace-nowrap pl-1.5 font-mono text-[11px] text-muted tabular-nums"
                    style={{ left: `${r.offsetPct + r.widthPct}%` }}
                  >
                    {formatDuration(r.span.duration_ns)}
                  </span>
                </span>
              </button>
            )
          })}
        </div>
      </div>
      {/* Phones: the name line above a full-width track. */}
      <ul className="md:hidden">
        {rows.map((r, i) => {
          const failed = r.span.status_code === 2
          return (
            <li key={r.span.span_id}>
              <button
                onClick={() => onSelect(i)}
                className={`w-full border-b border-line/50 px-3 py-2 text-left ${i === selected ? 'bg-raised shadow-[inset_3px_0_0_var(--accent)]' : ''}`}
              >
                <span
                  className="flex items-baseline gap-2 text-[13px]"
                  style={{ paddingLeft: r.depth * 10 }}
                >
                  <span className={`truncate ${failed ? 'font-medium text-error' : ''}`}>
                    {r.span.name}
                  </span>
                  <span className="ml-auto shrink-0 font-mono text-[11px] text-muted tabular-nums">
                    {formatDuration(r.span.duration_ns)}
                  </span>
                </span>
                <span className="relative mt-1.5 block h-1.5 rounded-full bg-raised">
                  <span
                    className={`absolute inset-y-0 rounded-full ${failed ? 'error-hatch' : seriesBg[r.colorIndex]}`}
                    style={{ left: `${r.offsetPct}%`, width: `${r.widthPct}%` }}
                  />
                </span>
              </button>
            </li>
          )
        })}
      </ul>
    </>
  )
}

function SpanDetail({ span }: { span: Span }) {
  const [onlySpan, setOnlySpan] = useState(false)
  const failed = span.status_code === 2
  const logs = onlySpan ? traceLogs.filter((l) => l.span_id === span.span_id) : traceLogs
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
      <h2 className="mt-2 break-words font-mono text-[16px] font-medium">{span.name}</h2>
      <p className="mt-1 text-sm text-muted">
        {formatDuration(span.duration_ns)}, starting {formatDuration(nsSince(trace.start, span.ts))}{' '}
        into the trace at {formatTimestamp(span.ts)}
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
            <li key={i} className="flex items-start gap-2 px-3 py-2">
              <span
                className={`mt-1.5 size-2 shrink-0 rounded-full ${severityBg[severityName(l.severity_number)]}`}
              />
              <span
                className={`min-w-0 flex-1 font-mono text-[12.5px] ${l.span_id === span.span_id ? 'font-semibold' : ''}`}
              >
                {l.body}
              </span>
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
  )
}
