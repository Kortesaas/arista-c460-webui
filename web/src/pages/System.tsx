import { useState } from 'react'
import { KeyRound } from 'lucide-react'
import { Page } from '@/app/Page'
import { LoadingState } from '@/components/Loading'
import { Dot } from '@/components/status'
import { api } from '@/api'
import { useApp } from '@/stores/app'
import { Button, Field, Input, KeyValue, Panel, Spinner } from '@/ui/kit'
import { formatBytes, formatDuration } from '@/utils/format'

export function SystemPage() {
  const state = useApp((store) => store.state)
  if (!state) return <LoadingState />
  const { device, interfaces } = state
  return (
    <Page title="System" description="Device information, Ethernet ports and access to this web interface.">
      <div className="grid gap-3 lg:grid-cols-2">
        <Panel title="Device">
          <KeyValue
            items={[
              { label: 'Model', value: device.model },
              { label: 'Firmware', value: device.firmware || '—' },
              { label: 'Hostname', value: device.hostname, mono: true },
              { label: 'MAC address', value: device.mac, mono: true },
              { label: 'Management IP', value: device.mgmtIp ? `${device.mgmtIp}/${device.mgmtPrefix}` : '—', mono: true },
              { label: 'Default gateway', value: device.gateway || '—', mono: true },
              { label: 'Regulatory country', value: device.country || '—' },
              { label: 'Uptime', value: formatDuration(device.uptimeSeconds) },
              { label: 'Load average', value: device.load.join(' / ') || '—' },
              { label: 'Memory', value: `${formatBytes(device.memAvailable)} free of ${formatBytes(device.memTotal)}` },
              { label: 'Flash storage', value: `${formatBytes(device.storageFree)} free of ${formatBytes(device.storageTotal)}` },
              { label: 'Temperature', value: device.temperatureC === null ? '—' : `${device.temperatureC.toFixed(1)} °C` },
              { label: 'SSH', value: device.sshEnabled ? 'enabled' : 'disabled' },
            ]}
          />
        </Panel>

        <div className="space-y-3">
          <Panel title="Ethernet ports" bodyClassName="p-0">
            <table className="w-full text-left text-[12px]">
              <thead className="border-b border-line text-2xs font-semibold uppercase tracking-wider text-faint">
                <tr>
                  <th className="px-3 py-2">Port</th>
                  <th className="px-3 py-2">Link</th>
                  <th className="px-3 py-2 text-right">Received</th>
                  <th className="px-3 py-2 text-right">Sent</th>
                  <th className="px-3 py-2 text-right">Errors</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line">
                {interfaces.map((iface) => (
                  <tr key={iface.name}>
                    <td className="mono px-3 py-2 text-ink">{iface.name}</td>
                    <td className="px-3 py-2">
                      <span className="flex items-center gap-1.5 text-muted">
                        <Dot tone={iface.up ? 'ok' : 'danger'} />
                        {iface.up ? `${iface.speed} ${iface.duplex.toLowerCase()}` : 'no link'}
                      </span>
                    </td>
                    <td className="tabular px-3 py-2 text-right text-muted">{formatBytes(iface.inOctets)}</td>
                    <td className="tabular px-3 py-2 text-right text-muted">{formatBytes(iface.outOctets)}</td>
                    <td className="tabular px-3 py-2 text-right text-muted">{iface.inErrors + iface.outErrors}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </Panel>
          <PasswordPanel />
          <Panel title="About">
            <p className="text-[12px] leading-5 text-muted">
              Local web interface for the C-460 access point, version <span className="mono text-ink">{device.uiVersion}</span>. It runs on the access point itself and manages it through the AP's own OpenConfig agent; no controller or cloud service is involved.
            </p>
            <p className="mt-2 text-[11px] leading-4 text-faint">Unofficial project, not affiliated with or endorsed by Arista Networks. Use at your own risk.</p>
          </Panel>
        </div>
      </div>
    </Page>
  )
}

function PasswordPanel() {
  const toast = useApp((store) => store.toast)
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [confirm, setConfirm] = useState('')
  const [busy, setBusy] = useState(false)
  const problem = next && next.length < 8 ? 'At least 8 characters.' : confirm && confirm !== next ? 'The passwords do not match.' : null

  const save = async () => {
    setBusy(true)
    try {
      await api.changePassword(current, next)
      toast('Password changed. Other sessions were signed out.')
      setCurrent('')
      setNext('')
      setConfirm('')
    } catch (error) {
      toast(error instanceof Error ? error.message : String(error), 'danger')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Panel title="Web interface password">
      <form
        className="grid gap-3 sm:grid-cols-3"
        onSubmit={(event) => {
          event.preventDefault()
          void save()
        }}
      >
        <Field label="Current">
          <Input type="password" autoComplete="current-password" value={current} onChange={(event) => setCurrent(event.target.value)} />
        </Field>
        <Field label="New">
          <Input type="password" autoComplete="new-password" value={next} onChange={(event) => setNext(event.target.value)} />
        </Field>
        <Field label="Repeat new">
          <Input type="password" autoComplete="new-password" value={confirm} onChange={(event) => setConfirm(event.target.value)} />
        </Field>
        <div className="flex items-center justify-between gap-3 sm:col-span-3">
          <p className="text-[12px] text-warn">{problem}</p>
          <Button type="submit" variant="primary" disabled={busy || !current || !next || next !== confirm || Boolean(problem)}>
            {busy ? <Spinner size={12} /> : <KeyRound size={13} />} Change password
          </Button>
        </div>
      </form>
    </Panel>
  )
}
