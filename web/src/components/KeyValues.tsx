// A titled table of key and value pairs; nested values show as formatted JSON.
export function KeyValues({
  title,
  rows,
  monoKeys,
}: {
  title: string
  rows: [string, string][]
  monoKeys?: boolean
}) {
  if (rows.length === 0) return null
  return (
    <section className="mt-7">
      <h2 className="mb-2 text-sm font-semibold">{title}</h2>
      <dl className="divide-y divide-line/70 rounded-lg border border-line">
        {rows.map(([k, v]) => (
          <div
            key={k}
            className="grid grid-cols-[minmax(8rem,32%)_1fr] gap-4 px-3 py-2 text-[13px]"
          >
            <dt
              className={`truncate text-muted ${monoKeys ? 'font-mono text-[12.5px]' : ''}`}
              title={k}
            >
              {k}
            </dt>
            <dd className="min-w-0 font-mono text-[12.5px]">
              {/^[[{]/.test(v) ? (
                <pre className="overflow-x-auto whitespace-pre">{pretty(v)}</pre>
              ) : (
                <span className="break-all">{v}</span>
              )}
            </dd>
          </div>
        ))}
      </dl>
    </section>
  )
}

function pretty(json: string): string {
  try {
    return JSON.stringify(JSON.parse(json), null, 2)
  } catch {
    return json
  }
}
