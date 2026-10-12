// Pieces every design variant uses: icons, attribute flattening, and local
// selection state. Styling stays in each variant.
import type { SVGProps } from 'react'
import type { Attributes } from '../api'

type IconProps = SVGProps<SVGSVGElement> & { size?: number }

function Icon({ size = 16, children, ...rest }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.8}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      {...rest}
    >
      {children}
    </svg>
  )
}

export const icons = {
  search: (p: IconProps) => (
    <Icon {...p}>
      <circle cx="11" cy="11" r="7" />
      <path d="m20 20-3.5-3.5" />
    </Icon>
  ),
  chevronDown: (p: IconProps) => (
    <Icon {...p}>
      <path d="m6 9 6 6 6-6" />
    </Icon>
  ),
  chevronRight: (p: IconProps) => (
    <Icon {...p}>
      <path d="m9 6 6 6-6 6" />
    </Icon>
  ),
  chevronLeft: (p: IconProps) => (
    <Icon {...p}>
      <path d="m15 6-6 6 6 6" />
    </Icon>
  ),
  refresh: (p: IconProps) => (
    <Icon {...p}>
      <path d="M20 11a8 8 0 1 0-2.3 5.7" />
      <path d="M20 4v7h-7" />
    </Icon>
  ),
  moon: (p: IconProps) => (
    <Icon {...p}>
      <path d="M20 14.5A8 8 0 0 1 9.5 4a8 8 0 1 0 10.5 10.5Z" />
    </Icon>
  ),
  sun: (p: IconProps) => (
    <Icon {...p}>
      <circle cx="12" cy="12" r="4" />
      <path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" />
    </Icon>
  ),
  filter: (p: IconProps) => (
    <Icon {...p}>
      <path d="M4 5h16M7 12h10M10 19h4" />
    </Icon>
  ),
  close: (p: IconProps) => (
    <Icon {...p}>
      <path d="M6 6l12 12M18 6 6 18" />
    </Icon>
  ),
  copy: (p: IconProps) => (
    <Icon {...p}>
      <rect x="9" y="9" width="11" height="11" rx="2" />
      <path d="M5 15V5a1 1 0 0 1 1-1h10" />
    </Icon>
  ),
  trace: (p: IconProps) => (
    <Icon {...p}>
      <path d="M3 6h7M8 12h8M14 18h7" />
    </Icon>
  ),
  logs: (p: IconProps) => (
    <Icon {...p}>
      <path d="M5 6h14M5 12h14M5 18h9" />
    </Icon>
  ),
  plus: (p: IconProps) => (
    <Icon {...p}>
      <path d="M12 5v14M5 12h14" />
    </Icon>
  ),
  clock: (p: IconProps) => (
    <Icon {...p}>
      <circle cx="12" cy="12" r="8" />
      <path d="M12 8v4l3 2" />
    </Icon>
  ),
  menu: (p: IconProps) => (
    <Icon {...p}>
      <path d="M4 7h16M4 12h16M4 17h16" />
    </Icon>
  ),
  signOut: (p: IconProps) => (
    <Icon {...p}>
      <path d="M15 4h3a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2h-3M10 16l4-4-4-4M14 12H4" />
    </Icon>
  ),
}

/** Attributes as sorted [key, display value] pairs; nested values become JSON. */
export function entries(attrs: Attributes | undefined): [string, string][] {
  if (!attrs) return []
  return Object.entries(attrs)
    .map(([k, v]): [string, string] => [k, typeof v === 'string' ? v : JSON.stringify(v)])
    .toSorted(([a], [b]) => a.localeCompare(b))
}

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
