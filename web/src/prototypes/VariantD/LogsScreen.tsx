// Variant D logs: full-width one-line rows under a full-width filter bar; the
// selected record opens in a pane beside the list, which only narrows.
import { useState } from 'react'
import type { Log } from '../../api'
import { formatClock, formatTimestamp, severityName } from '../../format'
import { Section } from '../VariantB/LogsScreen'
import { Header, TabBar } from '../VariantB/Chrome'
import '../VariantB/tokens.css'
import { logsPage } from '../fixtures'
import { entries, icons, severityBg, severityTone } from '../shared'
import { Filter, FilterBar, Pane, useListKeys } from './Chrome'
import './tokens.css'

/** Prototype only: wide screens open on the error record unless ?pane=closed. */
function initialSelection(): number | null {
  if (window.innerWidth < 768 || new URLSearchParams(location.search).get('pane') === 'closed')
    return null
  return 1
}

export function LogsScreen() {
  const logs = logsPage.items
  const [selected, setSelected] = useState<number | null>(initialSelection)
  useListKeys(logs.length, selected, setSelected)
  const open = selected !== null

  return (
    <div className="proto-b proto-d flex h-dvh flex-col bg-canvas font-sans text-fg">
      <Header active="logs" />
      <FilterBar
        placeholder="Search messages"
        summary="40 records from 13:20 to 14:20, newest first"
      >
        <Filter label="Service" value="All" />
        <Filter label="Environment" value="production" on />
        <Filter label="Level" value="info and up" on />
      </FilterBar>
      <div className="flex min-h-0 flex-1">
        <section className="min-w-0 flex-1 overflow-y-auto bg-surface pb-16 md:pb-0">
          {logs.map((l, i) => (
            <Row
              key={i}
              log={l}
              on={i === selected}
              narrow={open}
              onSelect={() => setSelected(i)}
            />
          ))}
          <div className="p-3">
            <button className="h-9 w-full rounded-md border border-line text-sm text-muted hover:bg-raised hover:text-fg">
              Load older
            </button>
          </div>
        </section>
        <Pane open={open} onClose={() => setSelected(null)} back="Logs">
          {selected !== null && <Detail log={logs[selected]} />}
        </Pane>
      </div>
      <TabBar active="logs" />
    </div>
  )
}

function Row({
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
  const sev = severityName(log.severity_number)
  const loud = sev === 'warn' || sev === 'error' || sev === 'fatal'
  return (
    <button
      onClick={onSelect}
      className={`grid w-full grid-cols-[3px_1fr] border-b border-line/60 text-left ${on ? 'bg-raised' : 'hover:bg-raised/60'}`}
    >
      <span className={on ? 'bg-accent' : loud ? severityBg[sev] : ''} />
      <span
        className={`grid min-w-0 items-baseline gap-x-3 px-3 py-1.5 md:h-[30px] md:items-center md:py-0 ${
          narrow
            ? 'grid-cols-[auto_1fr] md:grid-cols-[88px_44px_1fr]'
            : 'grid-cols-[auto_auto_1fr] md:grid-cols-[88px_44px_136px_1fr_16px]'
        }`}
      >
        <span className="font-mono text-[12px] text-muted tabular-nums">{formatClock(log.ts)}</span>
        <span className={`font-mono text-[12px] ${severityTone[sev]}`}>{sev}</span>
        <span className={`truncate text-[12.5px] text-muted ${narrow ? 'md:hidden' : ''}`}>
          {log.service}
        </span>
        <span
          className={`col-span-full line-clamp-2 font-mono text-[12.5px] md:col-span-1 md:truncate ${loud && sev !== 'warn' ? 'text-sev-error' : ''}`}
        >
          {log.body}
        </span>
        {!narrow && (
          <span className="hidden text-subtle md:block">
            {log.trace_id ? <icons.trace size={14} aria-label="Has a trace" /> : null}
          </span>
        )}
      </span>
    </button>
  )
}

function Detail({ log }: { log: Log }) {
  const sev = severityName(log.severity_number)
  const stack = log.attributes?.['exception.stacktrace']
  return (
    <div className="px-4 py-5 md:px-6">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-muted">
        <span
          className={`rounded-full bg-current/10 px-2 py-0.5 text-xs font-medium ${severityTone[sev]}`}
        >
          {sev}
        </span>
        <span>{formatTimestamp(log.ts)}</span>
        <span>{log.service}</span>
      </div>
      <h1 className="mt-3 whitespace-pre-wrap break-words font-mono text-[15px] leading-6 font-medium md:text-[16px] md:leading-7">
        {log.body}
      </h1>
      {typeof stack === 'string' && (
        <pre className="mt-4 overflow-x-auto rounded-lg bg-raised p-3.5 font-mono text-[12px] leading-5 text-muted">
          {stack}
        </pre>
      )}
      <div className="mt-5 flex flex-wrap gap-2">
        {log.trace_id && (
          <a
            href="#"
            className="inline-flex h-9 items-center gap-2 rounded-md bg-accent px-3.5 text-sm font-medium text-accent-fg"
          >
            <icons.trace size={15} />
            Open trace
          </a>
        )}
        <button className="inline-flex h-9 items-center gap-2 rounded-md border border-line px-3.5 text-sm hover:bg-raised">
          <icons.copy size={15} />
          Copy as JSON
        </button>
      </div>
      <Section
        title="Details"
        rows={[
          ['Environment', log.environment ?? ''],
          ['Version', log.version ?? ''],
          ['Severity number', String(log.severity_number)],
          ['Observed', log.observed_ts ? formatTimestamp(log.observed_ts) : ''],
          ['Trace ID', log.trace_id ?? ''],
          ['Span ID', log.span_id ?? ''],
          ['Scope', log.scope ?? ''],
        ].filter((r): r is [string, string] => r[1] !== '')}
      />
      <Section
        title="Attributes"
        rows={entries(log.attributes).filter(([k]) => k !== 'exception.stacktrace')}
        mono
      />
      <Section title="Resource" rows={entries(log.resource)} mono />
    </div>
  )
}
