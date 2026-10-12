// Turns a trace's flat span list into waterfall rows: nested by parent,
// depth-first in display order, with each bar's offset and width as a
// percentage of the trace.
import type { Span, Trace } from './api'
import { nsSince } from './format'

export type WaterfallRow = {
  span: Span
  depth: number
  offsetPct: number
  widthPct: number
  /** The span names a parent that is not in the trace (not yet exported, or truncated). */
  orphan: boolean
  colorIndex: number
  hasChildren: boolean
}

export const minWidthPct = 0.5
export const seriesCount = 8

/** A stable colour slot per service name, the same on every page. */
export function serviceColorIndex(service: string): number {
  let h = 5381
  for (let i = 0; i < service.length; i++) {
    h = ((h << 5) + h + service.charCodeAt(i)) | 0
  }
  return Math.abs(h) % seriesCount
}

function byStart(a: Span, b: Span): number {
  const d = nsSince(b.ts, a.ts)
  if (d !== 0) return d
  return a.span_id < b.span_id ? -1 : a.span_id > b.span_id ? 1 : 0
}

export function buildWaterfall(trace: Trace): WaterfallRow[] {
  const ids = new Set(trace.spans.map((s) => s.span_id))
  const children = new Map<string, Span[]>()
  const roots: Span[] = []
  for (const s of trace.spans) {
    const parent = s.parent_span_id
    if (parent && ids.has(parent)) {
      children.set(parent, [...(children.get(parent) ?? []), s])
    } else {
      roots.push(s)
    }
  }

  const rows: WaterfallRow[] = []
  const visited = new Set<string>()
  const total = trace.duration_ns

  const visit = (s: Span, depth: number, orphan: boolean) => {
    if (visited.has(s.span_id)) return
    visited.add(s.span_id)
    const kids = (children.get(s.span_id) ?? [])
      .filter((k) => !visited.has(k.span_id))
      .toSorted(byStart)
    rows.push({
      span: s,
      depth,
      orphan,
      colorIndex: serviceColorIndex(s.service),
      hasChildren: kids.length > 0,
      ...bar(s),
    })
    for (const k of kids) visit(k, depth + 1, false)
  }

  const bar = (s: Span) => {
    if (total <= 0) return { offsetPct: 0, widthPct: minWidthPct }
    const widthPct = Math.min(100, Math.max(minWidthPct, (s.duration_ns / total) * 100))
    const offsetPct = Math.min(
      100 - widthPct,
      Math.max(0, (nsSince(trace.start, s.ts) / total) * 100),
    )
    return { offsetPct, widthPct }
  }

  for (const r of roots.toSorted(byStart)) visit(r, 0, !!r.parent_span_id)
  // Spans caught in a parent cycle are never reached from a root; show them as orphans.
  for (const s of trace.spans.toSorted(byStart)) visit(s, 0, true)
  return rows
}
