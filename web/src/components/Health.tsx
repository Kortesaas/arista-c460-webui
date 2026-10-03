import { Link } from 'react-router-dom'
import { AlertTriangle, ArrowRight, CircleCheck, Info, OctagonAlert } from 'lucide-react'
import type { HealthItem } from '@/types'
import { cn } from '@/ui/cn'
import { Panel } from '@/ui/kit'

const severityOrder = { danger: 0, warn: 1, info: 2 } as const
const icons = { danger: OctagonAlert, warn: AlertTriangle, info: Info }
const tones = { danger: 'text-danger', warn: 'text-warn', info: 'text-muted' }

/** The overall tone shown next to the AP name: info findings do not count. */
export function healthTone(items: HealthItem[] | undefined): 'ok' | 'warn' | 'danger' {
  if (items?.some((i) => i.severity === 'danger')) return 'danger'
  if (items?.some((i) => i.severity === 'warn')) return 'warn'
  return 'ok'
}

export function HealthPanel({ items }: { items: HealthItem[] }) {
  const sorted = [...items].sort((a, b) => severityOrder[a.severity] - severityOrder[b.severity])
  const problems = items.filter((i) => i.severity !== 'info').length
  return (
    <Panel
      title={problems ? `Health · ${problems} ${problems === 1 ? 'problem' : 'problems'}` : 'Health'}
      help="Checked continuously on the AP: clock, power, temperature, uplinks, channel load, radar, weak clients and pending changes. Notes in grey are worth knowing but not a problem."
      bodyClassName="p-0"
    >
      {sorted.length === 0 ? (
        <p className="flex items-center gap-2 px-3 py-2.5 text-[13px] text-ok">
          <CircleCheck size={15} /> All checks passed
        </p>
      ) : (
        <ul className="divide-y divide-line">
          {sorted.map((item) => {
            const Icon = icons[item.severity]
            const body = (
              <>
                <Icon size={15} className={cn('mt-0.5 shrink-0', tones[item.severity])} />
                <span className="min-w-0 flex-1">
                  <span className={cn('block text-[13px] font-medium', item.severity === 'info' ? 'text-ink' : tones[item.severity])}>{item.title}</span>
                  <span className="block text-[12px] leading-4 text-muted">{item.detail}</span>
                </span>
                {item.link && <ArrowRight size={13} className="mt-1 shrink-0 text-faint transition-colors group-hover:text-accent-text" />}
              </>
            )
            return (
              <li key={item.id}>
                {item.link ? (
                  <Link to={item.link} className="group flex items-start gap-2.5 px-3 py-2 hover:bg-surface-2">
                    {body}
                  </Link>
                ) : (
                  <div className="flex items-start gap-2.5 px-3 py-2">{body}</div>
                )}
              </li>
            )
          })}
        </ul>
      )}
    </Panel>
  )
}
