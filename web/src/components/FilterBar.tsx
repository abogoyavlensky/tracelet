// The full-width filter row above a list. On phones the filters fold behind
// a "Filters" button next to the search box and the time range.
import { type ReactNode, useEffect, useState } from 'react'
import { icons } from './icons'

export function FilterBar({
  query,
  onQuery,
  placeholder,
  active,
  summary,
  range,
  children,
}: {
  query: string
  onQuery: (q: string) => void
  placeholder: string
  /** How many filters are set, shown on the phone's Filters button. */
  active: number
  summary: ReactNode
  /** The time range control, shown here on phones (the header has it on wider screens). */
  range: ReactNode
  children: ReactNode
}) {
  const [open, setOpen] = useState(false)
  return (
    <div className="shrink-0 border-b border-line bg-surface">
      <div className="flex flex-wrap items-center gap-2 px-3 py-2 md:px-5 md:py-2.5">
        <div
          className={`w-full flex-wrap items-center gap-2 md:flex md:w-auto md:shrink-0 ${open ? 'flex' : 'hidden'} order-2 md:order-none`}
        >
          {children}
        </div>
        <SearchBox value={query} onChange={onQuery} placeholder={placeholder} />
        <button
          type="button"
          aria-expanded={open}
          onClick={() => setOpen(!open)}
          className="flex h-10 items-center gap-1.5 rounded-md border border-line px-3 text-sm md:hidden"
        >
          <icons.filter size={15} />
          {active > 0 ? active : 'Filters'}
        </button>
        <div className="md:hidden">{range}</div>
        <span className="ml-auto hidden shrink-0 text-xs text-muted lg:block">{summary}</span>
      </div>
    </div>
  )
}

/** The search box; it reports changes 300 ms after typing stops. */
function SearchBox({
  value,
  onChange,
  placeholder,
}: {
  value: string
  onChange: (q: string) => void
  placeholder: string
}) {
  const [text, setText] = useState(value)
  // Follow the URL when it changes from elsewhere (back button, a link).
  const [seen, setSeen] = useState(value)
  if (value !== seen) {
    setSeen(value)
    setText(value)
  }
  useEffect(() => {
    if (text === value) return
    const t = setTimeout(() => onChange(text), 300)
    return () => clearTimeout(t)
  }, [text, value, onChange])
  return (
    <label className="flex h-10 min-w-0 flex-1 items-center gap-2 rounded-md bg-raised px-2.5 focus-within:ring-2 focus-within:ring-accent/40 md:h-8 md:max-w-xl md:min-w-48">
      <icons.search size={15} className="shrink-0 text-subtle" />
      <input
        type="search"
        value={text}
        onChange={(e) => setText(e.target.value)}
        placeholder={placeholder}
        className="w-full min-w-0 bg-transparent text-sm outline-none placeholder:text-subtle md:text-[13px]"
      />
    </label>
  )
}
