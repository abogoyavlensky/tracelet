// One line per record at full width; the selected record's service moves to
// the detail pane, so the list only narrows when the pane opens.
import { useEffect, useRef } from 'react'
import type { Log } from '../api'
import { formatClock, severityName } from '../format'
import { logKey } from '../live'
import { icons } from './icons'
import { severityBg, severityTone } from './tones'

export function LogList({
  logs,
  selected,
  narrow,
  onSelect,
}: {
  logs: Log[]
  selected: number | null
  narrow: boolean
  onSelect: (i: number) => void
}) {
  return (
    <ol>
      {logs.map((log, i) => (
        <LogRow
          key={logKey(log)}
          log={log}
          on={i === selected}
          narrow={narrow}
          onSelect={() => onSelect(i)}
        />
      ))}
    </ol>
  )
}

function LogRow({
  log,
  on,
  narrow,
  onSelect,
}: {
  log: Log
  on: boolean
  narrow: boolean
  onSelect: () => void
}) {
  const ref = useRef<HTMLButtonElement>(null)
  useEffect(() => {
    if (on) ref.current?.scrollIntoView({ block: 'nearest' })
  }, [on])
  const sev = severityName(log.severity_number)
  const loud = sev === 'warn' || sev === 'error' || sev === 'fatal'
  return (
    <li>
      <button
        ref={ref}
        type="button"
        onClick={onSelect}
        aria-current={on || undefined}
        className={`grid w-full grid-cols-[3px_1fr] border-b border-line/60 text-left ${on ? 'bg-raised' : 'hover:bg-raised/60'}`}
      >
        <span className={on ? 'bg-accent' : loud ? severityBg[sev] : ''} />
        <span
          className={`grid min-w-0 items-baseline gap-x-3 px-3 py-1.5 md:h-[30px] md:items-center md:py-0 ${
            narrow
              ? 'grid-cols-[auto_auto_1fr] md:grid-cols-[88px_44px_1fr]'
              : 'grid-cols-[auto_auto_1fr] md:grid-cols-[88px_44px_136px_1fr_16px]'
          }`}
        >
          <span className="font-mono text-[12px] text-muted tabular-nums">
            {formatClock(log.ts)}
          </span>
          <span className={`font-mono text-[12px] ${severityTone[sev]}`}>{sev}</span>
          <span className={`truncate text-[12.5px] text-muted ${narrow ? 'md:hidden' : ''}`}>
            {log.service}
          </span>
          <span
            className={`col-span-full line-clamp-2 font-mono text-[12.5px] break-all md:col-span-1 md:truncate md:break-normal ${
              sev === 'error' || sev === 'fatal' ? 'text-sev-error' : ''
            }`}
          >
            {log.body}
          </span>
          {!narrow && (
            <span className="hidden text-subtle md:block">
              {log.trace_id ? <icons.trace size={14} aria-label="Part of a trace" /> : null}
            </span>
          )}
        </span>
      </button>
    </li>
  )
}
