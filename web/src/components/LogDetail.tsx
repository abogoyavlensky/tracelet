// A log record in the detail pane: the message large, then its context.
import { Link } from '@tanstack/react-router'
import { useState } from 'react'
import type { Log } from '../api'
import { entries } from '../attributes'
import { formatTimestamp, severityName } from '../format'
import { rangeAround } from '../timeRange'
import { icons } from './icons'
import { KeyValues } from './KeyValues'
import { severityTone } from './tones'

export function LogDetail({ log, project }: { log: Log; project: string | undefined }) {
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
      <h1 className="mt-3 font-mono text-[15px] leading-6 font-medium break-words whitespace-pre-wrap md:text-[16px] md:leading-7">
        {log.body}
      </h1>
      {typeof stack === 'string' && (
        <pre className="mt-4 overflow-x-auto rounded-lg bg-raised p-3.5 font-mono text-[12px] leading-5 text-muted">
          {stack}
        </pre>
      )}
      <div className="mt-5 flex flex-wrap gap-2">
        {log.trace_id && (
          <Link
            to="/traces/$traceId"
            params={{ traceId: log.trace_id }}
            search={{ project, ...rangeAround(log.ts), span: log.span_id }}
            className="inline-flex h-9 items-center gap-2 rounded-md bg-accent px-3.5 text-sm font-medium text-accent-fg"
          >
            <icons.trace size={15} />
            Open trace
          </Link>
        )}
        <CopyButton text={JSON.stringify(log, null, 2)} />
      </div>
      <KeyValues
        title="Details"
        rows={(
          [
            ['Environment', log.environment ?? ''],
            ['Version', log.version ?? ''],
            ['Severity', `${log.severity_text || sev} (${log.severity_number})`],
            ['Observed', log.observed_ts ? formatTimestamp(log.observed_ts) : ''],
            ['Trace ID', log.trace_id ?? ''],
            ['Span ID', log.span_id ?? ''],
            ['Scope', log.scope ?? ''],
          ] as [string, string][]
        ).filter(([, v]) => v !== '')}
      />
      <KeyValues
        title="Attributes"
        rows={entries(log.attributes).filter(([k]) => k !== 'exception.stacktrace')}
        monoKeys
      />
      <KeyValues title="Resource" rows={entries(log.resource)} monoKeys />
    </div>
  )
}

export function CopyButton({ text, label = 'Copy as JSON' }: { text: string; label?: string }) {
  const [state, setState] = useState<'idle' | 'copied' | 'failed'>('idle')
  return (
    <button
      type="button"
      onClick={() => {
        void copyText(text).then((ok) => {
          setState(ok ? 'copied' : 'failed')
          setTimeout(() => setState('idle'), 2000)
        })
      }}
      className="inline-flex h-9 items-center gap-2 rounded-md border border-line px-3.5 text-sm hover:bg-raised"
    >
      <icons.copy size={15} />
      {state === 'copied'
        ? 'Copied'
        : state === 'failed'
          ? 'Copy failed: select and copy by hand'
          : label}
    </button>
  )
}

/**
 * Copies text. The Clipboard API needs a secure context (HTTPS or
 * localhost), so plain HTTP on a LAN address falls back to a hidden textarea.
 */
async function copyText(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {
    // Permission denied; try the fallback.
  }
  const area = document.createElement('textarea')
  area.value = text
  area.setAttribute('readonly', '')
  area.style.position = 'fixed'
  area.style.opacity = '0'
  document.body.append(area)
  area.select()
  try {
    return document.execCommand('copy')
  } catch {
    return false
  } finally {
    area.remove()
  }
}
