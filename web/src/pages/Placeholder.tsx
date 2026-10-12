// Stand-in pages until the chosen design is built (Tasks 5-8).
export function Placeholder({ title, data }: { title: string; data: unknown }) {
  return (
    <main className="p-6">
      <h1 className="text-xl font-semibold">{title}</h1>
      <pre className="mt-4 font-mono text-sm text-muted">{JSON.stringify(data, null, 2)}</pre>
    </main>
  )
}
