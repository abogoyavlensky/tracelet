// A filter pill holding free text, edited in a small popover.
import { useState } from 'react'
import { icons } from './icons'
import { Popover } from './Popover'

export function TextFilter({
  label,
  value,
  placeholder,
  hint,
  mono,
  validate,
  onChange,
}: {
  label: string
  value: string | undefined
  placeholder: string
  hint?: string
  mono?: boolean
  /** An error message for text the API would refuse, or undefined. */
  validate?: (text: string) => string | undefined
  onChange: (value: string | undefined) => void
}) {
  return (
    <Popover
      className={`flex h-9 shrink-0 items-center gap-1.5 rounded-md border px-2.5 text-[13px] md:h-8 ${
        value ? 'border-accent/35 bg-accent/8' : 'border-line hover:border-subtle'
      }`}
      label={
        <>
          <span className="text-muted">{label}</span>
          <span
            className={`max-w-40 truncate font-medium ${mono && value ? 'font-mono text-[12.5px]' : ''}`}
          >
            {value ?? 'Any'}
          </span>
          <icons.chevronDown size={13} className="text-subtle" />
        </>
      }
      panelClassName="w-72"
    >
      {(close) => (
        <Editor
          label={label}
          value={value ?? ''}
          placeholder={placeholder}
          hint={hint}
          mono={mono}
          validate={validate}
          onApply={(v) => (onChange(v || undefined), close())}
        />
      )}
    </Popover>
  )
}

function Editor({
  label,
  value,
  placeholder,
  hint,
  mono,
  validate,
  onApply,
}: {
  label: string
  value: string
  placeholder: string
  hint?: string
  mono?: boolean
  validate?: (text: string) => string | undefined
  onApply: (value: string) => void
}) {
  const [text, setText] = useState(value)
  const [error, setError] = useState<string>()
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        const v = text.trim()
        const problem = v ? validate?.(v) : undefined
        if (problem) return setError(problem)
        onApply(v)
      }}
    >
      <label className="block text-xs text-muted">
        {label}
        <input
          autoFocus
          value={text}
          onChange={(e) => (setText(e.target.value), setError(undefined))}
          placeholder={placeholder}
          className={`mt-1 block h-9 w-full rounded-md border border-line bg-surface px-2 text-sm text-fg placeholder:text-subtle ${mono ? 'font-mono' : ''}`}
        />
      </label>
      {hint && !error && <p className="mt-1.5 text-xs text-muted">{hint}</p>}
      {error && <p className="mt-1.5 text-xs text-error">{error}</p>}
      <div className="mt-2.5 flex gap-2">
        <button
          type="submit"
          className="h-9 flex-1 rounded-md bg-accent text-sm font-medium text-accent-fg"
        >
          Apply
        </button>
        {value && (
          <button
            type="button"
            onClick={() => onApply('')}
            className="h-9 rounded-md border border-line px-3 text-sm hover:bg-raised"
          >
            Clear
          </button>
        )}
      </div>
    </form>
  )
}
