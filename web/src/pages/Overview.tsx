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

/** Small link in a panel header, the same everywhere. */
function PanelLink({ to, children }: { to: string; children: string }) {
  return (
    <Link to={to} className="flex items-center gap-1 text-[12px] font-medium text-accent-text hover:underline">
      {children} <ArrowRight size={12} />
    </Link>
  )
}

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
        <Stat label="Networks" value={`${activeSsids.length}/${ssids.length}`} detail="broadcasting" tone={activeSsids.length ? 'ok' : 'warn'} icon={<Wifi size={13} />} />
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
          className="col-span-2 sm:col-span-1"
        />
      </div>

      {/* On narrow screens problems come first; on wide screens they sit in the right column. */}
      <div className="mt-3 xl:hidden">
        <HealthPanel items={state.health ?? []} />
      </div>
      <div className="mt-3 grid gap-3 xl:grid-cols-[1.4fr_1fr]">
        <section className="flex min-w-0 flex-col gap-3">
          <Panel title="Radios" bodyClassName="p-0" actions={<PanelLink to="/radios">Configure</PanelLink>}>
            <div className="grid divide-y divide-line md:grid-cols-3 md:divide-x md:divide-y-0">
              {radios.map((radio) => (
                <div key={radio.id} className="min-w-0 p-3">
                  <div className="flex items-center justify-between gap-2">
                    <BandChip band={radio.band} />
                    <Badge tone={radio.enabled ? 'ok' : 'neutral'}>
                      <Dot tone={radio.enabled ? 'ok' : 'neutral'} />
                      {radio.enabled ? 'On' : 'Off'}
                    </Badge>
                  </div>
                  <p className="tabular mt-2.5 text-lg font-semibold leading-6 text-ink">
                    Ch {radio.channel}
                    <span className="ml-1.5 text-[13px] font-normal text-muted">{radio.width} MHz</span>
                    {isDfs(radio.band, radio.channel) && <span className="ml-1.5 text-2xs font-semibold text-warn">DFS</span>}
                  </p>
                  <p className="tabular text-[12px] text-muted">
                    {radio.eirp ?? '—'} dBm EIRP
                    {radio.dca && ' · auto channel'}
                  </p>
                  <div className="mt-3">
                    <Meter value={radio.utilization} label="Channel utilisation" />
                  </div>
                  <div className="tabular mt-2 flex justify-between gap-2 text-[11px] text-faint">
                    <span>{plural(radio.clients, 'client')}</span>
                    <span>noise {radio.noiseFloor ?? '—'} dBm</span>
                    <span>{radio.neighbors} nearby</span>
                  </div>
                </div>
              ))}
            </div>
          </Panel>

          <TrendsPanel />
        </section>

        <div className="flex min-w-0 flex-col gap-3">
          <div className="hidden xl:block">
            <HealthPanel items={state.health ?? []} />
          </div>
          <Panel title="Access point" actions={<PanelLink to="/system">Details</PanelLink>}>
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
          <Panel title="Ethernet" actions={<PanelLink to="/network">Network</PanelLink>}>
            <PortCards interfaces={interfaces} compact />
          </Panel>
        </div>
      </div>

      <Panel title={`Wireless networks (${ssids.length})`} className="mt-3" bodyClassName="p-0" actions={<PanelLink to="/wireless">Manage</PanelLink>}>
        {ssids.length === 0 ? (
          <EmptyState icon={<Wifi size={24} />} title="No wireless networks" description="Create one under Wireless networks." />
        ) : (
          <>
            {/* Fixed columns on wider screens, so chips line up from row to row. */}
            <div className="hidden gap-x-4 border-b border-line px-3 py-1.5 text-2xs font-semibold uppercase tracking-wider text-faint sm:grid sm:grid-cols-[0.5rem_minmax(0,1fr)_8rem_10rem_5rem] xl:grid-cols-[0.5rem_minmax(0,1fr)_9rem_11rem_10rem_5rem]">
              <span />
              <span>Network</span>
              <span>VLAN</span>
              <span className="hidden xl:block">Security</span>
              <span>Bands</span>
              <span className="text-right">Clients</span>
            </div>
            <ul className="divide-y divide-line">
              {ssids.map((ssid) => (
                <li key={ssid.name} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2 sm:grid sm:grid-cols-[0.5rem_minmax(0,1fr)_8rem_10rem_5rem] sm:gap-x-4 xl:grid-cols-[0.5rem_minmax(0,1fr)_9rem_11rem_10rem_5rem]">
                  <Dot tone={ssid.enabled ? 'ok' : 'neutral'} />
                  <span className="min-w-0 flex-1 basis-[9rem] truncate text-[13px] font-medium text-ink">
                    {ssid.name}
                    {ssid.hidden && <span className="ml-1.5 text-2xs font-normal text-faint">hidden</span>}
                  </span>
                  <span className="flex min-w-0 items-center justify-self-start">
                    <VlanChip vlan={ssid.vlan} className="max-w-full truncate" />
                  </span>
                  <span className="hidden truncate text-[12px] text-muted xl:block" title={opModeShort(ssid.opmode)}>
                    {opModeShort(ssid.opmode)}
                  </span>
                  <span className="flex items-center gap-1">
                    {ssid.bands.map((band) => (
                      <BandChip key={band} band={band} />
                    ))}
                  </span>
                  <span className="tabular ml-auto text-right text-[12px] text-muted sm:ml-0">{plural(ssid.clients, 'client')}</span>
                </li>
              ))}
            </ul>
          </>
        )}
      </Panel>
    </Page>
  )
}
