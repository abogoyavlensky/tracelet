// Presets, or an absolute range in the browser's zone.
import { useState } from 'react'
import type { Range } from '../api'
import { fromLocalInput, presets, rangeLabel, type TimeRange, toLocalInput } from '../timeRange'
import { icons } from './icons'
import { Popover } from './Popover'

export const defaultRangeLabel = 'Last hour'

export function TimeRangePicker({
  value,
  resolved,
  onChange,
  compact,
}: {
  value: TimeRange
  /** The absolute window the last search covered, to prefill a custom range. */
  resolved?: Range
  onChange: (range: TimeRange) => void
  compact?: boolean
}) {
  const label = rangeLabel(value, defaultRangeLabel)
  return (
    <Popover
      align="end"
      title="Time range"
      className={`flex items-center gap-1.5 rounded-md border border-line text-sm hover:bg-raised ${compact ? 'h-10 px-3' : 'h-9 px-3'}`}
      label={
        <>
          <icons.clock size={15} className="text-muted" />
          <span className={compact ? 'sr-only' : ''}>{label}</span>
          {compact && (
            <span>
              {'last' in value
                ? (presets.find((p) => p.last === value.last)?.label ?? value.last)
                : 'from' in value
                  ? 'Custom'
                  : '1h'}
            </span>
          )}
          {!compact && <icons.chevronDown size={14} className="text-muted" />}
        </>
      }
      panelClassName="w-80"
    >
      {(close) => (
        <Panel value={value} resolved={resolved} onChange={(r) => (onChange(r), close())} />
      )}
    </Popover>
  )
}

function Panel({
  value,
  resolved,
  onChange,
}: {
  value: TimeRange
  resolved?: Range
  onChange: (r: TimeRange) => void
}) {
  const start = 'from' in value ? value : resolved
  const [from, setFrom] = useState(start ? toLocalInput(start.from) : '')
  const [to, setTo] = useState(start ? toLocalInput(start.to) : '')
  const [error, setError] = useState('')
  const current = 'last' in value ? value.last : 'from' in value ? '' : '1h'

  const apply = () => {
    const f = fromLocalInput(from)
    const t = fromLocalInput(to)
    if (!f || !t) return setError('Enter both times.')
    if (Date.parse(f) >= Date.parse(t)) return setError('The start must be before the end.')
    onChange({ from: f, to: t })
  }

  return (
    <div>
      <div className="grid grid-cols-1 gap-0.5">
        {presets.map((p) => (
          <button
            key={p.last}
            type="button"
            onClick={() => onChange({ last: p.last })}
            className={`rounded-md px-2.5 py-1.5 text-left text-sm ${current === p.last ? 'bg-accent/10 font-medium text-accent' : 'hover:bg-raised'}`}
          >
            {p.name}
          </button>
        ))}
      </div>
      <div className="mt-3 border-t border-line pt-3">
        <p className="text-xs font-medium text-muted">Custom range</p>
        <label className="mt-2 block text-xs text-muted">
          From
          <input
            type="datetime-local"
            value={from}
            onChange={(e) => setFrom(e.target.value)}
            className="mt-0.5 block h-9 w-full min-w-0 appearance-none rounded-md border border-line bg-surface px-2 text-sm text-fg"
          />
        </label>
        <label className="mt-2 block text-xs text-muted">
          To
          <input
            type="datetime-local"
            value={to}
            onChange={(e) => setTo(e.target.value)}
            className="mt-0.5 block h-9 w-full min-w-0 appearance-none rounded-md border border-line bg-surface px-2 text-sm text-fg"
          />
        </label>
        {error && <p className="mt-2 text-xs text-error">{error}</p>}
        <button
          type="button"
          onClick={apply}
          className="mt-3 h-9 w-full rounded-md bg-accent text-sm font-medium text-accent-fg"
        >
          Apply range
        </button>
      </div>
    </div>
  )
}
