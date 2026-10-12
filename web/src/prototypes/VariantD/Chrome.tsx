// Variant D chrome: B's header, a full-width filter bar, and a detail pane
// that opens beside the list on selection.
import { type ReactNode, useEffect } from 'react'
import { icons } from '../shared'

/** One filter: a muted label and its value, opening a menu. */
export function Filter({ label, value, on }: { label: string; value: string; on?: boolean }) {
  return (
    <button
      className={`flex h-8 shrink-0 items-center gap-1.5 rounded-md border px-2.5 text-[13px] ${
        on ? 'border-accent/35 bg-accent/8' : 'border-line hover:border-subtle'
      }`}
    >
      <span className="text-muted">{label}</span>
      <span className="font-medium">{value}</span>
      <icons.chevronDown size={13} className="text-subtle" />
    </button>
  )
}

export function FilterBar({
  children,
  placeholder,
  summary,
}: {
  children: ReactNode
  placeholder: string
  summary: string
}) {
  return (
    <div className="shrink-0 border-b border-line bg-surface">
      <div className="hidden items-center gap-2 px-5 py-2.5 md:flex">
        {children}
        <label className="flex h-8 min-w-48 max-w-xl flex-1 items-center gap-2 rounded-md bg-raised px-2.5 focus-within:ring-2 focus-within:ring-accent/40">
          <icons.search size={15} className="text-subtle" />
          <input
            className="w-full bg-transparent text-[13px] outline-none placeholder:text-subtle"
            placeholder={placeholder}
          />
        </label>
        <button className="flex h-8 items-center gap-1 rounded-md px-2 text-[13px] text-muted hover:bg-raised hover:text-fg">
          <icons.plus size={14} />
          Attribute
        </button>
        <span className="ml-auto shrink-0 text-xs text-muted">{summary}</span>
      </div>
      <div className="flex items-center gap-2 px-3 py-2 md:hidden">
        <label className="flex h-10 min-w-0 flex-1 items-center gap-2 rounded-md bg-raised px-2.5">
          <icons.search size={16} className="text-subtle" />
          <input
            className="w-full min-w-0 bg-transparent text-sm outline-none placeholder:text-subtle"
            placeholder={placeholder}
          />
        </label>
        <button className="flex h-10 items-center gap-1.5 rounded-md border border-line px-3 text-sm">
          <icons.filter size={15} />2
        </button>
        <button className="flex h-10 items-center gap-1.5 rounded-md border border-line px-3 text-sm">
          <icons.clock size={15} className="text-muted" />
          1h
        </button>
      </div>
    </div>
  )
}

/**
 * The detail pane: beside the list from md up, a full screen with a back
 * button on phones. Esc closes it.
 */
export function Pane({
  open,
  onClose,
  back,
  children,
}: {
  open: boolean
  onClose: () => void
  back: string
  children: ReactNode
}) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])
  if (!open) return null
  return (
    <aside className="fixed inset-0 top-14 z-10 overflow-y-auto bg-surface pb-16 md:static md:inset-auto md:z-auto md:w-[44%] md:max-w-[760px] md:min-w-[420px] md:shrink-0 md:border-l md:border-line md:pb-0">
      <div className="sticky top-0 z-10 flex h-11 items-center border-b border-line bg-surface/95 px-3 backdrop-blur md:px-5">
        <button onClick={onClose} className="flex items-center gap-1 text-sm text-accent md:hidden">
          <icons.chevronLeft size={18} />
          {back}
        </button>
        <span className="hidden text-xs text-muted md:inline">
          <Key>j</Key> <Key>k</Key> to move, <Key>Esc</Key> to close
        </span>
        <button
          aria-label="Close"
          onClick={onClose}
          className="ml-auto hidden size-8 place-items-center rounded-md text-muted hover:bg-raised hover:text-fg md:grid"
        >
          <icons.close size={16} />
        </button>
      </div>
      {children}
    </aside>
  )
}

function Key({ children }: { children: ReactNode }) {
  return (
    <kbd className="rounded border border-line bg-raised px-1 font-mono text-[11px]">
      {children}
    </kbd>
  )
}

/** j and k move the selection through a list of n items. */
export function useListKeys(n: number, selected: number | null, select: (i: number) => void) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.target instanceof HTMLInputElement) return
      if (e.key === 'j') select(Math.min(n - 1, (selected ?? -1) + 1))
      if (e.key === 'k') select(Math.max(0, (selected ?? 1) - 1))
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [n, selected, select])
}
