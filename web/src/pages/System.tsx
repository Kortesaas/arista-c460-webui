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
  const { toast, username: currentUser, signOut } = useApp()
  const [current, setCurrent] = useState('')
  const [username, setUsername] = useState(currentUser)
  const [next, setNext] = useState('')
  const [confirm, setConfirm] = useState('')
  const [busy, setBusy] = useState(false)
  const problem = !/^[A-Za-z0-9._-]{1,32}$/.test(username)
    ? "Username: 1–32 letters, digits, '.', '_' or '-'."
    : next && next.length < 6
      ? 'The new password needs at least 6 characters.'
      : confirm && confirm !== next
        ? 'The passwords do not match.'
        : null

  const save = async () => {
    setBusy(true)
    try {
      await api.changeCredentials(current, username, next)
      toast('Administrator account updated. Please sign in again.')
      await signOut()
    } catch (error) {
      toast(error instanceof Error ? error.message : String(error), 'danger')
      setBusy(false)
    }
  }

  return (
    <Panel title="Administrator account">
      <form
        className="grid gap-3 sm:grid-cols-2"
        onSubmit={(event) => {
          event.preventDefault()
          void save()
        }}
      >
        <Field label="Username">
          <Input autoComplete="username" autoCapitalize="none" spellCheck={false} value={username} onChange={(event) => setUsername(event.target.value)} />
        </Field>
        <Field label="Current password">
          <Input type="password" autoComplete="current-password" value={current} onChange={(event) => setCurrent(event.target.value)} />
        </Field>
        <Field label="New password">
          <Input type="password" autoComplete="new-password" value={next} onChange={(event) => setNext(event.target.value)} />
        </Field>
        <Field label="Repeat new password">
          <Input type="password" autoComplete="new-password" value={confirm} onChange={(event) => setConfirm(event.target.value)} />
        </Field>
        <div className="flex items-center justify-between gap-3 sm:col-span-2">
          <p className="text-[12px] text-warn">{problem}</p>
          <Button type="submit" variant="primary" disabled={busy || !current || !next || next !== confirm || Boolean(problem)}>
            {busy ? <Spinner size={12} /> : <KeyRound size={13} />} Save account
          </Button>
        </div>
      </form>
    </Panel>
  )
}
