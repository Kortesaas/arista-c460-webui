import { Link } from 'react-router-dom'
import { Antenna, ArrowRight, Cable, Thermometer, Users, Wifi } from 'lucide-react'
import { Page } from '@/app/Page'
import { LoadingState } from '@/components/Loading'
import { Age, BandChip, Dot, Meter, Stat, VlanChip } from '@/components/status'
import { useApp } from '@/stores/app'
import { cn } from '@/ui/cn'
import { Badge, EmptyState, KeyValue, Panel } from '@/ui/kit'
import { PortCards, portLabel } from '@/components/Ports'
import { formatDuration, isDfs, opModeShort, plural } from '@/utils/format'
import { HealthPanel, healthTone } from '@/components/Health'
import { TrendsPanel } from '@/components/Trends'

export function OverviewPage() {
  const state = useApp((store) => store.state)
  if (!state) return <LoadingState />
  const { device, radios, ssids, clients, interfaces } = state

  const activeSsids = ssids.filter((ssid) => ssid.enabled)
  const activeRadios = radios.filter((radio) => radio.enabled)
  const uplinks = interfaces.filter((iface) => iface.up)
  const tone = state.error || activeRadios.length === 0 ? 'danger' : healthTone(state.health)
  const memUsed = device.memTotal ? ((device.memTotal - device.memAvailable) / device.memTotal) * 100 : null
  const storageUsed = device.storageTotal ? ((device.storageTotal - device.storageFree) / device.storageTotal) * 100 : null

  return (
    <Page
      title={
        <span className="flex items-center gap-2.5">
          <span className={cn('h-3 w-3 rounded-full', tone === 'ok' ? 'bg-ok pulse-ok' : tone === 'warn' ? 'bg-warn' : 'bg-danger')} />
          {device.siteName || device.hostname}
        </span>
      }
      description={
        <span>
          {device.model} · firmware {device.firmware || '—'} · up {formatDuration(device.uptimeSeconds)} · updated <Age iso={state.generatedAt} />
        </span>
      }
      dense
    >
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-5">
        <Stat label="Wireless networks" value={`${activeSsids.length}/${ssids.length}`} detail="broadcasting" tone={activeSsids.length ? 'ok' : 'warn'} icon={<Wifi size={13} />} />
        <Stat label="Clients" value={clients.length} detail={clients.length ? `${plural(new Set(clients.map((c) => c.ssid)).size, 'network')}` : 'none connected'} icon={<Users size={13} />} />
        <Stat label="Radios" value={`${activeRadios.length}/${radios.length}`} detail={activeRadios.map((radio) => `${radio.band}`).join(' · ') + ' GHz'} tone={activeRadios.length === radios.length ? 'ok' : 'warn'} icon={<Antenna size={13} />} />
        <Stat
          label="Uplink"
          value={uplinks.length ? uplinks[0]!.speed || 'Up' : 'Down'}
          detail={interfaces.map((iface) => `${portLabel(iface)} ${iface.up ? 'up' : 'down'}`).join(' · ')}
          tone={uplinks.length ? 'ok' : 'danger'}
          icon={<Cable size={13} />}
        />
        <Stat
          label="Temperature"
          value={device.temperatureC === null ? '—' : `${Math.round(device.temperatureC)} °C`}
          detail="hottest sensor"
          tone={device.temperatureC !== null && device.temperatureC > 85 ? 'danger' : device.temperatureC !== null && device.temperatureC > 75 ? 'warn' : 'neutral'}
          icon={<Thermometer size={13} />}
        />
      </div>

      {/* On narrow screens problems come first; on wide screens they sit in the right column. */}
      <div className="mt-3 xl:hidden">
        <HealthPanel items={state.health ?? []} />
      </div>
      <div className="mt-3 grid gap-3 xl:grid-cols-[1.4fr_1fr]">
        <section className="min-w-0 space-y-3">
          <div>
            <div className="mb-1.5 flex items-center justify-between">
              <h3 className="text-[12px] font-semibold uppercase tracking-wider text-faint">Radios</h3>
              <Link to="/radios" className="flex items-center gap-1 text-[12px] text-accent-text hover:underline">
                Configure <ArrowRight size={12} />
              </Link>
            </div>
            <div className="grid gap-2 md:grid-cols-3">
              {radios.map((radio) => (
                <div key={radio.id} className="rounded-lg border border-line bg-surface p-3">
                  <div className="flex items-center justify-between gap-2">
                    <BandChip band={radio.band} />
                    <Badge tone={radio.enabled ? 'ok' : 'neutral'}>
                      <Dot tone={radio.enabled ? 'ok' : 'neutral'} />
                      {radio.enabled ? 'On' : 'Off'}
                    </Badge>
                  </div>
                  <p className="tabular mt-2 text-lg font-semibold text-ink">
                    Ch {radio.channel}
                    <span className="ml-1.5 text-[13px] font-normal text-muted">{radio.width} MHz</span>
                    {isDfs(radio.band, radio.channel) && <span className="ml-1.5 text-2xs font-semibold text-warn">DFS</span>}
                  </p>
                  <p className="tabular text-[12px] text-muted">
                    {radio.eirp ?? '—'} dBm EIRP
                    {radio.dca && ' · auto channel'}
                  </p>
                  <div className="mt-2.5">
                    <Meter value={radio.utilization} label="Channel utilisation" />
                  </div>
                  <div className="tabular mt-2 flex justify-between text-[11px] text-faint">
                    <span>{plural(radio.clients, 'client')}</span>
                    <span>noise {radio.noiseFloor ?? '—'} dBm</span>
                    <span>{radio.neighbors} nearby</span>
                  </div>
                </div>
              ))}
            </div>
          </div>

          <TrendsPanel />

          <Panel
            title={`Wireless networks (${ssids.length})`}
            bodyClassName="p-0"
            actions={
              <Link to="/wireless" className="text-[12px] text-accent-text hover:underline">
                Manage
              </Link>
            }
          >
            {ssids.length === 0 ? (
              <EmptyState icon={<Wifi size={24} />} title="No wireless networks" description="Create one under Wireless networks." />
            ) : (
              <ul className="divide-y divide-line">
                {ssids.map((ssid) => (
                  <li key={ssid.name} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2">
                    <Dot tone={ssid.enabled ? 'ok' : 'neutral'} />
                    <span className="min-w-0 flex-1 basis-[9rem] truncate text-[13px] font-medium text-ink">
                      {ssid.name}
                      {ssid.hidden && <span className="ml-1.5 text-2xs font-normal text-faint">hidden</span>}
                    </span>
                    <VlanChip vlan={ssid.vlan} />
                    <span className="hidden text-[12px] text-muted sm:inline">{opModeShort(ssid.opmode)}</span>
                    <span className="flex gap-1">
                      {ssid.bands.map((band) => (
                        <BandChip key={band} band={band} />
                      ))}
                    </span>
                    <span className="tabular w-16 text-right text-[12px] text-muted">{plural(ssid.clients, 'client')}</span>
                  </li>
                ))}
              </ul>
            )}
          </Panel>
        </section>

        <div className="min-w-0 space-y-3">
          <div className="hidden xl:block">
            <HealthPanel items={state.health ?? []} />
          </div>
          <Panel
            title="Access point"
            actions={
              <Link to="/system" className="text-[12px] text-accent-text hover:underline">
                Details
              </Link>
            }
          >
            <KeyValue
              items={[
                { label: 'Management IP', value: device.mgmtIp ? `${device.mgmtIp}/${device.mgmtPrefix}` : '—', mono: true },
                { label: 'Gateway', value: device.gateway || '—', mono: true },
                { label: 'Firmware', value: device.firmware || '—' },
                { label: 'Uptime', value: formatDuration(device.uptimeSeconds) },
              ]}
            />
            <div className="mt-3 grid grid-cols-2 gap-3 border-t border-line pt-3">
              <Meter value={memUsed} label="Memory" warnAt={75} dangerAt={90} />
              <Meter value={storageUsed} label="Flash" warnAt={75} dangerAt={90} />
            </div>
          </Panel>
          <Panel
            title="Ethernet"
            actions={
              <Link to="/network" className="text-[12px] text-accent-text hover:underline">
                Network
              </Link>
            }
          >
            <PortCards interfaces={interfaces} compact />
          </Panel>
        </div>
      </div>
    </Page>
  )
}
