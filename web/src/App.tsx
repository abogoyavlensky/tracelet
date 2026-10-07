import { useQuery } from '@tanstack/react-query'
import { getHealth } from './api'

export function App() {
  return (
    <main className="min-h-screen bg-slate-950 text-slate-100">
      <div className="mx-auto flex max-w-3xl flex-col gap-8 px-6 py-16">
        <header className="flex flex-col gap-2">
          <h1 className="text-3xl font-semibold tracking-tight">Tracelet</h1>
          <p className="text-slate-400">Lightweight observability for your projects.</p>
        </header>
        <ServerStatus />
      </div>
    </main>
  )
}

function ServerStatus() {
  const health = useQuery({
    queryKey: ['health'],
    queryFn: getHealth,
    refetchInterval: 5_000,
  })

  if (health.isPending) {
    return <StatusCard tone="neutral" title="Connecting" detail="Waiting for the server." />
  }

  if (health.isError) {
    return <StatusCard tone="error" title="Server unreachable" detail={health.error.message} />
  }

  return <StatusCard tone="ok" title="Server healthy" detail={`Version ${health.data.version}`} />
}

type StatusCardProps = {
  tone: 'ok' | 'error' | 'neutral'
  title: string
  detail: string
}

const toneClasses: Record<StatusCardProps['tone'], string> = {
  ok: 'border-emerald-500/40 bg-emerald-500/10',
  error: 'border-rose-500/40 bg-rose-500/10',
  neutral: 'border-slate-700 bg-slate-900',
}

function StatusCard({ tone, title, detail }: StatusCardProps) {
  return (
    <section className={`rounded-lg border px-5 py-4 ${toneClasses[tone]}`}>
      <h2 className="font-medium">{title}</h2>
      <p className="mt-1 text-sm text-slate-400">{detail}</p>
    </section>
  )
}
