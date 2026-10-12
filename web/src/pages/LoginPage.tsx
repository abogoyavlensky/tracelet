// Sign in by pasting a token. One token per browser keeps each device
// revocable on its own; the session cookie then carries it.
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { useState } from 'react'
import { ApiError, signIn } from '../api'
import { Mark } from '../components/icons'
import { sessionKey } from '../session'

export function LoginPage({ next }: { next?: string }) {
  const [token, setToken] = useState('')
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const mutation = useMutation({
    mutationFn: signIn,
    onSuccess: async (session) => {
      queryClient.setQueryData(sessionKey, session)
      await navigate({ href: next ?? '/logs' })
    },
  })

  return (
    <main className="flex min-h-dvh items-center justify-center bg-canvas px-4 py-10 font-sans text-fg">
      <div className="w-full max-w-md">
        <div className="flex items-center gap-2.5">
          <Mark size={28} />
          <span className="text-xl font-semibold">Tracelet</span>
        </div>
        <form
          className="mt-6 rounded-xl border border-line bg-surface p-6 shadow-sm"
          onSubmit={(e) => {
            e.preventDefault()
            if (token.trim()) mutation.mutate(token.trim())
          }}
        >
          <h1 className="text-lg font-semibold">Sign in</h1>
          <p className="mt-1 text-sm text-muted">
            Paste a read or admin token. The browser keeps it in a cookie scripts cannot read.
          </p>
          <label className="mt-5 block text-sm font-medium">
            Token
            <input
              type="password"
              autoComplete="current-password"
              autoFocus
              value={token}
              onChange={(e) => setToken(e.target.value)}
              placeholder="tl_…"
              className="mt-1.5 block h-11 w-full rounded-md border border-line bg-surface px-3 font-mono text-sm placeholder:text-subtle focus:border-accent focus:ring-2 focus:ring-accent/30 focus:outline-none"
            />
          </label>
          {mutation.isError && (
            <p className="mt-2 text-sm text-error">{signInError(mutation.error)}</p>
          )}
          <button
            type="submit"
            disabled={mutation.isPending || !token.trim()}
            className="mt-4 h-11 w-full rounded-md bg-accent text-sm font-medium text-accent-fg disabled:opacity-60"
          >
            {mutation.isPending ? 'Signing in…' : 'Sign in'}
          </button>
        </form>
        <div className="mt-5 px-1 text-sm text-muted">
          <p>
            Create a token for this browser, so you can revoke it on its own if the device goes
            missing:
          </p>
          <pre className="mt-2 rounded-md bg-raised p-3 font-mono text-xs break-words whitespace-pre-wrap text-fg">
            tracelet tokens create --scope read --name laptop
          </pre>
          <p className="mt-2">
            The admin token printed when the server first started (or set with{' '}
            <code className="font-mono text-xs">TRACELET_ADMIN_TOKEN</code>) works for a first
            sign-in, but it is better kept for the CLI.
          </p>
        </div>
      </div>
    </main>
  )
}

function signInError(error: unknown): string {
  if (error instanceof ApiError && error.status === 401)
    return 'That token is not valid or was revoked.'
  if (error instanceof ApiError && error.status === 403)
    return 'Ingest tokens only send telemetry. Sign in with a read or admin token.'
  return error instanceof Error ? error.message : 'Signing in failed.'
}
