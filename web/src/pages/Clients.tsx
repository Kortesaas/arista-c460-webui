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
      (client) => (!ssid || client.ssid === ssid) && (!q || [client.mac, client.ipv4, ...client.ipv6, client.hostname, client.os, client.ssid].some((value) => value.toLowerCase().includes(q))),
    )
  }, [state?.clients, query, ssid])
  if (!state) return <LoadingState />

  return (
    <Page
      title="Clients"
      description="Devices currently associated with this access point."
    >
      <div className="mb-3 flex flex-wrap items-center gap-2 rounded-lg border border-line bg-surface p-3">
          <div className="relative min-w-[180px] flex-1 sm:max-w-sm">
            <Search size={13} className="pointer-events-none absolute left-2 top-2.5 text-faint" />
            <Input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="MAC, IP, hostname…" className="pl-7" aria-label="Filter clients" />
          </div>
          <div className="w-full sm:w-48">
          <Select value={ssid} onChange={(event) => setSsid(event.target.value)} aria-label="Network">
            <option value="">All networks</option>
            {state.ssids.map((item) => (
              <option key={item.name} value={item.name}>
                {item.name}
              </option>
            ))}
          </Select>
          </div>
          <span className="ml-auto text-[12px] text-faint">{clients.length} of {state.clients.length} shown</span>
      </div>
      {clients.length === 0 ? (
        <Panel>
          <EmptyState
            icon={<Users size={28} />}
            title={state.clients.length ? 'No matching clients' : 'No clients connected'}
            description={state.clients.length ? 'Adjust the search or network filter.' : 'Clients appear here as soon as they join one of the networks.'}
          />
        </Panel>
      ) : (
        <>
        <ul className="space-y-2 md:hidden">
          {clients.map((client) => (
            <li key={`${client.ssid}-${client.mac}`} className="rounded-lg border border-line bg-surface p-3">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0"><ClientIdentity client={client} /></div>
                <Button size="sm" aria-label={`Details for ${client.hostname || client.mac}`} onClick={() => setSelected(client)}>View</Button>
              </div>
              <div className="mt-2 flex flex-wrap items-center gap-2">
                <span className="break-words text-[12px] text-ink">{client.ssid}</span>
                <VlanChip vlan={client.vlan} />
                <BandChip band={client.band} />
                <SignalBars rssi={client.rssi} />
              </div>
              <dl className="mt-3 grid grid-cols-2 gap-x-3 gap-y-2 border-t border-line pt-2 text-[11px]">
                <div><dt className="text-faint">Rate ↓ / ↑</dt><dd className="tabular text-muted">{client.rxRate ?? '—'} / {client.txRate ?? '—'} Mb/s</dd></div>
                <div><dt className="text-faint">Connected</dt><dd className="text-muted">{client.connectedSince ? <Age iso={client.connectedSince} /> : '—'}</dd></div>
                <div><dt className="text-faint">Downloaded</dt><dd className="tabular text-muted">{formatBytes(client.rxBytes)}</dd></div>
                <div><dt className="text-faint">Uploaded</dt><dd className="tabular text-muted">{formatBytes(client.txBytes)}</dd></div>
              </dl>
            </li>
          ))}
        </ul>
        <div className="hidden overflow-x-auto rounded-lg border border-line bg-surface md:block">
          <table className="w-full min-w-[1200px] table-fixed text-left text-[13px]">
            <colgroup>
              <col /><col className="w-52" /><col className="w-24" /><col className="w-32" />
              <col className="w-16" /><col className="w-36" /><col className="w-40" /><col className="w-24" /><col className="w-16" />
            </colgroup>
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
                    <ClientIdentity client={client} />
                  </td>
                  <td className="px-3 py-2">
                    <div className="flex flex-wrap items-center gap-1.5">
                      <span className="min-w-0 break-words text-ink">{client.ssid}</span>
                      <VlanChip vlan={client.vlan} />
                    </div>
                  </td>
                  <td className="px-3 py-2">
                    <BandChip band={client.band} />
                    {client.mode && <span className="mt-1 block truncate text-[11px] text-faint" title={client.mode}>{client.mode}</span>}
                  </td>
                  <td className="px-3 py-2">
                    <SignalBars rssi={client.rssi} />
                  </td>
                  <td className="tabular px-3 py-2 text-right text-muted">{client.snr === null ? '—' : `${client.snr} dB`}</td>
                  <td className="tabular px-3 py-2 text-right text-muted">
                    {client.rxRate ?? '—'} / {client.txRate ?? '—'} <span className="text-faint">Mb/s</span>
                  </td>
                  <td className="tabular px-3 py-2 text-right text-[12px] text-faint">
                    <p>↓ {formatBytes(client.rxBytes)}</p><p>↑ {formatBytes(client.txBytes)}</p>
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
        </>
      )}
      {selected && <ClientDialog client={selected} onClose={() => setSelected(null)} />}
    </Page>
  )
}

function ClientIdentity({ client }: { client: Client }) {
  const address = client.ipv4 || client.ipv6.find((ip) => !/^fe[89ab]/i.test(ip)) || client.ipv6[0] || ''
  const addressKind = !client.ipv4 && address ? (/^fe[89ab]/i.test(address) ? 'IPv6 link-local' : 'IPv6') : ''
  const name = client.hostname || address || client.mac
  const detail = client.hostname || address
    ? [client.mac, client.hostname ? address : '', addressKind, !address ? 'No IP address observed' : '', client.os].filter(Boolean).join(' · ')
    : ['No IP address observed', client.os].filter(Boolean).join(' · ')
  return <>
    <p className="truncate text-[13px] font-medium text-ink" title={name}>{name}</p>
    <p className="mono truncate text-[11px] text-faint" title={detail}>{detail}</p>
  </>
}
