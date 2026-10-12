// A filter pill over a native <select>: it looks like the rest of the filter
// bar and gets the platform's own picker, which is the best one on a phone.
import { icons } from './icons'

export type Option = { value: string; label: string }

export function SelectFilter({
  label,
  value,
  options,
  onChange,
}: {
  label: string
  value: string
  options: Option[]
  onChange: (value: string) => void
}) {
  const current = options.find((o) => o.value === value) ?? options[0]
  const active = value !== options[0]?.value
  return (
    <label
      className={`relative flex h-9 shrink-0 cursor-pointer items-center gap-1.5 rounded-md border px-2.5 text-[13px] focus-within:ring-2 focus-within:ring-accent/40 md:h-8 ${
        active ? 'border-accent/35 bg-accent/8' : 'border-line hover:border-subtle'
      }`}
    >
      <span className="text-muted">{label}</span>
      <span className="max-w-40 truncate font-medium">{current?.label}</span>
      <icons.chevronDown size={13} className="text-subtle" />
      <select
        aria-label={label}
        className="absolute inset-0 cursor-pointer opacity-0"
        value={value}
        onChange={(e) => onChange(e.target.value)}
      >
        {options.map((o) => (
          <option key={o.value} value={o.value}>
            {o.label}
          </option>
        ))}
      </select>
    </label>
  )
}
