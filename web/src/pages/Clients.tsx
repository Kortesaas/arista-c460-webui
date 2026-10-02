import { useMemo, useState } from 'react'
import { Search, Users } from 'lucide-react'
import { Page } from '@/app/Page'
import { LoadingState } from '@/components/Loading'
import { ClientDialog } from '@/components/ClientDetails'
import type { Client } from '@/types'
import { Age, BandChip, SignalBars, VlanChip } from '@/components/status'
import { useApp } from '@/stores/app'
import { Button, EmptyState, Input, Panel, Select } from '@/ui/kit'
import { formatBytes } from '@/utils/format'

export function ClientsPage() {
  const state = useApp((store) => store.state)
  const [query, setQuery] = useState('')
  const [ssid, setSsid] = useState('')
  const [selected, setSelected] = useState<Client | null>(null)
  const clients = useMemo(() => {
    const q = query.trim().toLowerCase()
    return (state?.clients ?? []).filter(
      (client) => (!ssid || client.ssid === ssid) && (!q || [client.mac, client.ipv4, client.hostname, client.os, client.ssid].some((value) => value.toLowerCase().includes(q))),
    )
  }, [state?.clients, query, ssid])
  if (!state) return <LoadingState />

  return (
    <Page
      title="Clients"
      description="Devices currently associated with this access point."
      actions={
        <>
          <div className="relative w-56">
            <Search size={13} className="pointer-events-none absolute left-2 top-2.5 text-faint" />
            <Input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="MAC, IP, hostname…" className="pl-7" aria-label="Filter clients" />
          </div>
          <div className="w-44">
          <Select value={ssid} onChange={(event) => setSsid(event.target.value)} aria-label="Network">
            <option value="">All networks</option>
            {state.ssids.map((item) => (
              <option key={item.name} value={item.name}>
                {item.name}
              </option>
            ))}
          </Select>
          </div>
        </>
      }
    >
      {clients.length === 0 ? (
        <Panel>
          <EmptyState
            icon={<Users size={28} />}
            title={state.clients.length ? 'No matching clients' : 'No clients connected'}
            description={state.clients.length ? 'Adjust the search or network filter.' : 'Clients appear here as soon as they join one of the networks.'}
          />
        </Panel>
      ) : (
        <div className="overflow-x-auto rounded-lg border border-line bg-surface">
          <table className="w-full min-w-[900px] text-left text-[13px]">
            <thead className="border-b border-line text-2xs font-semibold uppercase tracking-wider text-faint">
              <tr>
                <th className="px-3 py-2">Client</th>
                <th className="px-3 py-2">Network</th>
                <th className="px-3 py-2">Band</th>
                <th className="px-3 py-2">Signal</th>
                <th className="px-3 py-2 text-right">SNR</th>
                <th className="px-3 py-2 text-right">Rate ↓/↑</th>
                <th className="px-3 py-2 text-right">Traffic</th>
                <th className="px-3 py-2 text-right">Connected</th>
                <th className="px-3 py-2 text-right">Details</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-line">
              {clients.map((client) => (
                <tr key={`${client.ssid}-${client.mac}`} className="hover:bg-surface-2/50">
                  <td className="px-3 py-2">
                    {client.hostname || client.ipv4 ? (
                      <>
                        <p className="font-medium text-ink">{client.hostname || client.ipv4}</p>
                        <p className="mono text-[11px] text-faint">
                          {[client.mac, client.hostname ? client.ipv4 : '', client.os].filter(Boolean).join(' · ')}
                        </p>
                      </>
                    ) : (
                      <>
                        <p className="mono font-medium text-ink">{client.mac}</p>
                        <p className="text-[11px] text-faint">{client.os || 'No IP address seen yet'}</p>
                      </>
                    )}
                  </td>
                  <td className="px-3 py-2">
                    <div className="flex items-center gap-1.5">
                      <span className="text-ink">{client.ssid}</span>
                      <VlanChip vlan={client.vlan} />
                    </div>
                  </td>
                  <td className="px-3 py-2">
                    <BandChip band={client.band} />
                    {client.mode && <span className="ml-1 text-[11px] text-faint">{client.mode}</span>}
                  </td>
                  <td className="px-3 py-2">
                    <SignalBars rssi={client.rssi} />
                  </td>
                  <td className="tabular px-3 py-2 text-right text-muted">{client.snr === null ? '—' : `${client.snr} dB`}</td>
                  <td className="tabular px-3 py-2 text-right text-muted">
                    {client.rxRate ?? '—'} / {client.txRate ?? '—'} <span className="text-faint">Mb/s</span>
                  </td>
                  <td className="tabular px-3 py-2 text-right text-[12px] text-faint">
                    ↓ {formatBytes(client.rxBytes)} · ↑ {formatBytes(client.txBytes)}
                  </td>
                  <td className="px-3 py-2 text-right text-[12px] text-muted">{client.connectedSince ? <Age iso={client.connectedSince} /> : '—'}</td>
                  <td className="px-3 py-2 text-right">
                    <Button size="sm" aria-label={`Details for ${client.hostname || client.mac}`} onClick={() => setSelected(client)}>
                      View
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {selected && <ClientDialog client={selected} onClose={() => setSelected(null)} />}
    </Page>
  )
}
