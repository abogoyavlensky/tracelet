// What a list shows while loading, when empty, on error, and at its end.
import type { ReactNode } from 'react'
import { ApiError } from '../api'
import { icons } from './icons'

export function ErrorState({ error, onRetry }: { error: unknown; onRetry?: () => void }) {
  const message = error instanceof Error ? error.message : String(error)
  const retry =
    error instanceof ApiError && error.retryAfter ? ` Try again in ${error.retryAfter} s.` : ''
  return (
    <div className="m-4 flex items-start gap-3 rounded-lg border border-error/30 bg-error/8 p-4 text-sm md:mx-5">
      <icons.warning size={18} className="mt-0.5 shrink-0 text-error" />
      <div className="min-w-0">
        <p className="font-medium text-error">The search failed</p>
        <p className="mt-0.5 break-words text-muted">
          {message}.{retry}
        </p>
        {onRetry && (
          <button
            type="button"
            onClick={onRetry}
            className="mt-2 h-8 rounded-md border border-line bg-surface px-3 text-[13px] hover:bg-raised"
          >
            Retry
          </button>
        )}
      </div>
    </div>
  )
}

export function EmptyState({ title, children }: { title: string; children?: ReactNode }) {
  return (
    // w-full: auto margins in the shell's column flexbox would otherwise size
    // the box to its widest unbreakable line, wider than a phone.
    <div className="mx-auto w-full max-w-md px-6 py-16 text-center">
      <p className="font-medium">{title}</p>
      {children && <div className="mt-1.5 text-sm text-muted">{children}</div>}
    </div>
  )
}

export function Notice({ children }: { children: ReactNode }) {
  return (
    <p className="border-b border-sev-warn/25 bg-sev-warn/8 px-4 py-2 text-[13px] text-sev-warn md:px-5">
      {children}
    </p>
  )
}

export function LoadMore({
  hasMore,
  loading,
  onLoad,
  label,
}: {
  hasMore: boolean
  loading: boolean
  onLoad: () => void
  label: string
}) {
  if (!hasMore) return null
  return (
    <div className="p-3">
      <button
        type="button"
        disabled={loading}
        onClick={onLoad}
        className="h-10 w-full rounded-md border border-line text-sm text-muted hover:bg-raised hover:text-fg disabled:opacity-60 md:h-9"
      >
        {loading ? 'Loading…' : label}
      </button>
    </div>
  )
}

export function Loading({ label }: { label: string }) {
  return <p className="px-5 py-10 text-center text-sm text-muted">{label}</p>
}
