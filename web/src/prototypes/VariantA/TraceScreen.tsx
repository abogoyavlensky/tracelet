// Variant A trace: a hairline time grid is the one bold element; everything
// around it stays dense and quiet.
import { useMemo, useState } from 'react'
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
import { entries, icons, seriesBg, severityTone } from '../shared'
import { TopBar } from './Chrome'
import { KeyValues } from './LogsScreen'
import './tokens.css'

const ticks = [0, 0.25, 0.5, 0.75, 1]

export function TraceScreen() {
  const rows = useMemo(() => buildWaterfall(trace), [])
  const [selected, setSelected] = useState(errorSpanId)
  const [onlySpan, setOnlySpan] = useState(false)
  const span = rows.find((r) => r.span.span_id === selected)?.span ?? rows[0].span
  const root = rows[0].span
  const orphans = rows.filter((r) => r.orphan).length
  const logs = onlySpan ? traceLogs.filter((l) => l.span_id === span.span_id) : traceLogs

  return (
    <div className="proto-a min-h-screen bg-canvas font-sans text-fg">
      <TopBar active="traces" />
      <section className="border-b border-line bg-surface px-3 py-3 md:px-4">
        <a href="#" className="inline-flex items-center gap-1 text-xs text-muted hover:text-fg">
          <icons.chevronLeft size={13} />
          Traces
        </a>
        <div className="mt-1 flex flex-wrap items-baseline gap-x-4 gap-y-1">
          <h1 className="font-mono text-[15px] font-semibold">{root.name}</h1>
          <span className="font-mono text-[15px] tabular-nums">
            {formatDuration(trace.duration_ns)}
          </span>
        </div>
        <div className="mt-1.5 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted">
          <button className="flex items-center gap-1 font-mono hover:text-fg">
            {trace.trace_id}
            <icons.copy size={12} />
          </button>
          <span>{formatTimestamp(trace.start)}</span>
          <span>{trace.spans.length} spans</span>
          <span className="flex flex-wrap items-center gap-3">
            {trace.services.map((s) => (
              <span key={s} className="flex items-center gap-1">
                <span
                  className={`size-2 rounded-[2px] ${seriesBg[rows.find((r) => r.span.service === s)?.colorIndex ?? 0]}`}
                />
                {s}
              </span>
            ))}
          </span>
          <button className="ml-auto flex items-center gap-1 hover:text-fg">
            <icons.refresh size={13} />
            Refresh
          </button>
        </div>
        {orphans > 0 && (
          <p className="mt-2 text-xs text-sev-warn">
            {orphans} span{orphans > 1 ? 's name a parent' : ' names a parent'} that is not here
            yet; the trace may still be arriving.
          </p>
        )}
      </section>

      <Waterfall rows={rows} selected={span.span_id} onSelect={setSelected} />

      <section className="grid gap-px border-t border-line bg-line lg:grid-cols-2">
        <div className="bg-surface px-3 py-3 md:px-4">
          <div className="flex flex-wrap items-baseline gap-x-3">
            <h2 className="font-mono text-[13px] font-semibold">{span.name}</h2>
            <span className="text-xs text-muted">
              {span.service}, {spanKindName(span.kind)}
            </span>
            <span className={`text-xs ${span.status_code === 2 ? 'text-error' : 'text-muted'}`}>
              {spanStatusName(span.status_code)}
            </span>
          </div>
          {span.status_message && (
            <p className="mt-1 font-mono text-[12px] text-error">{span.status_message}</p>
          )}
          <div className="mt-3 space-y-4">
            <KeyValues
              title="Timing"
              rows={[
                ['starts at', `+${formatDuration(nsSince(trace.start, span.ts))}`],
                ['duration', formatDuration(span.duration_ns)],
                ['span', span.span_id],
                ['parent', span.parent_span_id ?? 'none (root)'],
              ]}
            />
            <KeyValues title="Attributes" rows={entries(span.attributes)} />
            <KeyValues title="Resource" rows={entries(span.resource)} />
          </div>
        </div>
        <div className="bg-surface px-3 py-3 md:px-4">
          {span.events?.map((e, i) => (
            <div key={i} className="mb-4">
              <h3 className="text-xs font-medium text-muted">
                Event <span className="font-mono text-fg">{e.name}</span> at +
                {formatDuration(nsSince(span.ts, e.ts ?? span.ts))}
              </h3>
              <p className="mt-1 font-mono text-[12px] text-error">
                {String(e.attributes?.['exception.message'] ?? '')}
              </p>
              <pre className="mt-1 overflow-x-auto rounded border border-line bg-raised p-2 font-mono text-[11.5px] leading-[1.15rem] text-muted">
                {String(e.attributes?.['exception.stacktrace'] ?? '')}
              </pre>
            </div>
          ))}
          <div className="flex items-center justify-between">
            <h3 className="text-xs font-medium text-muted">Logs in this trace</h3>
            <label className="flex items-center gap-1.5 text-xs text-muted">
              <input
                type="checkbox"
                checked={onlySpan}
                onChange={(e) => setOnlySpan(e.target.checked)}
                className="accent-[var(--accent)]"
              />
              Only this span
            </label>
          </div>
          <ul className="mt-1 divide-y divide-line/70 border-y border-line/70">
            {logs.map((l, i) => {
              const sev = severityName(l.severity_number)
              return (
                <li
                  key={i}
                  className="grid grid-cols-[72px_40px_1fr] items-baseline gap-x-2 py-1 font-mono text-[12px]"
                >
                  <span className="text-muted tabular-nums">
                    +{formatDuration(nsSince(trace.start, l.ts))}
                  </span>
                  <span className={severityTone[sev]}>{sev}</span>
                  <span
                    className={`min-w-0 truncate ${l.span_id === span.span_id ? 'font-semibold' : ''}`}
                  >
                    {l.body}
                  </span>
                </li>
              )
            })}
            {logs.length === 0 && (
              <li className="py-2 text-xs text-muted">No logs for this span.</li>
            )}
          </ul>
          <p className="mt-2 text-xs text-subtle">
            Logs from {formatClock(trace.start)} to a second past the trace end.
          </p>
        </div>
      </section>
    </div>
  )
}

function Waterfall({
  rows,
  selected,
  onSelect,
}: {
  rows: WaterfallRow[]
  selected: string
  onSelect: (id: string) => void
}) {
  return (
    <section className="bg-surface">
      {/* Desktop: name tree beside a time grid. */}
      <div className="hidden md:block">
        <div className="grid grid-cols-[minmax(260px,34%)_1fr] border-b border-line text-[11px] text-muted">
          <span className="px-4 py-1.5">Span</span>
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
          <div className="pointer-events-none absolute inset-y-0 left-[max(260px,34%)] right-16">
            {ticks.map((t) => (
              <span
                key={t}
                className="absolute inset-y-0 w-px bg-line/70"
                style={{ left: `${t * 100}%` }}
              />
            ))}
          </div>
          {rows.map((r) => (
            <button
              key={r.span.span_id}
              onClick={() => onSelect(r.span.span_id)}
              className={`relative grid h-6 w-full grid-cols-[minmax(260px,34%)_1fr] items-center text-left ${
                r.span.span_id === selected
                  ? 'bg-raised shadow-[inset_2px_0_0_var(--accent)]'
                  : 'hover:bg-raised/60'
              }`}
            >
              <span
                className="flex min-w-0 items-center gap-1.5 pr-3 font-mono text-[12px]"
                style={{ paddingLeft: 16 + r.depth * 14 }}
              >
                <span className={`size-2 shrink-0 rounded-[2px] ${seriesBg[r.colorIndex]}`} />
                <span className={`truncate ${r.span.status_code === 2 ? 'text-error' : ''}`}>
                  {r.span.name}
                </span>
                {r.orphan && <span className="shrink-0 text-[11px] text-sev-warn">orphan</span>}
              </span>
              <span className="relative mr-16 h-full">
                <span
                  className={`absolute top-1/2 h-2 -translate-y-1/2 rounded-[2px] ${r.span.status_code === 2 ? 'error-hatch' : seriesBg[r.colorIndex]}`}
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
          ))}
        </div>
      </div>
      {/* Phone: the name line above a full-width track. */}
      <ul className="md:hidden">
        {rows.map((r) => (
          <li key={r.span.span_id}>
            <button
              onClick={() => onSelect(r.span.span_id)}
              className={`w-full px-3 py-1.5 text-left ${r.span.span_id === selected ? 'bg-raised shadow-[inset_2px_0_0_var(--accent)]' : ''}`}
            >
              <span
                className="flex items-baseline gap-2 font-mono text-[12px]"
                style={{ paddingLeft: r.depth * 10 }}
              >
                <span className={`truncate ${r.span.status_code === 2 ? 'text-error' : ''}`}>
                  {r.span.name}
                </span>
                <span className="ml-auto shrink-0 text-[11px] text-muted tabular-nums">
                  {formatDuration(r.span.duration_ns)}
                </span>
              </span>
              <span className="relative mt-1 block h-1.5 rounded-full bg-raised">
                <span
                  className={`absolute inset-y-0 rounded-full ${r.span.status_code === 2 ? 'error-hatch' : seriesBg[r.colorIndex]}`}
                  style={{ left: `${r.offsetPct}%`, width: `${r.widthPct}%` }}
                />
              </span>
            </button>
          </li>
        ))}
      </ul>
    </section>
  )
}
