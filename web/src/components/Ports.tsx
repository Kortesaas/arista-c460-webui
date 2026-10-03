import { ArrowDown, ArrowUp } from 'lucide-react'
import type { Interface } from '@/types'
import { cn } from '@/ui/cn'
import { Badge, HelpTip } from '@/ui/kit'
import { formatBytes } from '@/utils/format'

export const portLabel = (iface: Interface) => (iface.port ? `ETH ${iface.port}` : iface.name)

/** RJ45 socket outline with a link LED, drawn to resemble the AP's ports. */
function Socket({ up }: { up: boolean }) {
  return (
    <svg viewBox="0 0 40 32" width={40} height={32} aria-hidden="true" className="shrink-0 text-muted">
      <rect x="1" y="1" width="38" height="30" rx="3" fill="none" stroke="currentColor" strokeWidth="1.5" />
      <path d="M8 9h24v14h-6v4H14v-4H8z" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinejoin="round" />
      {[12, 15, 18, 21, 24, 27].map((x) => (
        <line key={x} x1={x} y1="11" x2={x} y2="15" stroke="currentColor" strokeWidth="1.2" />
      ))}
      <circle cx="34" cy="5.5" r="2.3" className={up ? 'fill-[var(--ok)]' : 'fill-[var(--border-strong)]'} />
    </svg>
  )
}

/** The two Ethernet sockets with link state, uplink role and traffic. */
export function PortCards({ interfaces, compact }: { interfaces: Interface[]; compact?: boolean }) {
  return (
    <div className="grid gap-2 sm:grid-cols-2">
      {interfaces.map((iface) => (
        <div key={iface.name} className={cn('rounded-lg border p-3', iface.up ? 'border-line bg-surface' : 'border-dashed border-line bg-surface-2')}>
          <div className="flex items-start gap-3">
            <Socket up={iface.up} />
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-1.5">
                <span className="text-[13px] font-semibold text-ink">{portLabel(iface)}</span>
                {iface.role === 'uplink' ? <Badge tone="accent">Uplink</Badge> : <Badge>Backup</Badge>}
                <HelpTip label={portLabel(iface)}>
                  Linux interface <span className="mono">{iface.name}</span>, MAC <span className="mono">{iface.mac || '—'}</span>. The firmware always names the active uplink eth0,
                  so the Linux name can change when the uplink moves to the other socket.
                </HelpTip>
              </div>
              <p className={cn('mt-0.5 text-[12px]', iface.up ? 'text-ok' : 'text-faint')}>
                {iface.up ? `${iface.speed}${iface.duplex ? ` · ${iface.duplex.toLowerCase()} duplex` : ''}` : 'No link'}
              </p>
            </div>
          </div>
          {!compact && (
            <div className="tabular mt-2.5 flex flex-wrap gap-x-4 gap-y-1 border-t border-line pt-2 text-[11px] text-faint">
              <span className="flex items-center gap-1" title="Received">
                <ArrowDown size={11} /> {formatBytes(iface.inOctets)}
              </span>
              <span className="flex items-center gap-1" title="Sent">
                <ArrowUp size={11} /> {formatBytes(iface.outOctets)}
              </span>
              <span className={cn(iface.inErrors + iface.outErrors > 0 && 'text-warn')}>
                {iface.inErrors + iface.outErrors} errors · {iface.inDiscards + iface.outDiscards} discards
              </span>
            </div>
          )}
        </div>
      ))}
    </div>
  )
}
