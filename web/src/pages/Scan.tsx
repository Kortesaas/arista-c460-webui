import { useMemo, useState } from 'react'
import { ArrowDown, ArrowUp, Radar, Search } from 'lucide-react'
import { Page } from '@/app/Page'
import { LoadingState } from '@/components/Loading'
import { ChannelChart, ChannelLegend, realRssi } from '@/components/Charts'
import { Age, BandChip, SignalBars } from '@/components/status'
import { useApp } from '@/stores/app'
import type { Band, Neighbor } from '@/types'
import { cn } from '@/ui/cn'
import { EmptyState, Input, Panel, Segmented, Toggle } from '@/ui/kit'
import { opModeShort } from '@/utils/format'

type SortKey = 'signal' | 'ssid' | 'channel'
const BANDS: Band[] = ['2.4', '5', '6']

export function ScanPage() {
  const state = useApp((store) => store.state)
  const [band, setBand] = useState<Band | 'all'>('all')
  const [query, setQuery] = useState('')
  const [showHidden, setShowHidden] = useState(true)
  const [sort, setSort] = useState<{ key: SortKey; desc: boolean }>({ key: 'signal', desc: true })

  const neighbors = useMemo(() => {
    const q = query.trim().toLowerCase()
    const list = (state?.neighbors ?? []).filter(
      (n) => (band === 'all' || n.band === band) && (showHidden || n.ssid) && (!q || n.ssid.toLowerCase().includes(q) || n.bssid.toLowerCase().includes(q)),
    )
    const value = (n: Neighbor) => (sort.key === 'signal' ? (realRssi(n.rssi) ?? -999) : sort.key === 'channel' ? n.primaryChannel || n.channel : n.ssid.toLowerCase() || '~')
    return [...list].sort((a, b) => {
      const va = value(a)
      const vb = value(b)
      const order = va < vb ? -1 : va > vb ? 1 : 0
      return sort.desc ? -order : order
    })
  }, [state?.neighbors, band, query, showHidden, sort])
  if (!state) return <LoadingState />

  const radios = new Map(state.radios.map((radio) => [radio.band, radio]))
  const strong = state.neighbors.filter((n) => (realRssi(n.rssi) ?? -999) >= -67).length
  const header = (key: SortKey, label: string, className?: string) => (
    <th className={cn('px-3 py-2', className)}>
      <button type="button" className="inline-flex items-center gap-1 uppercase tracking-wider hover:text-ink" onClick={() => setSort((s) => ({ key, desc: s.key === key ? !s.desc : key === 'signal' }))}>
        {label}
        {sort.key === key && (sort.desc ? <ArrowDown size={11} /> : <ArrowUp size={11} />)}
      </button>
    </th>
  )

  return (
    <Page title="RF environment" description={`${state.neighbors.length} other access points heard, ${strong} of them strong enough to interfere.`}>
      <div className="grid items-start gap-3 xl:grid-cols-3">
        {BANDS.filter((b) => radios.has(b)).map((b) => {
          const radio = radios.get(b)!
          const count = state.neighbors.filter((n) => n.band === b).length
          return (
            <Panel
              key={b}
              title={
                <span className="flex items-center gap-2">
                  <BandChip band={b} /> {count} {count === 1 ? 'network' : 'networks'}
                </span>
              }
              actions={<span className="tabular text-[12px] text-muted">this AP: ch {radio.channel}</span>}
            >
              <ChannelChart band={b} neighbors={state.neighbors} ours={radio.channel} channels={radio.allowedChannels} />
            </Panel>
          )
        })}
      </div>
      <div className="mt-2 flex flex-wrap items-center justify-between gap-2">
        <ChannelLegend />
        <p className="text-[11px] text-faint">Bar height = number of networks on the channel; colour = the strongest one. Pick a channel with short, green bars.</p>
      </div>

      <Panel
        className="mt-3"
        title="Networks heard"
        help="From the AP's background scans. Signal is how loud each network is at the AP; above −67 dBm it competes noticeably for airtime on the same channel."
        bodyClassName="p-0"
        actions={<span className="tabular text-[12px] text-muted">{neighbors.length} shown</span>}
      >
        <div className="flex flex-wrap items-center gap-2 border-b border-line p-2.5">
          <div className="relative min-w-[180px] flex-1 sm:max-w-xs">
            <Search size={13} className="pointer-events-none absolute left-2 top-2.5 text-faint" />
            <Input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="SSID or BSSID" className="pl-7" aria-label="Filter networks" />
          </div>
          <Segmented
            value={band}
            onChange={setBand}
            options={[
              { value: 'all', label: 'All bands' },
              { value: '2.4', label: '2.4' },
              { value: '5', label: '5' },
              { value: '6', label: '6' },
            ]}
          />
          <div className="ml-auto w-44">
            <Toggle checked={showHidden} onChange={setShowHidden} label="Hidden networks" />
          </div>
        </div>
        {neighbors.length === 0 ? (
          <EmptyState icon={<Radar size={28} />} title="No matching networks" description="Nothing heard with the current filter." />
        ) : (
          <div className="max-h-[520px] overflow-auto">
          <table className="w-full min-w-[800px] table-fixed text-left text-[13px]">
            <colgroup>
              <col />
              <col className="w-24" />
              <col className="w-24" />
              <col className="w-36" />
              <col className="w-40" />
              <col className="w-24" />
            </colgroup>
              <thead className="sticky top-0 z-10 border-b border-line bg-surface text-2xs font-semibold text-faint">
                <tr>
                  {header('ssid', 'Network')}
                  <th className="px-3 py-2 uppercase tracking-wider">Band</th>
                  {header('channel', 'Channel', 'text-right')}
                  {header('signal', 'Signal')}
                  <th className="px-3 py-2 uppercase tracking-wider">Security</th>
                  <th className="px-3 py-2 text-right uppercase tracking-wider">Seen</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line">
                {neighbors.map((n) => {
                  const channel = n.primaryChannel || n.channel
                  const sameChannel = radios.get(n.band)?.channel === channel
                  return (
                    <tr key={`${n.radioId}-${n.bssid}`} className="hover:bg-surface-2">
                      <td className="px-3 py-1.5">
                        <p className={n.ssid ? 'text-ink' : 'italic text-faint'}>{n.ssid || 'hidden network'}</p>
                        <p className="mono text-[11px] text-faint">{n.bssid}</p>
                      </td>
                      <td className="px-3 py-1.5">
                        <BandChip band={n.band} />
                      </td>
                      <td className={cn('tabular px-3 py-1.5 text-right', sameChannel ? 'font-semibold text-accent-text' : 'text-muted')} title={sameChannel ? 'Same channel as this AP' : undefined}>
                        {channel}
                      </td>
                      <td className="px-3 py-1.5">
                        <SignalBars rssi={realRssi(n.rssi)} />
                      </td>
                      <td className="px-3 py-1.5 text-[12px] text-muted">{n.opmode ? opModeShort(n.opmode) : '—'}</td>
                      <td className="px-3 py-1.5 text-right text-[12px] text-faint">{n.lastSeen ? <Age iso={n.lastSeen} /> : '—'}</td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </Panel>
    </Page>
  )
}
