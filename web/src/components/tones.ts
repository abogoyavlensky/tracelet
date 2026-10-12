// Tailwind classes per severity and per service colour slot, written out in
// full so Tailwind finds them.
export const severityTone: Record<string, string> = {
  unset: 'text-subtle',
  trace: 'text-sev-trace',
  debug: 'text-sev-debug',
  info: 'text-sev-info',
  warn: 'text-sev-warn',
  error: 'text-sev-error',
  fatal: 'text-sev-fatal',
}

export const severityBg: Record<string, string> = {
  unset: 'bg-subtle',
  trace: 'bg-sev-trace',
  debug: 'bg-sev-debug',
  info: 'bg-sev-info',
  warn: 'bg-sev-warn',
  error: 'bg-sev-error',
  fatal: 'bg-sev-fatal',
}

export const seriesBg = [
  'bg-series-0',
  'bg-series-1',
  'bg-series-2',
  'bg-series-3',
  'bg-series-4',
  'bg-series-5',
  'bg-series-6',
  'bg-series-7',
]

export const seriesText = [
  'text-series-0',
  'text-series-1',
  'text-series-2',
  'text-series-3',
  'text-series-4',
  'text-series-5',
  'text-series-6',
  'text-series-7',
]
