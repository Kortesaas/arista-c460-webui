import { useMemo, useState } from 'react'
import { Radar, Search } from 'lucide-react'
import { Page } from '@/app/Page'
import { LoadingState } from '@/components/Loading'
import { Age, BandChip, SignalBars } from '@/components/status'
import { useApp } from '@/stores/app'
import type { Band } from '@/types'
import { cn } from '@/ui/cn'
import { EmptyState, Input, Panel, Segmented } from '@/ui/kit'
import { opModeShort } from '@/utils/format'

export function ScanPage() {
  const state = useApp((store) => store.state)
  const [band, setBand] = useState<Band | 'all'>('all')
  const [query, setQuery] = useState('')
  const neighbors = useMemo(() => {
    const q = query.trim().toLowerCase()
    return (state?.neighbors ?? []).filter((n) => (band === 'all' || n.band === band) && (!q || n.ssid.toLowerCase().includes(q) || n.bssid.toLowerCase().includes(q)))
  }, [state?.neighbors, band, query])
  if (!state) return <LoadingState />

  // Channel occupancy per band, to spot crowded channels next to ours.
  const ours = new Map(state.radios.map((radio) => [radio.band, radio.channel]))
  const occupancy = new Map<string, number>()
  for (const n of neighbors) occupancy.set(`${n.band}:${n.primaryChannel || n.channel}`, (occupancy.get(`${n.band}:${n.primaryChannel || n.channel}`) ?? 0) + 1)
  const busiest = [...occupancy.entries()].sort((a, b) => b[1] - a[1]).slice(0, 12)

  return (
    <Page
      title="RF scan"
      description="Other access points this AP can hear, from its background scans. Use it to pick quieter channels."
      actions={
        <>
          <div className="relative">
            <Search size={13} className="pointer-events-none absolute left-2 top-2.5 text-faint" />
            <Input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="SSID or BSSID" className="w-48 pl-7" />
          </div>
          <Segmented
            value={band}
            onChange={setBand}
            options={[
              { value: 'all', label: 'All' },
              { value: '2.4', label: '2.4' },
              { value: '5', label: '5' },
              { value: '6', label: '6' },
            ]}
          />
        </>
      }
    >
      {busiest.length > 0 && (
        <div className="mb-3 flex flex-wrap gap-1.5">
          {busiest.map(([key, count]) => {
            const [b, channel] = key.split(':') as [Band, string]
            const mine = ours.get(b) === Number(channel)
            return (
              <span key={key} className={cn('tabular rounded border px-2 py-1 text-[12px]', mine ? 'border-accent bg-accent-soft text-accent-text' : 'border-line bg-surface text-muted')} title={mine ? 'This AP uses this channel' : undefined}>
                {b} GHz ch {channel} · <span className="font-semibold">{count}</span>
              </span>
            )
          })}
        </div>
      )}
      {neighbors.length === 0 ? (
        <Panel>
          <EmptyState icon={<Radar size={28} />} title="No neighbouring networks" description="Nothing heard on the selected band yet." />
        </Panel>
      ) : (
        <div className="overflow-x-auto rounded-lg border border-line bg-surface">
          <table className="w-full min-w-[760px] text-left text-[13px]">
            <thead className="border-b border-line text-2xs font-semibold uppercase tracking-wider text-faint">
              <tr>
                <th className="px-3 py-2">SSID</th>
                <th className="px-3 py-2">BSSID</th>
                <th className="px-3 py-2">Band</th>
                <th className="px-3 py-2 text-right">Channel</th>
                <th className="px-3 py-2">Signal</th>
                <th className="px-3 py-2">Security</th>
                <th className="px-3 py-2 text-right">Last seen</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-line">
              {neighbors.map((n) => (
                <tr key={`${n.radioId}-${n.bssid}`} className="hover:bg-surface-2/50">
                  <td className="px-3 py-2 text-ink">{n.ssid || <span className="text-faint">hidden</span>}</td>
                  <td className="mono px-3 py-2 text-[12px] text-muted">{n.bssid}</td>
                  <td className="px-3 py-2">
                    <BandChip band={n.band} />
                  </td>
                  <td className={cn('tabular px-3 py-2 text-right', ours.get(n.band) === (n.primaryChannel || n.channel) ? 'font-semibold text-accent-text' : 'text-muted')}>{n.primaryChannel || n.channel}</td>
                  <td className="px-3 py-2">
                    <SignalBars rssi={n.rssi} />
                  </td>
                  <td className="px-3 py-2 text-[12px] text-muted">{n.opmode ? opModeShort(n.opmode) : '—'}</td>
                  <td className="px-3 py-2 text-right text-[12px] text-faint">{n.lastSeen ? <Age iso={n.lastSeen} /> : '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Page>
  )
}
