// Variant A logs: one line per record, details expanding in place.
import { Fragment, useState } from 'react'
import type { Log } from '../../api'
import { formatClock, formatTimestamp, severityName } from '../../format'
import { logsPage } from '../fixtures'
import { entries, icons, severityBg, severityTone } from '../shared'
import { Field, FilterRow, TopBar } from './Chrome'
import './tokens.css'

export function LogsScreen() {
  const [open, setOpen] = useState<number | null>(1)
  const logs = logsPage.items

  return (
    <div className="proto-a min-h-screen bg-canvas font-sans text-fg">
      <TopBar active="logs" />
      <FilterRow placeholder="body contains…">
        <Field k="service" v="any" />
        <Field k="env" v="production" />
        <Field k="level ≥" v="info" />
      </FilterRow>
      <p className="px-3 py-2 text-xs text-muted md:px-4">
        Showing {logs.length} records from {formatClock(logsPage.range.from).slice(0, 5)} to{' '}
        {formatClock(logsPage.range.to).slice(0, 5)}, newest first.
      </p>
      <div className="border-y border-line bg-surface">
        {logs.map((log, i) => (
          <Fragment key={i}>
            <Row log={log} open={open === i} onToggle={() => setOpen(open === i ? null : i)} />
            {open === i && <Details log={log} />}
          </Fragment>
        ))}
      </div>
      <div className="flex justify-center py-4">
        <button className="h-9 rounded border border-line bg-surface px-4 text-sm hover:border-subtle">
          Load older records
        </button>
      </div>
    </div>
  )
}

function Row({ log, open, onToggle }: { log: Log; open: boolean; onToggle: () => void }) {
  const sev = severityName(log.severity_number)
  return (
    <button
      onClick={onToggle}
      aria-expanded={open}
      className={`group grid w-full grid-cols-[3px_1fr] border-b border-line/70 text-left last:border-b-0 ${
        open ? 'bg-raised' : 'hover:bg-raised/60'
      }`}
    >
      <span
        className={`${severityBg[sev]} ${sev === 'info' || sev === 'debug' || sev === 'trace' ? 'opacity-35' : ''}`}
      />
      <span className="grid min-w-0 grid-cols-[auto_auto_1fr] items-baseline gap-x-3 px-3 py-1.5 md:h-7 md:grid-cols-[88px_44px_128px_1fr_auto] md:items-center md:py-0">
        <span className="font-mono text-[12px] text-muted tabular-nums">{formatClock(log.ts)}</span>
        <span className={`font-mono text-[12px] ${severityTone[sev]}`}>{sev}</span>
        <span className="truncate text-[12.5px] text-muted md:order-none">{log.service}</span>
        <span className="col-span-3 line-clamp-2 font-mono text-[12.5px] md:col-span-1 md:truncate">
          {log.body}
        </span>
        <span className="hidden text-subtle md:block">
          {log.trace_id ? <icons.trace size={14} aria-label="Has a trace" /> : null}
        </span>
      </span>
    </button>
  )
}

function Details({ log }: { log: Log }) {
  const stack = log.attributes?.['exception.stacktrace']
  const attrs = entries(log.attributes).filter(([k]) => k !== 'exception.stacktrace')
  return (
    <div className="border-b border-line bg-raised px-3 pb-4 pt-1 md:pl-[calc(3px+0.75rem+88px+0.75rem)]">
      <pre className="whitespace-pre-wrap break-words font-mono text-[12.5px] leading-5">
        {log.body}
      </pre>
      {typeof stack === 'string' && (
        <pre className="mt-2 overflow-x-auto rounded border border-line bg-surface p-2 font-mono text-[11.5px] leading-[1.15rem] text-muted">
          {stack}
        </pre>
      )}
      <div className="mt-3 grid gap-x-8 gap-y-4 lg:grid-cols-3">
        <KeyValues
          title="Record"
          rows={[
            ['time', formatTimestamp(log.ts)],
            ['severity', `${log.severity_text ?? ''} (${log.severity_number})`],
            ['service', log.service],
            ['environment', log.environment ?? ''],
            ['version', log.version ?? ''],
            ['trace', log.trace_id ?? ''],
            ['span', log.span_id ?? ''],
          ].filter((row): row is [string, string] => row[1] !== '')}
        />
        <KeyValues title="Attributes" rows={attrs} />
        <KeyValues title="Resource" rows={entries(log.resource)} />
      </div>
      {log.trace_id && (
        <a
          href="#"
          className="mt-4 inline-flex h-8 items-center gap-1.5 rounded bg-accent px-3 text-[12.5px] font-medium text-accent-fg"
        >
          <icons.trace size={14} />
          Open trace
        </a>
      )}
    </div>
  )
}

function KeyValues({ title, rows }: { title: string; rows: [string, string][] }) {
  if (rows.length === 0) return null
  return (
    <section className="min-w-0">
      <h3 className="mb-1 text-xs font-medium text-muted">{title}</h3>
      <dl className="grid grid-cols-[minmax(7rem,auto)_1fr] gap-x-3 font-mono text-[12px] leading-5">
        {rows.map(([k, v]) => (
          <div key={k} className="contents">
            <dt className="truncate text-muted">{k}</dt>
            <dd className="break-all">{v}</dd>
          </div>
        ))}
      </dl>
    </section>
  )
}

export { KeyValues }
