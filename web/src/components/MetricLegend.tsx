// The chart's legend: one row per series with its colour, label, and value
// at the cursor (or its last value). Clicking a row hides or shows it.
import { seriesBg } from './tones'
import { seriesColorCount } from './MetricChart'

export type LegendItem = { label: string; value: string }

export function MetricLegend({
  items,
  hidden,
  onToggle,
}: {
  items: LegendItem[]
  hidden: ReadonlySet<number>
  onToggle: (index: number) => void
}) {
  if (items.length < 2 && !hidden.size) {
    const only = items[0]
    return only ? (
      <p className="px-1 text-[13px] text-muted">
        <span className="font-mono text-fg tabular-nums">{only.value}</span>
      </p>
    ) : null
  }
  return (
    <ul className="grid gap-x-6 gap-y-0.5 sm:grid-cols-2 xl:grid-cols-3">
      {items.map((item, i) => {
        const off = hidden.has(i)
        return (
          <li key={`${i}-${item.label}`}>
            <button
              type="button"
              aria-pressed={!off}
              title={off ? 'Show this series' : 'Hide this series'}
              onClick={() => onToggle(i)}
              className={`flex h-8 w-full min-w-0 items-center gap-2 rounded-md px-2 text-left text-[13px] hover:bg-raised ${off ? 'opacity-45' : ''}`}
            >
              <span
                className={`h-0.5 w-3.5 shrink-0 rounded-full ${seriesBg[i % seriesColorCount]}`}
              />
              <span className="min-w-0 flex-1 truncate">{item.label}</span>
              <span className="shrink-0 font-mono text-[12px] text-muted tabular-nums">
                {item.value}
              </span>
            </button>
          </li>
        )
      })}
    </ul>
  )
}
