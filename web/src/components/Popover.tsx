// A button that opens a small panel below it. Closes on outside click and Esc.
import { type ReactNode, useEffect, useRef, useState } from 'react'

export function Popover({
  label,
  title,
  className,
  panelClassName = '',
  align = 'start',
  children,
}: {
  label: ReactNode
  title?: string
  className: string
  panelClassName?: string
  align?: 'start' | 'end'
  children: (close: () => void) => ReactNode
}) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        title={title}
        aria-expanded={open}
        className={className}
        onClick={() => setOpen(!open)}
      >
        {label}
      </button>
      {open && (
        <div
          className={`absolute top-full z-30 mt-1.5 max-w-[calc(100vw-1.5rem)] rounded-lg border border-line bg-surface p-3 shadow-lg ${
            align === 'end' ? 'right-0' : 'left-0'
          } ${panelClassName}`}
        >
          {children(() => setOpen(false))}
        </div>
      )}
    </div>
  )
}
