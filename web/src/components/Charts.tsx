import { useMemo } from 'react'
import type { Band, Neighbor, WirelessEvent } from '@/types'
import { cn } from '@/ui/cn'

/** A neighbour reported at 0 dBm is a placeholder from the scan, not a real reading. */
export const realRssi = (rssi: number | null) => (rssi === null || rssi >= 0 ? null : rssi)

const defaultChannels: Record<Band, number[]> = {
  '2.4': [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13],
  '5': [36, 40, 44, 48, 52, 56, 60, 64, 100, 104, 108, 112, 116, 120, 124, 128, 132, 136, 140, 149, 153, 157, 161, 165],
  '6': [1, 5, 9, 13, 17, 21, 25, 29, 33, 37, 41, 45, 49, 53, 57, 61, 65, 69, 73, 77, 81, 85, 89, 93],
}

/**
 * Channel occupancy: one column per channel, height = number of other access
 * points heard there, colour = how strong the loudest one is. Our own channel
 * is outlined so a quieter alternative is easy to spot.
 */
export function ChannelChart({
  band,
  neighbors,
  ours,
  channels,
  height = 96,
  compact,
}: {
  band: Band
  neighbors: Neighbor[]
  ours?: number
  channels?: number[]
  height?: number
  compact?: boolean
}) {
  const columns = useMemo(() => {
    const list = channels?.length ? channels : defaultChannels[band]
    const byChannel = new Map<number, { count: number; strongest: number | null }>()
    for (const n of neighbors) {
      if (n.band !== band) continue
      const channel = n.primaryChannel || n.channel
      const entry = byChannel.get(channel) ?? { count: 0, strongest: null }
      entry.count += 1
      const rssi = realRssi(n.rssi)
      if (rssi !== null && (entry.strongest === null || rssi > entry.strongest)) entry.strongest = rssi
      byChannel.set(channel, entry)
    }
    return list.map((channel) => ({ channel, ...(byChannel.get(channel) ?? { count: 0, strongest: null }) }))
  }, [band, neighbors, channels])
  const max = Math.max(4, ...columns.map((c) => c.count))
  const tone = (rssi: number | null) => (rssi === null ? 'bg-faint opacity-80' : rssi >= -67 ? 'bg-danger opacity-80' : rssi >= -80 ? 'bg-warn opacity-80' : 'bg-ok opacity-80')
  const labelEvery = columns.length > 16 ? (compact ? 4 : 2) : 1

  return (
    <div className="min-w-0">
      <div className="flex items-end gap-[3px]" style={{ height }} role="img" aria-label={`Channel occupancy on ${band} GHz`}>
        {columns.map((c) => {
          const mine = c.channel === ours
          return (
            <div
              key={c.channel}
              className={cn('relative flex h-full min-w-0 flex-1 flex-col justify-end rounded-sm', mine && 'bg-accent-soft ring-1 ring-accent')}
              title={`Channel ${c.channel}: ${c.count} other ${c.count === 1 ? 'network' : 'networks'}${c.strongest !== null ? `, strongest ${c.strongest} dBm` : ''}${mine ? ' · this AP' : ''}`}
            >
              {c.count > 0 && <div className={cn('w-full rounded-sm', tone(c.strongest))} style={{ height: `${Math.max(6, (c.count / max) * 100)}%` }} />}
              {c.count > 0 && !compact && <span className="tabular absolute inset-x-0 text-center text-[9px] font-semibold text-muted" style={{ bottom: `calc(${Math.max(6, (c.count / max) * 100)}% + 1px)` }}>{c.count}</span>}
            </div>
          )
        })}
      </div>
      <div className="mt-1 flex gap-[3px]">
        {columns.map((c, i) => (
          <span key={c.channel} className={cn('tabular min-w-0 flex-1 text-center text-[9px] leading-3', c.channel === ours ? 'font-bold text-accent-text' : 'text-faint')}>
            {i % labelEvery === 0 || c.channel === ours ? c.channel : ''}
          </span>
        ))}
      </div>
    </div>
  )
}

export function ChannelLegend() {
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px] text-faint">
      <span className="flex items-center gap-1">
        <span className="h-2 w-2 rounded-sm bg-danger opacity-80" />
        strong (≥ −67 dBm)
      </span>
      <span className="flex items-center gap-1">
        <span className="h-2 w-2 rounded-sm bg-warn opacity-80" />
        medium
      </span>
      <span className="flex items-center gap-1">
        <span className="h-2 w-2 rounded-sm bg-ok opacity-80" />
        weak (&lt; −80 dBm)
      </span>
      <span className="flex items-center gap-1">
        <span className="h-2 w-3 rounded-sm bg-accent-soft ring-1 ring-accent" />
        this AP
      </span>
    </div>
  )
}

/** Parses the AP log timestamp "2026.10.02 07:10:11.123" (AP local time). */
export function eventTime(value: string): number | null {
  const m = /^(\d{4})\.(\d{2})\.(\d{2}) (\d{2}):(\d{2}):(\d{2})/.exec(value)
  if (!m) return null
  return new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]), Number(m[4]), Number(m[5]), Number(m[6])).getTime()
}

/** Events per time slot, stacked by kind: client activity versus radio warnings. */
export function EventTimeline({ events }: { events: WirelessEvent[] }) {
  const data = useMemo(() => {
    const times = events.map((e) => eventTime(e.time)).filter((t): t is number => t !== null)
    if (times.length < 2) return null
    const start = Math.min(...times)
    const end = Math.max(...times)
    const span = Math.max(end - start, 60_000)
    const steps = [60_000, 5 * 60_000, 15 * 60_000, 3_600_000, 6 * 3_600_000, 24 * 3_600_000]
    const step = steps.find((s) => span / s <= 72) ?? steps[steps.length - 1]!
    const first = Math.floor(start / step) * step
    const count = Math.floor((end - first) / step) + 1
    const buckets = Array.from({ length: count }, (_, i) => ({ at: first + i * step, clients: 0, warnings: 0, other: 0 }))
    for (const e of events) {
      const t = eventTime(e.time)
      if (t === null) continue
      const b = buckets[Math.floor((t - first) / step)]
      if (!b) continue
      if (e.tone === 'warn') b.warnings += 1
      else if (e.kind.startsWith('AP-STA-')) b.clients += 1
      else b.other += 1
    }
    return { buckets, step, max: Math.max(1, ...buckets.map((b) => b.clients + b.warnings + b.other)) }
  }, [events])
  if (!data) return null
  const label = (t: number) => {
    const d = new Date(t)
    return data.step >= 24 * 3_600_000 ? d.toLocaleDateString([], { day: '2-digit', month: 'short' }) : d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false })
  }
  const every = Math.ceil(data.buckets.length / 8)
  return (
    <div>
      <div className="flex h-20 items-end gap-[2px]" role="img" aria-label="Wireless events over time">
        {data.buckets.map((b) => {
          const total = b.clients + b.warnings + b.other
          return (
            <div
              key={b.at}
              className="flex h-full min-w-0 flex-1 flex-col justify-end"
              title={`${label(b.at)}: ${b.clients} client events, ${b.warnings} warnings, ${b.other} other`}
            >
              {total > 0 && (
                <div className="flex w-full flex-col overflow-hidden rounded-sm" style={{ height: `${Math.max(6, (total / data.max) * 100)}%` }}>
                  {b.warnings > 0 && <div className="bg-warn opacity-80" style={{ flex: b.warnings }} />}
                  {b.other > 0 && <div className="bg-faint opacity-80" style={{ flex: b.other }} />}
                  {b.clients > 0 && <div className="bg-accent opacity-80" style={{ flex: b.clients }} />}
                </div>
              )}
            </div>
          )
        })}
      </div>
      <div className="mt-1 flex gap-[2px]">
        {data.buckets.map((b, i) => (
          <span key={b.at} className="tabular min-w-0 flex-1 overflow-visible whitespace-nowrap text-[9px] leading-3 text-faint">
            {i % every === 0 ? label(b.at) : ''}
          </span>
        ))}
      </div>
      <div className="mt-2 flex flex-wrap gap-x-3 text-[11px] text-faint">
        <span className="flex items-center gap-1">
          <span className="h-2 w-2 rounded-sm bg-accent opacity-80" />
          client connects / disconnects
        </span>
        <span className="flex items-center gap-1">
          <span className="h-2 w-2 rounded-sm bg-warn opacity-80" />
          radio warnings
        </span>
        <span className="flex items-center gap-1">
          <span className="h-2 w-2 rounded-sm bg-faint opacity-80" />
          other
        </span>
      </div>
    </div>
  )
}
