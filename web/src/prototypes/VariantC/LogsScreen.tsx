// Variant C logs: roomy rows, severity as a quiet tint, details in a drawer.
import { useState } from 'react'
import type { Log } from '../../api'
import { formatClock, formatTimestamp, severityName } from '../../format'
import { logsPage } from '../fixtures'
import { entries, icons, severityBg, severityTone } from '../shared'
import { Chip, Drawer, Fields, PageHead, Shell } from './Chrome'
import './tokens.css'

const tint: Record<string, string> = {
  warn: 'bg-sev-warn/7',
  error: 'bg-sev-error/7',
  fatal: 'bg-sev-fatal/8',
}

export function LogsScreen() {
  const logs = logsPage.items
  // Wide screens open on a record so the drawer shows; phones open on the list.
  const [open, setOpen] = useState<number | null>(() => (window.innerWidth >= 768 ? 1 : null))

  return (
    <Shell active="logs">
      <div className="px-4 py-5 md:px-8 md:py-7">
        <PageHead title="Logs" summary="The last hour, 13:20 to 14:20, newest first." />
        <div className="-mx-4 mt-5 flex gap-2 overflow-x-auto px-4 md:mx-0 md:flex-wrap md:px-0">
          <label className="flex h-9 min-w-56 flex-1 items-center gap-2 rounded-full border border-line bg-surface px-3.5 focus-within:border-accent md:max-w-sm">
            <icons.search size={16} className="text-subtle" />
            <input
              className="w-full bg-transparent text-sm outline-none placeholder:text-subtle"
              placeholder="Search messages"
            />
          </label>
          <Chip on>Info and up</Chip>
          <Chip on>Production</Chip>
          <Chip>All services</Chip>
          <Chip>
            <icons.plus size={14} />
            Attribute
          </Chip>
        </div>
        <ol className="mt-5 overflow-hidden rounded-2xl border border-line bg-surface">
          {logs.map((l, i) => (
            <Row key={i} log={l} onOpen={() => setOpen(i)} />
          ))}
        </ol>
        <button className="mx-auto mt-5 block h-10 rounded-full border border-line bg-surface px-5 text-sm text-muted hover:text-fg">
          Show older records
        </button>
      </div>
      {open !== null && <LogDrawer log={logs[open]} onClose={() => setOpen(null)} />}
    </Shell>
  )
}

function Row({ log, onOpen }: { log: Log; onOpen: () => void }) {
  const sev = severityName(log.severity_number)
  return (
    <li className={`border-b border-line/60 last:border-b-0 ${tint[sev] ?? ''}`}>
      <button
        onClick={onOpen}
        className="flex w-full gap-4 px-4 py-3 text-left hover:bg-raised/50 md:px-5"
      >
        <span className="hidden w-[76px] shrink-0 pt-0.5 font-mono text-[13px] text-muted tabular-nums md:block">
          {formatClock(log.ts).slice(0, 8)}
        </span>
        <span className="min-w-0 flex-1">
          <span className="block text-[14.5px] leading-6 break-words">{log.body}</span>
          <span className="mt-0.5 flex flex-wrap items-center gap-x-3 text-xs text-muted">
            <span className={`flex items-center gap-1.5 ${severityTone[sev]}`}>
              <span className={`size-1.5 rounded-full ${severityBg[sev]}`} />
              {sev}
            </span>
            <span>{log.service}</span>
            <span className="font-mono tabular-nums md:hidden">
              {formatClock(log.ts).slice(0, 8)}
            </span>
            {log.trace_id && (
              <span className="flex items-center gap-1">
                <icons.trace size={12} />
                in a trace
              </span>
            )}
          </span>
        </span>
      </button>
    </li>
  )
}

function LogDrawer({ log, onClose }: { log: Log; onClose: () => void }) {
  const sev = severityName(log.severity_number)
  const stack = log.attributes?.['exception.stacktrace']
  return (
    <Drawer
      onClose={onClose}
      title={
        <p className="flex items-center gap-2 text-sm">
          <span className={`flex items-center gap-1.5 font-medium ${severityTone[sev]}`}>
            <span className={`size-2 rounded-full ${severityBg[sev]}`} />
            {sev}
          </span>
          <span className="text-muted">{formatTimestamp(log.ts)}</span>
        </p>
      }
    >
      <p className="text-lg leading-7 font-medium">{log.body}</p>
      <p className="mt-1 text-sm text-muted">
        From {log.service} {log.version}, {log.environment}
      </p>
      {log.trace_id && (
        <a
          href="#"
          className="mt-4 inline-flex h-10 items-center gap-2 rounded-full bg-accent px-4 text-sm font-medium text-accent-fg"
        >
          <icons.trace size={16} />
          Open the trace
        </a>
      )}
      {typeof stack === 'string' && (
        <section className="mt-6">
          <h3 className="text-sm font-semibold">Stack trace</h3>
          <pre className="mt-2 overflow-x-auto rounded-xl bg-raised p-3.5 font-mono text-[12px] leading-5 text-muted">
            {stack}
          </pre>
        </section>
      )}
      <Fields
        title="Attributes"
        rows={entries(log.attributes).filter(([k]) => k !== 'exception.stacktrace')}
      />
      <Fields
        title="Record"
        rows={[
          ['Trace ID', log.trace_id ?? ''],
          ['Span ID', log.span_id ?? ''],
          ['Severity', `${log.severity_text} (${log.severity_number})`],
          ['Scope', log.scope ?? ''],
        ].filter((r): r is [string, string] => r[1] !== '')}
      />
      <Fields title="Resource" rows={entries(log.resource)} />
    </Drawer>
  )
}
