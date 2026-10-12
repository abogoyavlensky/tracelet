// The metric catalog, one metric per row, each a link to its chart.
import { Link } from '@tanstack/react-router'
import type { MetricInfo } from '../api'
import { formatShort } from '../format'
import { kindLabel } from '../metrics'
import type { MetricSearch } from '../search'

export function MetricList({
  metrics,
  carry,
}: {
  metrics: MetricInfo[]
  /** Search params the chart page keeps: project, range, service, environment. */
  carry: MetricSearch
}) {
  return (
    <ol>
      {metrics.map((m) => (
        <li key={`${m.name}-${m.type}-${m.monotonic}-${m.unit ?? ''}`}>
          <Link
            to="/metrics/$name"
            params={{ name: m.name }}
            search={carry}
            className="grid min-w-0 grid-cols-[1fr_auto] items-center gap-x-3 gap-y-1 border-b border-line/60 px-3 py-2 hover:bg-raised/60 md:h-10 md:grid-cols-[minmax(0,1fr)_96px_96px_minmax(0,22%)_72px_120px] md:px-5 md:py-0"
          >
            <span className="truncate font-mono text-[13px]">{m.name}</span>
            <span className="justify-self-end md:justify-self-start">
              <span className="rounded-full bg-raised px-2 py-0.5 text-[11.5px] text-muted">
                {kindLabel(m)}
              </span>
            </span>
            <span className="hidden truncate font-mono text-[12px] text-muted md:inline">
              {m.unit || '–'}
            </span>
            <span className="truncate text-[12.5px] text-muted">{m.services.join(', ')}</span>
            <span className="text-right text-[12px] text-muted tabular-nums md:text-left">
              {m.series} series
            </span>
            <span className="hidden text-right text-[12px] text-subtle tabular-nums md:inline">
              {formatShort(m.last_ts)}
            </span>
          </Link>
        </li>
      ))}
    </ol>
  )
}
