// Dev-only routes for comparing the design variants on fixtures:
// /_proto, and /_proto/<a|b|c>/<logs|trace>, with ?theme=dark for dark.
import { useParams, useSearch } from '@tanstack/react-router'
import { applyTheme } from '../theme'
import { LogsScreen as LogsA } from './VariantA/LogsScreen'
import { TraceScreen as TraceA } from './VariantA/TraceScreen'
import { LogsScreen as LogsB } from './VariantB/LogsScreen'
import { TraceScreen as TraceB } from './VariantB/TraceScreen'
import { LogsScreen as LogsC } from './VariantC/LogsScreen'
import { TraceScreen as TraceC } from './VariantC/TraceScreen'

const screens: Record<string, Record<string, () => React.JSX.Element>> = {
  a: { logs: LogsA, trace: TraceA },
  b: { logs: LogsB, trace: TraceB },
  c: { logs: LogsC, trace: TraceC },
}

const names: Record<string, string> = {
  a: 'A, dense table',
  b: 'B, split pane',
  c: 'C, calm cards',
}

export function ProtoIndex() {
  return (
    <main className="mx-auto max-w-xl p-8 font-sans">
      <h1 className="text-xl font-semibold">Design variants</h1>
      <ul className="mt-4 space-y-2">
        {Object.keys(screens).map((v) => (
          <li key={v}>
            {names[v]}:{' '}
            <a className="text-accent underline" href={`/_proto/${v}/logs`}>
              logs
            </a>
            ,{' '}
            <a className="text-accent underline" href={`/_proto/${v}/trace`}>
              trace
            </a>
          </li>
        ))}
      </ul>
    </main>
  )
}

export function ProtoScreen() {
  const { variant, screen } = useParams({ strict: false }) as { variant: string; screen: string }
  const { theme } = useSearch({ strict: false }) as { theme?: string }
  // Applied during render, before the screens read the theme for their toggles.
  applyTheme(theme === 'dark' ? 'dark' : 'light')
  const Screen = screens[variant]?.[screen]
  return Screen ? <Screen /> : <ProtoIndex />
}
