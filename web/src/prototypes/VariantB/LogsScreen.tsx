// Variant B logs: a list on the left, the selected record in a reading pane.
import { useState } from 'react'
import type { Log } from '../../api'
import { formatClock, formatTimestamp, severityName } from '../../format'
import { logsPage } from '../fixtures'
import { entries, icons, severityBg, severityTone } from '../shared'
import { Header, Pill, TabBar } from './Chrome'
import './tokens.css'

export function LogsScreen() {
  const [selected, setSelected] = useState(1)
  // Phones show the list or the record, never both.
  const [reading, setReading] = useState(false)
  const logs = logsPage.items
  const log = logs[selected]

  return (
    <div className="proto-b flex h-dvh flex-col bg-canvas font-sans text-fg">
      <Header active="logs" />
      <div className="grid min-h-0 flex-1 grid-cols-1 md:grid-cols-[minmax(360px,36%)_1fr]">
        <aside
          className={`min-h-0 min-w-0 flex-col border-r border-line bg-surface md:flex ${reading ? 'hidden' : 'flex'}`}
        >
          <div className="space-y-2 border-b border-line p-3">
            <label className="flex h-9 items-center gap-2 rounded-md bg-raised px-2.5 focus-within:ring-2 focus-within:ring-accent/40">
              <icons.search size={15} className="text-subtle" />
              <input
                className="w-full bg-transparent text-sm outline-none placeholder:text-subtle"
                placeholder="Search messages"
              />
            </label>
            <div className="-mx-3 flex gap-1.5 overflow-x-auto px-3">
              <Pill k="Level" v="info and up" on />
              <Pill k="Environment" v="production" on />
              <Pill k="Service" v="" />
              <Pill k="+ Attribute" v="" />
            </div>
          </div>
          <p className="border-b border-line px-4 py-1.5 text-xs text-muted">
            40 records from 13:20 to 14:20
          </p>
          <ul className="min-h-0 flex-1 overflow-y-auto pb-16 md:pb-0">
            {logs.map((l, i) => (
              <Item
                key={i}
                log={l}
                on={i === selected}
                onSelect={() => {
                  setSelected(i)
                  setReading(true)
                }}
              />
            ))}
            <li className="p-3">
              <button className="h-9 w-full rounded-md border border-line text-sm text-muted hover:bg-raised hover:text-fg">
                Load older
              </button>
            </li>
          </ul>
        </aside>
        <Reader log={log} reading={reading} onBack={() => setReading(false)} />
      </div>
      <TabBar active="logs" />
    </div>
  )
}

function Item({ log, on, onSelect }: { log: Log; on: boolean; onSelect: () => void }) {
  const sev = severityName(log.severity_number)
  return (
    <li>
      <button
        onClick={onSelect}
        className={`block w-full border-b border-line/70 px-4 py-2.5 text-left ${
          on ? 'bg-raised shadow-[inset_3px_0_0_var(--accent)]' : 'hover:bg-raised/60'
        }`}
      >
        <span className="flex items-center gap-2 text-xs">
          <span className={`size-2 rounded-full ${severityBg[sev]}`} />
          <span className="truncate text-muted">{log.service}</span>
          {log.trace_id && (
            <icons.trace size={13} className="shrink-0 text-subtle" aria-label="Has a trace" />
          )}
          <span className="ml-auto shrink-0 font-mono text-muted tabular-nums">
            {formatClock(log.ts)}
          </span>
        </span>
        <span
          className={`mt-1 line-clamp-2 font-mono text-[13px] leading-5 ${sev === 'error' || sev === 'fatal' ? 'text-sev-error' : ''}`}
        >
          {log.body}
        </span>
      </button>
    </li>
  )
}

function Reader({ log, reading, onBack }: { log: Log; reading: boolean; onBack: () => void }) {
  const sev = severityName(log.severity_number)
  const stack = log.attributes?.['exception.stacktrace']
  return (
    <article
      className={`min-h-0 overflow-y-auto bg-surface pb-16 md:block md:pb-0 ${reading ? 'block' : 'hidden'}`}
    >
      <button
        onClick={onBack}
        className="flex h-11 items-center gap-1 px-3 text-sm text-accent md:hidden"
      >
        <icons.chevronLeft size={18} />
        Logs
      </button>
      <div className="mx-auto max-w-4xl px-4 pb-7 md:px-8 md:pt-7">
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-muted">
          <span
            className={`rounded-full bg-current/10 px-2 py-0.5 text-xs font-medium ${severityTone[sev]}`}
          >
            {sev}
          </span>
          <span>{formatTimestamp(log.ts)}</span>
          <span>{log.service}</span>
        </div>
        <h1 className="mt-3 whitespace-pre-wrap break-words font-mono text-[15px] leading-6 font-medium md:text-[17px] md:leading-7">
          {log.body}
        </h1>
        {typeof stack === 'string' && (
          <pre className="mt-4 overflow-x-auto rounded-lg bg-raised p-4 font-mono text-[12.5px] leading-5 text-muted">
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
    </article>
  )
}

export function Section({
  title,
  rows,
  mono,
}: {
  title: string
  rows: [string, string][]
  mono?: boolean
}) {
  if (rows.length === 0) return null
  return (
    <section className="mt-7">
      <h2 className="mb-2 text-sm font-semibold">{title}</h2>
      <dl className="divide-y divide-line/70 rounded-lg border border-line">
        {rows.map(([k, v]) => (
          <div
            key={k}
            className="grid grid-cols-[minmax(9rem,32%)_1fr] gap-4 px-3 py-2 text-[13px]"
          >
            <dt className={`truncate text-muted ${mono ? 'font-mono text-[12.5px]' : ''}`}>{k}</dt>
            <dd className="min-w-0 break-all font-mono text-[12.5px]">{v}</dd>
          </div>
        ))}
      </dl>
    </section>
  )
}
