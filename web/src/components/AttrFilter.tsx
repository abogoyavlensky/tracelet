// Attribute filters: exact `key:value` matches on top-level attributes.
import { useState } from 'react'
import { icons } from './icons'
import { Popover } from './Popover'

export function AttrFilter({
  value,
  onChange,
}: {
  value: string[]
  onChange: (attrs: string[]) => void
}) {
  return (
    <>
      {value.map((a) => (
        <span
          key={a}
          className="flex h-9 shrink-0 items-center gap-1 rounded-md border border-accent/35 bg-accent/8 pl-2.5 pr-1 font-mono text-[12.5px] md:h-8"
        >
          {a}
          <button
            type="button"
            aria-label={`Remove ${a}`}
            onClick={() => onChange(value.filter((x) => x !== a))}
            className="grid size-6 place-items-center rounded text-muted hover:bg-raised hover:text-fg"
          >
            <icons.close size={13} />
          </button>
        </span>
      ))}
      <Popover
        className="flex h-9 shrink-0 items-center gap-1 rounded-md px-2 text-[13px] text-muted hover:bg-raised hover:text-fg md:h-8"
        label={
          <>
            <icons.plus size={14} />
            Attribute
          </>
        }
        panelClassName="w-72"
      >
        {(close) => (
          <AddAttr onAdd={(a) => (onChange(value.includes(a) ? value : [...value, a]), close())} />
        )}
      </Popover>
    </>
  )
}

function AddAttr({ onAdd }: { onAdd: (attr: string) => void }) {
  const [text, setText] = useState('')
  const [error, setError] = useState('')
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        const attr = text.trim()
        if (!/^[^:]+:/.test(attr)) return setError('Use key:value, such as user.id:42.')
        onAdd(attr)
      }}
    >
      <label className="block text-xs text-muted">
        Attribute equals
        <input
          autoFocus
          value={text}
          onChange={(e) => (setText(e.target.value), setError(''))}
          placeholder="user.id:42"
          className="mt-1 block h-9 w-full rounded-md border border-line bg-surface px-2 font-mono text-sm text-fg placeholder:text-subtle"
        />
      </label>
      {error && <p className="mt-1.5 text-xs text-error">{error}</p>}
      <button
        type="submit"
        className="mt-2.5 h-9 w-full rounded-md bg-accent text-sm font-medium text-accent-fg"
      >
        Add filter
      </button>
    </form>
  )
}
