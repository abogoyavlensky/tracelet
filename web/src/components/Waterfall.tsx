// The trace waterfall: a name tree beside a hairline time grid on wide
// screens; on phones each span is a name line above a full-width track.
import { useEffect, useRef } from 'react'
import { formatDuration } from '../format'
import { type WaterfallRow, visibleRows } from '../waterfall'
import { icons } from './icons'
import { seriesBg } from './tones'

const ticks = [0, 0.25, 0.5, 0.75, 1]

export function Waterfall({
  rows,
  durationNs,
  selected,
  narrow,
  collapsed,
  onToggle,
  onSelect,
}: {
  rows: WaterfallRow[]
  durationNs: number
  selected: string | undefined
  narrow: boolean
  collapsed: Set<string>
  onToggle: (spanId: string) => void
  onSelect: (spanId: string) => void
}) {
  const nameCol = narrow ? 'minmax(200px,40%)' : 'minmax(260px,32%)'
  return (
    <>
      {/* Wide screens: collapsible name tree beside a hairline time grid. */}
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
                className="absolute top-1.5 -translate-x-1/2 font-mono whitespace-nowrap tabular-nums first:translate-x-0 last:-translate-x-full"
                style={{ left: `${t * 100}%` }}
              >
                {formatDuration(t * durationNs)}
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
          {visibleRows(rows, collapsed).map((r) => (
            <WideRow
              key={r.span.span_id}
              row={r}
              nameCol={nameCol}
              on={r.span.span_id === selected}
              collapsed={collapsed.has(r.span.span_id)}
              onToggle={onToggle}
              onSelect={onSelect}
            />
          ))}
        </div>
      </div>
      {/* Phones list every span: there is no collapse control at this size. */}
      <ul className="md:hidden">
        {rows.map((r) => (
          <PhoneRow
            key={r.span.span_id}
            row={r}
            on={r.span.span_id === selected}
            onSelect={onSelect}
          />
        ))}
      </ul>
    </>
  )
}

function useScrollIntoView(on: boolean) {
  const ref = useRef<HTMLDivElement & HTMLButtonElement>(null)
  useEffect(() => {
    if (on) ref.current?.scrollIntoView({ block: 'nearest' })
  }, [on])
  return ref
}

function WideRow({
  row: r,
  nameCol,
  on,
  collapsed,
  onToggle,
  onSelect,
}: {
  row: WaterfallRow
  nameCol: string
  on: boolean
  collapsed: boolean
  onToggle: (spanId: string) => void
  onSelect: (spanId: string) => void
}) {
  const ref = useScrollIntoView(on)
  const failed = r.span.status_code === 2
  return (
    <div
      ref={ref}
      className={`relative grid h-7 w-full items-center ${on ? 'bg-raised shadow-[inset_3px_0_0_var(--accent)]' : 'hover:bg-raised/60'}`}
      style={{ gridTemplateColumns: `${nameCol} 1fr` }}
    >
      <span
        className="flex min-w-0 items-center gap-1 pr-3 text-[13px]"
        style={{ paddingLeft: 8 + r.depth * 14 }}
      >
        {r.hasChildren ? (
          <button
            type="button"
            aria-label={collapsed ? `Expand ${r.span.name}` : `Collapse ${r.span.name}`}
            aria-expanded={!collapsed}
            onClick={() => onToggle(r.span.span_id)}
            className="grid size-5 shrink-0 place-items-center rounded text-subtle hover:bg-line/60 hover:text-fg"
          >
            {collapsed ? <icons.chevronRight size={13} /> : <icons.chevronDown size={13} />}
          </button>
        ) : (
          <span className="w-5 shrink-0" />
        )}
        <button
          type="button"
          onClick={() => onSelect(r.span.span_id)}
          className="flex min-w-0 flex-1 items-center gap-1.5 text-left"
        >
          <span className={`size-2 shrink-0 rounded-full ${seriesBg[r.colorIndex]}`} />
          <span className={`truncate ${failed ? 'font-medium text-error' : ''}`}>
            {r.span.name}
          </span>
          {r.orphan && <span className="shrink-0 text-[11px] text-sev-warn">no parent</span>}
        </button>
      </span>
      <button
        type="button"
        tabIndex={-1}
        aria-hidden="true"
        onClick={() => onSelect(r.span.span_id)}
        className="relative mr-16 h-full"
      >
        <span
          className={`absolute top-1/2 h-2.5 -translate-y-1/2 rounded-sm ${failed ? 'error-hatch' : seriesBg[r.colorIndex]}`}
          style={{ left: `${r.offsetPct}%`, width: `${r.widthPct}%` }}
        />
        <span
          className="absolute top-1/2 -translate-y-1/2 pl-1.5 font-mono text-[11px] whitespace-nowrap text-muted tabular-nums"
          style={{ left: `${r.offsetPct + r.widthPct}%` }}
        >
          {formatDuration(r.span.duration_ns)}
        </span>
      </button>
    </div>
  )
}

function PhoneRow({
  row: r,
  on,
  onSelect,
}: {
  row: WaterfallRow
  on: boolean
  onSelect: (spanId: string) => void
}) {
  const ref = useScrollIntoView(on)
  const failed = r.span.status_code === 2
  return (
    <li>
      <button
        ref={ref}
        type="button"
        onClick={() => onSelect(r.span.span_id)}
        className={`w-full border-b border-line/50 px-3 py-2 text-left ${on ? 'bg-raised shadow-[inset_3px_0_0_var(--accent)]' : ''}`}
      >
        <span
          className="flex items-baseline gap-2 text-[13px]"
          style={{ paddingLeft: Math.min(r.depth, 8) * 10 }}
        >
          <span className={`truncate ${failed ? 'font-medium text-error' : ''}`}>
            {r.span.name}
          </span>
          {r.orphan && <span className="shrink-0 text-[11px] text-sev-warn">no parent</span>}
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
}
