// Variant B chrome: a header holding navigation and the time range, so the
// panes below can use the full height.
import { useTheme } from '../../theme'
import { session } from '../fixtures'
import { icons } from '../shared'

export function Header({ active }: { active: 'logs' | 'traces' }) {
  const [theme, setTheme] = useTheme()
  return (
    <header className="flex h-14 shrink-0 items-center gap-3 border-b border-line bg-surface px-3 md:gap-4 md:px-5">
      <span className="flex items-center gap-2">
        <Mark />
        <span className="hidden text-[15px] font-semibold sm:inline">Tracelet</span>
      </span>
      <button className="flex items-center gap-1.5 rounded-md border border-line px-2.5 py-1.5 text-sm hover:bg-raised">
        {session.projects[0].name}
        <icons.chevronDown size={14} className="text-muted" />
      </button>
      <nav className="hidden rounded-lg bg-raised p-0.5 md:flex">
        <Segment label="Logs" on={active === 'logs'} />
        <Segment label="Traces" on={active === 'traces'} />
      </nav>
      <div className="ml-auto flex items-center gap-1.5 md:gap-2">
        <button className="hidden h-9 items-center gap-2 rounded-md border border-line px-3 text-sm hover:bg-raised md:flex">
          <icons.clock size={15} className="text-muted" />
          Last hour
          <icons.chevronDown size={14} className="text-muted" />
        </button>
        <button
          aria-label="Refresh"
          className="hidden size-9 place-items-center rounded-md text-muted hover:bg-raised hover:text-fg md:grid"
        >
          <icons.refresh size={16} />
        </button>
        <span className="mx-1 hidden h-6 w-px bg-line md:block" />
        <span title="Server healthy" className="grid size-9 place-items-center">
          <span className="size-2.5 rounded-full bg-ok ring-4 ring-ok/15" />
        </span>
        <button
          aria-label="Toggle dark mode"
          className="grid size-9 place-items-center rounded-md text-muted hover:bg-raised hover:text-fg"
          onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')}
        >
          {theme === 'dark' ? <icons.sun /> : <icons.moon />}
        </button>
        <button className="flex h-9 items-center gap-2 rounded-md px-2 text-sm text-muted hover:bg-raised hover:text-fg">
          <span className="grid size-6 place-items-center rounded-full bg-accent text-[11px] font-semibold text-accent-fg">
            L
          </span>
          <span className="hidden lg:inline">{session.token_name}</span>
        </button>
      </div>
    </header>
  )
}

/** A small waterfall glyph: the product's mark. */
function Mark() {
  return (
    <svg width="22" height="22" viewBox="0 0 22 22" aria-hidden="true">
      <rect x="2" y="4" width="11" height="3" rx="1.5" className="fill-accent" />
      <rect x="6" y="9.5" width="12" height="3" rx="1.5" className="fill-accent opacity-70" />
      <rect x="10" y="15" width="8" height="3" rx="1.5" className="fill-accent opacity-45" />
    </svg>
  )
}

function Segment({ label, on }: { label: string; on: boolean }) {
  return (
    <a
      href="#"
      className={`rounded-md px-3.5 py-1 text-sm ${on ? 'bg-surface font-medium text-fg shadow-sm' : 'text-muted hover:text-fg'}`}
    >
      {label}
    </a>
  )
}

/** Phone navigation, pinned to the bottom like a native tab bar. */
export function TabBar({ active }: { active: 'logs' | 'traces' }) {
  return (
    <nav className="fixed inset-x-0 bottom-0 z-10 grid grid-cols-2 border-t border-line bg-surface pb-[env(safe-area-inset-bottom)] md:hidden">
      {(['logs', 'traces'] as const).map((t) => {
        const Icon = t === 'logs' ? icons.logs : icons.trace
        return (
          <a
            key={t}
            href="#"
            className={`flex h-14 flex-col items-center justify-center gap-0.5 text-[11px] ${active === t ? 'text-accent' : 'text-muted'}`}
          >
            <Icon size={18} />
            {t === 'logs' ? 'Logs' : 'Traces'}
          </a>
        )
      })}
    </nav>
  )
}

export function Pill({ k, v, on }: { k: string; v: string; on?: boolean }) {
  return (
    <button
      className={`flex h-7 shrink-0 items-center gap-1 rounded-full border px-2.5 text-xs ${
        on ? 'border-accent/40 bg-accent/10 text-fg' : 'border-line text-muted hover:text-fg'
      }`}
    >
      <span className={on ? 'text-muted' : ''}>{k}</span>
      {v && <span className="font-medium">{v}</span>}
    </button>
  )
}
