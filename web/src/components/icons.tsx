// Inline SVG icons (no icon library) and the brand mark.
import type { SVGProps } from 'react'

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
  metrics: (p: IconProps) => (
    <Icon {...p}>
      <path d="M4 4v16h16" />
      <path d="m7 15 4-5 3 3 5-6" />
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
  warning: (p: IconProps) => (
    <Icon {...p}>
      <path d="M12 4 2.5 20h19L12 4Z" />
      <path d="M12 10v4M12 17h.01" />
    </Icon>
  ),
  signOut: (p: IconProps) => (
    <Icon {...p}>
      <path d="M15 4h3a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2h-3M10 16l4-4-4-4M14 12H4" />
    </Icon>
  ),
}

/** The brand mark: three staggered bars, a small waterfall. Also the favicon. */
export function Mark({ size = 22 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 22 22" aria-hidden="true">
      <rect x="2" y="4" width="11" height="3" rx="1.5" className="fill-accent" />
      <rect x="6" y="9.5" width="12" height="3" rx="1.5" className="fill-accent opacity-70" />
      <rect x="10" y="15" width="8" height="3" rx="1.5" className="fill-accent opacity-45" />
    </svg>
  )
}
