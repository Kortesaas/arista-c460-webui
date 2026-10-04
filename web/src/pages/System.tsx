import { useState } from 'react'
import { KeyRound } from 'lucide-react'
import { Page } from '@/app/Page'
import { LoadingState } from '@/components/Loading'
import { DisplaySettingsPanel, MaintenancePanel } from '@/components/SystemSettings'
import { RefreshPanel } from '@/components/RefreshSettings'
import { BackupPanel } from '@/components/Backup'
import { ViewerPanel } from '@/components/ViewerAccount'
import { ApiAccessPanel } from '@/components/ApiAccess'
import { Meter } from '@/components/status'
import { cn } from '@/ui/cn'
import { api } from '@/api'
import { useApp } from '@/stores/app'
import { Button, Field, Input, Panel, Spinner } from '@/ui/kit'
import { formatBytes, formatDuration } from '@/utils/format'

export function SystemPage() {
  const isAdmin = useApp((store) => store.role === 'admin')
  const state = useApp((store) => store.state)
  if (!state) return <LoadingState />
  const { device, hardware } = state
  const memUsed = device.memTotal ? ((device.memTotal - device.memAvailable) / device.memTotal) * 100 : null
  const flashUsed = device.storageTotal ? ((device.storageTotal - device.storageFree) / device.storageTotal) * 100 : null
  const facts: { label: string; value: string; mono?: boolean; tone?: 'warn' | 'danger' }[] = [
    { label: 'Model', value: device.model },
    { label: 'Firmware', value: device.firmware || '—', mono: true },
    { label: 'Serial number', value: hardware?.serial || '—', mono: true },
    { label: 'MAC address', value: device.mac, mono: true },
    { label: 'Uptime', value: formatDuration(device.uptimeSeconds) },
    {
      label: 'Temperature',
      value: device.temperatureC === null ? '—' : `${device.temperatureC.toFixed(0)} °C`,
      tone: device.temperatureC !== null && device.temperatureC > 85 ? 'danger' : device.temperatureC !== null && device.temperatureC > 75 ? 'warn' : undefined,
    },
    { label: 'Power source', value: hardware?.powerSource || '—' },
    { label: '6 GHz power class', value: hardware?.radioPower || '—' },
  ]
  return (
    <Page title="System" description="Device information, maintenance and access to this interface." width="settings">
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
        {facts.map((fact) => (
          <div key={fact.label} className="min-w-0 rounded-lg border border-line bg-surface px-3 py-2.5">
            <p className="truncate text-[11px] text-faint">{fact.label}</p>
            <p className={cn('mt-0.5 truncate text-[14px] font-semibold', fact.mono && 'mono', fact.tone === 'danger' ? 'text-danger' : fact.tone === 'warn' ? 'text-warn' : 'text-ink')} title={fact.value}>
              {fact.value}
            </p>
          </div>
        ))}
      </div>

      <Panel title="Resources" className="mt-3">
        <div className="grid gap-4 sm:grid-cols-3">
          <div>
            <Meter value={memUsed} label="Memory" warnAt={75} dangerAt={90} />
            <p className="tabular mt-1 text-[11px] text-faint">
              {formatBytes(device.memTotal - device.memAvailable)} of {formatBytes(device.memTotal)}
            </p>
          </div>
          <div>
            <Meter value={flashUsed} label="Flash storage" warnAt={75} dangerAt={90} />
            <p className="tabular mt-1 text-[11px] text-faint">{formatBytes(device.storageFree)} free</p>
          </div>
          <div>
            <Meter value={device.cpuUsage ?? null} label="CPU usage" warnAt={75} dangerAt={90} />
          </div>
        </div>
      </Panel>

      <div className="mt-3 grid items-start gap-3 xl:grid-cols-2">
        {/* Left: the device itself. Right: names, logins and the interface. */}
        <div className="min-w-0 space-y-3">
          <DisplaySettingsPanel state={state} />
          <MaintenancePanel state={state} />
          <BackupPanel />
          <RefreshPanel />
        </div>
        <div className="min-w-0 space-y-3">
          {isAdmin && <PasswordPanel />}
          <ViewerPanel />
          {isAdmin && <ApiAccessPanel />}
          <Panel title="About">
            <p className="text-[12px] leading-5 text-muted">
              ARRR-ISTA C460 web interface <span className="mono text-ink">{device.uiVersion}</span>. Runs on the access point and manages it through the AP’s own
              configuration agent; no controller or cloud service is involved.
            </p>
            <p className="mt-2 text-[11px] leading-4 text-faint">ARRR-ISTA is a parody. Unofficial project, not affiliated with or endorsed by Arista Networks.</p>
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
