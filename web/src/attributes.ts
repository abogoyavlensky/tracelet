import type { Attributes } from './api'

/** Attributes as sorted [key, display value] pairs; nested values become JSON. */
export function entries(attrs: Attributes | undefined): [string, string][] {
  if (!attrs) return []
  return Object.entries(attrs)
    .map(([k, v]): [string, string] => [k, typeof v === 'string' ? v : JSON.stringify(v)])
    .toSorted(([a], [b]) => a.localeCompare(b))
}
