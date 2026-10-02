import { EventTimeline } from '@/components/Charts'
import { useEffect, useMemo, useRef, useState } from 'react'
import { RefreshCw, Search, Download } from 'lucide-react'
import { api } from '@/api'
import type { NetworkSnapshot, WirelessEventLog } from '@/types'
import { Badge, Button, Input, Panel, Select, Spinner } from '@/ui/kit'

export function NetworkPathsPanel() {
  const [data, setData] = useState<NetworkSnapshot | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const pending = useRef(false)
  const load = async () => {
    if (pending.current) return
    pending.current = true
    setBusy(true)
    try {
      setData(await api.network())
      setError('')
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      pending.current = false
      setBusy(false)
    }
  }
  useEffect(() => {
    void load()
  }, [])
  return (
    <div className="space-y-3">
      <Panel
        title="Network paths"
        actions={
          <Button size="sm" disabled={busy} onClick={() => void load()}>
            {busy ? <Spinner size={12} /> : <RefreshCw size={12} />} Refresh
          </Button>
        }
      >
        <p className="text-[12px] leading-5 text-muted">
          Wireless traffic is bridged to the switch. The router supplies client addresses; a VLAN bridge does not need its own AP address.
        </p>
        {error && (
          <p role="alert" className="mt-3 text-[12px] text-danger">
            {error}
          </p>
        )}
        {data?.warnings.map((warning) => (
          <p key={warning} role="alert" className="mt-2 text-[12px] text-warn">
            {warning}
          </p>
        ))}
        {data ? (
          <div className="mt-3 grid gap-2 xl:grid-cols-2">
            {data.bridges.map((bridge) => (
              <div key={bridge.name} className="rounded border border-line p-3">
                <div className="flex items-center justify-between gap-2">
                  <span className="text-[13px] font-medium text-ink">
                    {bridge.vlanMode === 'tagged'
                      ? `VLAN ${bridge.vlan}`
                      : bridge.vlanMode === 'native'
                        ? 'Native network'
                        : bridge.vlanMode === 'mixed'
                          ? 'Mixed VLAN bridge'
                          : 'VLAN mapping unknown'}
                  </span>
                  <Badge tone={bridge.up ? 'ok' : 'warn'}>{bridge.up ? 'Enabled' : 'Disabled'}</Badge>
                </div>
                <p className="mt-1 break-words text-[12px] text-muted">{bridge.networks.join(' · ') || 'No wireless networks mapped'}</p>
                <p className="mono mt-2 text-[11px] text-faint">
                  {bridge.name}
                  {bridge.addresses.length ? ` · ${bridge.addresses.join(', ')}` : ' · Layer 2 bridge'}
                </p>
                <div className="mt-2 flex flex-wrap gap-1">
                  {bridge.members.map((member) => (
                    <Badge key={member}>{member}</Badge>
                  ))}
                </div>
              </div>
            ))}
          </div>
        ) : (
          !error && <p className="mt-3 text-[12px] text-muted">Reading network paths…</p>
        )}
        {data && <p className="mt-3 text-[11px] text-faint">Sampled {new Date(data.sampledAt).toLocaleTimeString()}. Snapshots are cached for up to five seconds.</p>}
      </Panel>
      {data && (
        <>
          <Panel title="Management routes" bodyClassName="p-0">
            <p className="border-b border-line px-3 py-2 text-[11px] text-faint">IPv4 and IPv6 routes. Link-local IPv6 routes are omitted.</p>
            <div className="overflow-x-auto">
              <table className="w-full min-w-[450px] text-left text-[12px]">
                <thead className="border-b border-line text-faint">
                  <tr>
                    {['Destination', 'Via', 'Interface'].map((h) => (
                      <th key={h} className="px-3 py-2 font-medium">
                        {h}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody className="divide-y divide-line">
                  {data.routes.map((route, i) => (
                    <tr key={i}>
                      <td className="mono px-3 py-2 text-ink">{route.dst === 'default' ? 'Default gateway' : route.dst}</td>
                      <td className="mono px-3 py-2 text-muted">{route.gateway || 'Directly connected'}</td>
                      <td className="mono px-3 py-2 text-muted">{route.dev}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            {!data.routes.length && <p className="p-3 text-[12px] text-muted">No routes returned.</p>}
          </Panel>
          <Panel title="Learned network neighbours" bodyClassName="p-0">
            <p className="border-b border-line px-3 py-2 text-[11px] leading-4 text-faint">
              Recent IPv4/IPv6 neighbours learned by the AP. Unresolved entries can indicate a missing gateway or a disconnected device.
            </p>
            <div className="overflow-x-auto">
              <table className="w-full min-w-[580px] text-left text-[12px]">
                <thead className="border-b border-line text-faint">
                  <tr>
                    {['Address', 'MAC', 'Interface', 'State'].map((h) => (
                      <th key={h} className="px-3 py-2 font-medium">
                        {h}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody className="divide-y divide-line">
                  {data.neighbors.map((peer) => (
                    <tr key={`${peer.dev}-${peer.dst}`}>
                      <td className="px-3 py-2">
                        <span className="mono text-ink">{peer.dst}</span>
                        {peer.gateway && (
                          <span className="ml-2">
                            <Badge tone="accent">Gateway</Badge>
                          </span>
                        )}
                      </td>
                      <td className="mono px-3 py-2 text-muted">{peer.lladdr || '—'}</td>
                      <td className="mono px-3 py-2 text-muted">{peer.dev}</td>
                      <td className="px-3 py-2">
                        <Badge tone={peer.state.some((s) => ['INCOMPLETE', 'FAILED'].includes(s)) ? 'warn' : 'neutral'}>
                          {peer.state
                            .map(
                              (s) =>
                                ({
                                  INCOMPLETE: 'Unresolved',
                                  FAILED: 'Failed',
                                  REACHABLE: 'Reachable',
                                  STALE: 'Cached',
                                  DELAY: 'Checking',
                                  PROBE: 'Checking',
                                  PERMANENT: 'Permanent',
                                })[s] || s,
                            )
                            .join(', ') || 'Unknown'}
                        </Badge>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            {!data.neighbors.length && <p className="p-3 text-[12px] text-muted">No neighbours learned yet.</p>}
          </Panel>
        </>
      )}
    </div>
  )
}
export function WirelessEventsPanel() {
  const [data, setData] = useState<WirelessEventLog | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [query, setQuery] = useState('')
  const [kind, setKind] = useState('all')
  const pending = useRef(false)
  const load = async () => {
    if (pending.current) return
    pending.current = true
    setBusy(true)
    try {
      setData(await api.events())
      setError('')
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      pending.current = false
      setBusy(false)
    }
  }
  useEffect(() => {
    void load()
    const tick = setInterval(() => {
      if (document.visibilityState !== 'hidden') void load()
    }, 5000)
    return () => clearInterval(tick)
  }, [])
  const rows = useMemo(
    () =>
      (data?.events || []).filter(
        (e) =>
          (kind === 'all' || (kind === 'clients' ? e.kind.startsWith('AP-STA-') : e.tone === 'warn')) &&
          `${e.network} ${e.client} ${e.interface} ${e.summary}`.toLowerCase().includes(query.toLowerCase().trim()),
      ),
    [data, query, kind],
  )
  const exportEvents = () => {
    const text = [
      'Wireless events · AP local time',
      ...rows.map(
        (e) => `${e.time || 'Unknown time'} | ${e.network || e.interface} | ${e.summary}${e.client ? ` | ${e.client}` : ''}${e.frequency ? ` | ${e.frequency} MHz` : ''}`,
      ),
    ].join('\n')
    const url = URL.createObjectURL(new Blob([text], { type: 'text/plain' }))
    const a = document.createElement('a')
    a.href = url
    a.download = 'ap-wireless-events.txt'
    document.body.appendChild(a)
    a.click()
    a.remove()
    setTimeout(() => URL.revokeObjectURL(url), 1000)
  }
  return (
    <Panel
      title="Recent wireless events"
      help="The latest 150 events from the AP's logs, refreshed every five seconds while this page is open. Times use the AP's clock; network names follow the current interface mapping."
      actions={
        <>
          <Button size="sm" disabled={!rows.length} onClick={exportEvents}>
            <Download size={12} />
            <span className="hidden sm:inline">Export</span>
          </Button>
          <Button size="sm" disabled={busy} onClick={() => void load()}>
            {busy ? <Spinner size={12} /> : <RefreshCw size={12} />} Refresh
          </Button>
        </>
      }
      bodyClassName="p-0"
    >
      <div className="border-b border-line p-3">
        <div className="grid gap-2 sm:grid-cols-[1fr_150px]">
          <div className="relative">
            <Search size={13} className="absolute left-2 top-2.5 text-faint" />
            <Input className="pl-7" aria-label="Filter wireless events" placeholder="Network, client or event" value={query} onChange={(e) => setQuery(e.target.value)} />
          </div>
          <Select aria-label="Event type" value={kind} onChange={(e) => setKind(e.target.value)}>
            <option value="all">All events</option>
            <option value="clients">Client connections</option>
            <option value="warnings">Radio warnings</option>
          </Select>
        </div>
        {data && data.clockSynced !== true && <p className="mt-2 text-[11px] text-warn">The AP clock is not synchronised, so event times may be off. Set an NTP server under Network.</p>}
        {rows.length > 1 && (
          <div className="mt-3">
            <EventTimeline events={rows} />
          </div>
        )}
      </div>
      {(error || data?.error) && (
        <p role="alert" className="p-3 text-[12px] text-danger">
          {error || data?.error}
        </p>
      )}
      <div className="max-h-[480px] overflow-auto">
        <table className="w-full min-w-[640px] text-left text-[12px]">
          <thead className="sticky top-0 border-b border-line bg-surface text-faint">
            <tr>
              {['AP local time', 'Network', 'Event', 'Client'].map((h) => (
                <th key={h} className="px-3 py-2 font-medium">
                  {h}
                </th>
              ))}
            </tr>
          </thead>
          <tbody className="divide-y divide-line">
            {rows.map((event) => (
              <tr key={event.id}>
                <td className="mono whitespace-nowrap px-3 py-2 text-faint">{event.time ? event.time.replace(/\.\d+$/, '') : '—'}</td>
                <td className="px-3 py-2 text-ink">
                  {event.network || 'Unknown network'}
                  <p className="mono text-[11px] text-faint">{event.interface}</p>
                </td>
                <td className="px-3 py-2">
                  <span className={event.tone === 'warn' ? 'text-warn' : 'text-muted'}>{event.summary}</span>
                  {event.frequency && <p className="tabular mt-0.5 text-[11px] text-faint">{event.frequency} MHz</p>}
                </td>
                <td className="mono px-3 py-2 text-muted">{event.client || '—'}</td>
              </tr>
            ))}
          </tbody>
        </table>
        {!rows.length && <p className="p-6 text-center text-[12px] text-muted">{busy ? 'Reading events…' : 'No matching wireless events.'}</p>}
      </div>
      <div className="border-t border-line px-3 py-2 text-[11px] text-faint">{rows.length} events shown · latest 150 retained from available logs</div>
    </Panel>
  )
}
