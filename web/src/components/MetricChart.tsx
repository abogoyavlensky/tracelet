// A uPlot time series in the semantic colour tokens. uPlot draws on a
// canvas, so colours are read from the CSS variables and the chart is
// rebuilt when the theme changes. Lines never cross a step without data.
import { useEffect, useRef, useState } from 'react'
import uPlot from 'uplot'
import 'uplot/dist/uPlot.min.css'
import { axisTime, type ChartData, hasIsolatedValue } from '../metrics'

export const seriesColorCount = 8

type Colors = { line: string; muted: string; series: string[] }

function readColors(): Colors {
  const style = getComputedStyle(document.documentElement)
  const token = (name: string) => style.getPropertyValue(name).trim()
  return {
    line: token('--line'),
    muted: token('--muted'),
    series: Array.from({ length: seriesColorCount }, (_, i) => token(`--series-${i}`)),
  }
}

/** The token colours, read again whenever the dark class on <html> changes. */
function useThemeColors(): Colors {
  const [colors, setColors] = useState(readColors)
  useEffect(() => {
    const observer = new MutationObserver(() => setColors(readColors()))
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
    return () => observer.disconnect()
  }, [])
  return colors
}

const separator = '\u0000'

/**
 * X-axis tick spacings in seconds, from a minute to a year, so a long custom
 * range on a narrow screen still finds a spacing wide enough for labels.
 */
const day = 86400
const timeIncrs = [
  60,
  120,
  300,
  600,
  900,
  1800,
  3600,
  7200,
  10800,
  21600,
  43200,
  day,
  2 * day,
  7 * day,
  14 * day,
  30 * day,
  60 * day,
  90 * day,
  180 * day,
  365 * day,
]

export function MetricChart({
  data,
  labels,
  hidden,
  formatY,
  onCursor,
  height,
}: {
  data: ChartData
  labels: string[]
  hidden: ReadonlySet<number>
  formatY: (value: number) => string
  onCursor: (index: number | null) => void
  height: number
}) {
  const container = useRef<HTMLDivElement>(null)
  const plot = useRef<uPlot | null>(null)
  const colors = useThemeColors()
  // Data, callbacks, and visibility reach the chart through this ref, so a
  // render rebuilds the chart only when its series, height, or theme change.
  const latest = useRef({ data, onCursor, formatY, hidden })
  useEffect(() => {
    latest.current = { data, onCursor, formatY, hidden }
  })
  const shape = labels.join(separator)

  useEffect(() => {
    const el = container.current
    if (!el) return
    const names = shape === '' ? [] : shape.split(separator)
    const initial = latest.current.data
    const axis = {
      stroke: colors.muted,
      grid: { stroke: colors.line, width: 1 },
      ticks: { stroke: colors.line, width: 1 },
    }
    const opts: uPlot.Options = {
      width: el.clientWidth,
      height,
      legend: { show: false },
      cursor: { points: { size: 7 } },
      scales: { x: { time: true } },
      axes: [
        {
          ...axis,
          // Labels show minutes, so ticks are never closer than a minute.
          incrs: timeIncrs,
          // The span comes from the data drawn now, which setData replaces.
          values: (u, splits) => {
            const xs = u.data[0]
            const span = xs.length > 1 ? xs[xs.length - 1] - xs[0] : 0
            return splits.map((s) => axisTime(s, span))
          },
        },
        {
          ...axis,
          values: (_u, splits) => splits.map((s) => latest.current.formatY(s)),
          // Wide enough for the longest tick label, about 7 px a character.
          size: (_u, values) =>
            Math.max(40, ...(values ?? []).map((v) => String(v).length * 7 + 18)),
        },
      ],
      series: [
        {},
        ...names.map((label, i) => ({
          label,
          stroke: colors.series[i % seriesColorCount],
          width: 1.5,
          spanGaps: false,
          show: !latest.current.hidden.has(i),
          // Decided per draw, so new data with a lone value still shows it.
          points: {
            show: (u: uPlot, si: number) =>
              hasIsolatedValue((u.data[si] ?? []) as (number | null)[]),
          },
        })),
      ],
      hooks: {
        setCursor: [
          (u) => {
            const idx = u.cursor.idx
            latest.current.onCursor(idx === null || idx === undefined ? null : idx)
          },
        ],
      },
    }
    const u = new uPlot(opts, initial as uPlot.AlignedData, el)
    plot.current = u
    const resize = new ResizeObserver(() => u.setSize({ width: el.clientWidth, height }))
    resize.observe(el)
    return () => {
      resize.disconnect()
      u.destroy()
      plot.current = null
    }
  }, [shape, height, colors])

  useEffect(() => {
    plot.current?.setData(data as uPlot.AlignedData)
  }, [data])

  useEffect(() => {
    const u = plot.current
    if (!u) return
    for (let i = 1; i < u.series.length; i++) {
      const show = !hidden.has(i - 1)
      if (u.series[i].show !== show) u.setSeries(i, { show })
    }
  }, [hidden])

  return <div ref={container} className="w-full" onMouseLeave={() => onCursor(null)} />
}
