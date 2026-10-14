// The detail pane: beside the list from md up, a full screen with a back
// button on phones. Esc closes it; j and k move the list's selection.
import { type ReactNode, useEffect } from 'react'
import { icons } from './icons'

export function Pane({
  onClose,
  back,
  children,
}: {
  onClose: () => void
  back: string
  children: ReactNode
}) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !isTyping(e)) onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])
  return (
    <aside className="fixed inset-x-0 top-14 bottom-0 z-20 overflow-y-auto bg-surface pb-tabbar md:static md:z-auto md:w-[44%] md:max-w-[760px] md:min-w-[420px] md:shrink-0 md:border-l md:border-line md:pb-0">
      <div className="sticky top-0 z-10 flex h-11 items-center border-b border-line bg-surface/95 px-3 backdrop-blur md:px-5">
        <button
          type="button"
          onClick={onClose}
          className="flex h-10 items-center gap-1 text-sm text-accent md:hidden"
        >
          <icons.chevronLeft size={18} />
          {back}
        </button>
        <span className="hidden text-xs text-muted md:inline">
          <Key>j</Key> <Key>k</Key> to move, <Key>Esc</Key> to close
        </span>
        <button
          type="button"
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

function isTyping(e: KeyboardEvent): boolean {
  const t = e.target
  return (
    t instanceof HTMLInputElement ||
    t instanceof HTMLTextAreaElement ||
    t instanceof HTMLSelectElement
  )
}

/** j and k move the selection through n items; nothing selected starts at the top. */
export function useListKeys(n: number, selected: number | null, select: (i: number) => void) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (isTyping(e) || e.metaKey || e.ctrlKey || e.altKey || n === 0) return
      if (e.key === 'j') select(selected === null ? 0 : Math.min(n - 1, selected + 1))
      if (e.key === 'k') select(selected === null ? 0 : Math.max(0, selected - 1))
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [n, selected, select])
}
