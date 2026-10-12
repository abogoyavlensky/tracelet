// Variant A chrome: a thin top bar and a single command-style filter row.
import type { ReactNode } from 'react'
import { useTheme } from '../../theme'
import { session } from '../fixtures'
import { icons } from '../shared'

export function TopBar({ active }: { active: 'logs' | 'traces' }) {
  const [theme, setTheme] = useTheme()
  return (
    <header className="border-b border-line bg-surface">
      <div className="flex h-11 items-center gap-3 px-3 md:gap-5 md:px-4">
        <span className="text-[15px] font-semibold tracking-tight">tracelet</span>
        <button className="flex items-center gap-1 rounded px-2 py-1 text-sm hover:bg-raised">
          <span className="font-medium">{session.projects[0].name}</span>
          <icons.chevronDown size={14} className="text-muted" />
        </button>
        <nav className="hidden h-full items-stretch gap-1 md:flex">
          <Tab label="Logs" on={active === 'logs'} />
          <Tab label="Traces" on={active === 'traces'} />
        </nav>
        <div className="ml-auto flex items-center gap-1 md:gap-3">
          <span className="hidden items-center gap-1.5 text-xs text-muted sm:flex">
            <span className="size-2 rounded-full bg-ok" />
            Healthy
          </span>
          <button
            aria-label="Toggle dark mode"
            className="grid size-9 place-items-center rounded text-muted hover:bg-raised hover:text-fg"
            onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')}
          >
            {theme === 'dark' ? <icons.sun /> : <icons.moon />}
          </button>
          <span className="hidden text-xs text-muted md:inline">
            Signed in as <span className="text-fg">{session.token_name}</span>
          </span>
          <button
            aria-label="Sign out"
            className="grid size-9 place-items-center rounded text-muted hover:bg-raised hover:text-fg"
          >
            <icons.signOut />
          </button>
        </div>
      </div>
      <nav className="flex border-t border-line md:hidden">
        <Tab label="Logs" on={active === 'logs'} wide />
        <Tab label="Traces" on={active === 'traces'} wide />
      </nav>
    </header>
  )
}

function Tab({ label, on, wide }: { label: string; on: boolean; wide?: boolean }) {
  return (
    <a
      href="#"
      className={`flex items-center justify-center border-b-2 px-3 text-sm ${wide ? 'h-10 flex-1' : ''} ${
        on ? 'border-accent font-medium text-fg' : 'border-transparent text-muted hover:text-fg'
      }`}
    >
      {label}
    </a>
  )
}

/** One filter in the command row: a muted key and its value. */
export function Field({ k, v, mono = true }: { k: string; v: string; mono?: boolean }) {
  return (
    <button className="flex h-8 shrink-0 items-center gap-1.5 rounded border border-line bg-surface px-2 text-[12.5px] hover:border-subtle">
      <span className="text-muted">{k}</span>
      <span className={mono ? 'font-mono' : ''}>{v}</span>
      <icons.chevronDown size={12} className="text-subtle" />
    </button>
  )
}

export function FilterRow({
  children,
  placeholder,
  value,
}: {
  children: ReactNode
  placeholder: string
  value?: string
}) {
  return (
    <div className="border-b border-line bg-canvas">
      <div className="hidden items-center gap-2 px-4 py-2 md:flex">
        {children}
        <label className="flex h-8 min-w-48 flex-1 items-center gap-2 rounded border border-line bg-surface px-2 focus-within:border-accent">
          <icons.search size={14} className="text-subtle" />
          <input
            className="w-full bg-transparent font-mono text-[12.5px] outline-none placeholder:text-subtle"
            placeholder={placeholder}
            defaultValue={value}
          />
        </label>
        <button className="flex h-8 items-center gap-1 rounded px-2 text-[12.5px] text-muted hover:bg-raised hover:text-fg">
          <icons.plus size={14} />
          Attribute
        </button>
        <span className="mx-1 h-5 w-px bg-line" />
        <RangeButton />
        <button
          aria-label="Refresh"
          className="grid size-8 place-items-center rounded text-muted hover:bg-raised hover:text-fg"
        >
          <icons.refresh size={15} />
        </button>
      </div>
      <div className="flex items-center gap-2 px-3 py-2 md:hidden">
        <label className="flex h-10 min-w-0 flex-1 items-center gap-2 rounded border border-line bg-surface px-2">
          <icons.search size={15} className="text-subtle" />
          <input
            className="w-full min-w-0 bg-transparent font-mono text-[13px] outline-none placeholder:text-subtle"
            placeholder={placeholder}
            defaultValue={value}
          />
        </label>
        <button className="flex h-10 items-center gap-1.5 rounded border border-line bg-surface px-3 text-sm">
          <icons.filter size={15} />2
        </button>
        <button className="flex h-10 items-center gap-1 rounded border border-line bg-surface px-3 font-mono text-[13px]">
          1h
        </button>
      </div>
    </div>
  )
}

function RangeButton() {
  return (
    <button className="flex h-8 items-center gap-1.5 rounded border border-line bg-surface px-2 text-[12.5px] hover:border-subtle">
      <icons.clock size={14} className="text-muted" />
      Last 1h
      <icons.chevronDown size={12} className="text-subtle" />
    </button>
  )
}
