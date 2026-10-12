// Variant C chrome: a left sidebar on wide screens, a slim top bar on phones,
// and a drawer for details.
import type { ReactNode } from 'react'
import { useTheme } from '../../theme'
import { session } from '../fixtures'
import { icons } from '../shared'

export function Shell({ active, children }: { active: 'logs' | 'traces'; children: ReactNode }) {
  return (
    <div className="proto-c min-h-dvh bg-canvas font-sans text-fg md:grid md:grid-cols-[232px_1fr]">
      <Sidebar active={active} />
      <header className="sticky top-0 z-10 flex h-14 items-center gap-2 border-b border-line bg-surface/95 px-3 backdrop-blur md:hidden">
        <button aria-label="Menu" className="grid size-10 place-items-center rounded-lg text-muted">
          <icons.menu size={20} />
        </button>
        <span className="font-semibold">{active === 'logs' ? 'Logs' : 'Traces'}</span>
        <span className="text-sm text-muted">in {session.projects[0].name}</span>
        <span className="ml-auto size-2.5 rounded-full bg-ok" title="Server healthy" />
      </header>
      <main className="min-w-0">{children}</main>
    </div>
  )
}

function Sidebar({ active }: { active: 'logs' | 'traces' }) {
  const [theme, setTheme] = useTheme()
  return (
    <aside className="sticky top-0 hidden h-dvh flex-col border-r border-line bg-surface px-3 py-4 md:flex">
      <span className="px-2 text-[17px] font-semibold tracking-tight">Tracelet</span>
      <button className="mt-5 flex items-center gap-2.5 rounded-xl border border-line bg-canvas px-2.5 py-2 text-left hover:border-subtle">
        <span className="grid size-8 place-items-center rounded-lg bg-accent text-sm font-semibold text-accent-fg">
          Q
        </span>
        <span className="min-w-0 flex-1">
          <span className="block text-sm font-medium">{session.projects[0].name}</span>
          <span className="block text-xs text-muted">Switch project</span>
        </span>
        <icons.chevronDown size={14} className="text-muted" />
      </button>
      <nav className="mt-5 space-y-0.5">
        <NavItem label="Logs" icon={<icons.logs size={17} />} on={active === 'logs'} />
        <NavItem label="Traces" icon={<icons.trace size={17} />} on={active === 'traces'} />
      </nav>
      <div className="mt-auto space-y-1 border-t border-line pt-3 text-sm">
        <p className="flex items-center gap-2 px-2.5 py-1.5 text-muted">
          <span className="size-2 rounded-full bg-ok" />
          Server healthy
        </p>
        <button
          onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')}
          className="flex w-full items-center gap-2 rounded-lg px-2.5 py-1.5 text-muted hover:bg-raised hover:text-fg"
        >
          {theme === 'dark' ? <icons.sun size={16} /> : <icons.moon size={16} />}
          {theme === 'dark' ? 'Light mode' : 'Dark mode'}
        </button>
        <button className="flex w-full items-center gap-2 rounded-lg px-2.5 py-1.5 text-muted hover:bg-raised hover:text-fg">
          <icons.signOut size={16} />
          Sign out of {session.token_name}
        </button>
      </div>
    </aside>
  )
}

function NavItem({ label, icon, on }: { label: string; icon: ReactNode; on: boolean }) {
  return (
    <a
      href="#"
      className={`flex items-center gap-2.5 rounded-lg px-2.5 py-2 text-sm ${
        on ? 'bg-accent/10 font-medium text-accent' : 'text-muted hover:bg-raised hover:text-fg'
      }`}
    >
      {icon}
      {label}
    </a>
  )
}

export function PageHead({ title, summary }: { title: string; summary: ReactNode }) {
  return (
    <div className="flex flex-wrap items-end justify-between gap-3">
      <div>
        <h1 className="hidden text-2xl font-semibold tracking-tight md:block">{title}</h1>
        <p className="text-sm text-muted md:mt-1">{summary}</p>
      </div>
      <div className="flex items-center gap-2">
        <button className="flex h-10 items-center gap-2 rounded-xl border border-line bg-surface px-3.5 text-sm hover:border-subtle">
          <icons.clock size={16} className="text-muted" />
          Last hour
          <icons.chevronDown size={14} className="text-muted" />
        </button>
        <button
          aria-label="Refresh"
          className="grid size-10 place-items-center rounded-xl border border-line bg-surface text-muted hover:text-fg"
        >
          <icons.refresh size={16} />
        </button>
      </div>
    </div>
  )
}

export function Chip({ children, on }: { children: ReactNode; on?: boolean }) {
  return (
    <button
      className={`flex h-9 shrink-0 items-center gap-1.5 rounded-full border px-3.5 text-sm ${
        on
          ? 'border-transparent bg-accent/12 text-accent'
          : 'border-line bg-surface text-muted hover:text-fg'
      }`}
    >
      {children}
    </button>
  )
}

export function Drawer({
  title,
  onClose,
  children,
}: {
  title: ReactNode
  onClose: () => void
  children: ReactNode
}) {
  return (
    <div className="fixed inset-0 z-20 flex justify-end">
      <button aria-label="Close" onClick={onClose} className="absolute inset-0 bg-fg/10" />
      <section className="relative flex h-full w-full max-w-[560px] flex-col bg-surface shadow-2xl md:rounded-l-2xl">
        <div className="flex items-center gap-3 border-b border-line px-5 py-3.5">
          <div className="min-w-0 flex-1">{title}</div>
          <button
            aria-label="Close"
            onClick={onClose}
            className="grid size-9 place-items-center rounded-lg text-muted hover:bg-raised"
          >
            <icons.close size={18} />
          </button>
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-5">{children}</div>
      </section>
    </div>
  )
}

export function Fields({ title, rows }: { title: string; rows: [string, string][] }) {
  if (rows.length === 0) return null
  return (
    <section className="mt-6">
      <h3 className="text-sm font-semibold">{title}</h3>
      <dl className="mt-2 space-y-2">
        {rows.map(([k, v]) => (
          <div key={k}>
            <dt className="text-xs text-muted">{k}</dt>
            <dd className="break-all font-mono text-[13px]">{v}</dd>
          </div>
        ))}
      </dl>
    </section>
  )
}
