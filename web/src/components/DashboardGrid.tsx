// The panel grid: one column on phones, two from md, four from xl. A panel's
// width is in quarters of the widest grid.
import type { ReactNode } from 'react'
import { spanClass } from '../dashboards'

export function DashboardGrid({
  items,
}: {
  items: { key: string; width: number; node: ReactNode }[]
}) {
  return (
    <div className="grid grid-cols-1 items-start gap-3 md:grid-cols-2 xl:grid-cols-4">
      {items.map((item) => (
        <div key={item.key} className={`min-w-0 ${spanClass(item.width)}`}>
          {item.node}
        </div>
      ))}
    </div>
  )
}
