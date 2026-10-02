import { useEffect, useState } from 'react'
import { Lightbulb, Pencil, Plus, Power, Save, Trash2 } from 'lucide-react'
import { api } from '@/api'
import { useApp } from '@/stores/app'
import type { ApState, Management, ManagementInput, TrustCheck } from '@/types'
import { Badge, Button, Dialog, DialogActions, Field, HelpTip, Input, KeyValue, Panel, Select, Spinner } from '@/ui/kit'

function useSystemAction() {
  const [busy, setBusy] = useState(false)
  const toast = useApp((s) => s.toast)
  const refresh = useApp((s) => s.refresh)
  const run = async (operation: () => Promise<unknown>, message: string | (() => string)) => {
    setBusy(true)
    try {
      await operation()
      toast(typeof message === 'function' ? message() : message, 'ok')
      void refresh()
      return true
    } catch (error) {
      toast(error instanceof Error ? error.message : String(error), 'danger')
      return false
    } finally {
      setBusy(false)
    }
  }
  return { busy, run }
}

export function ManagementPanel({ state }: { state: ApState }) {
  const [editing, setEditing] = useState(false)
  const [saved, setSaved] = useState<ManagementInput | null>(null)
  const m = state.management
  const ready = Boolean(m?.mode && m.commVlan && !state.managementError)
  return (
    <Panel
      title="Management network"
      actions={
        <Button size="sm" disabled={!ready} onClick={() => setEditing(true)}>
          <Pencil size={12} />
          Edit
        </Button>
      }
    >
      {!ready ? (
        <p className="text-[12px] text-muted">{state.managementError || 'Reading management settings…'}</p>
      ) : (
        <>
          <KeyValue
            items={[
              { label: 'Address mode', value: m.mode === 'dhcp' ? 'Automatic (DHCP client)' : 'Static IPv4' },
              { label: 'Management VLAN', value: m.commVlan === 'untagged' ? 'Native / untagged' : m.commVlan },
              { label: 'Configured IP', value: m.ipv4 || 'Assigned by DHCP', mono: true },
              { label: 'Subnet mask', value: m.netmask || '—', mono: true },
              { label: 'Gateway', value: m.gateway || '—', mono: true },
              { label: 'DNS servers', value: m.dns?.join(', ') || 'Provided by DHCP', mono: true },
              { label: 'Search domain', value: m.dnsSearch || '—', mono: true },
              { label: 'Running IP', value: state.device.mgmtIp || '—', mono: true },
            ]}
          />
          {(saved || m.pendingReboot) && (
            <div className="mt-3 rounded border border-warn bg-warn-soft p-2 text-[12px] text-warn">
              Saved network settings need an AP restart.
              {(saved?.mode === 'static' ? saved.ipv4 : !saved && m.mode === 'static' ? m.ipv4 : '') && (
                <p className="mt-1">
                  After restart:{' '}
                  <a className="underline" href={`http://${saved?.ipv4 || m.ipv4}/`}>
                    {saved?.ipv4 || m.ipv4}
                  </a>
                </p>
              )}
              {saved?.mode === 'dhcp' && <p className="mt-1">Find the AP’s new address in your router’s DHCP leases.</p>}
            </div>
          )}
        </>
      )}
      {editing && (
        <ManagementDialog
          management={m}
          onClose={() => setEditing(false)}
          onSaved={(input) => {
            setSaved(input)
            setEditing(false)
          }}
        />
      )}
    </Panel>
  )
}

function ipv4(value: string): number | null {
  if (!/^(0|[1-9]\d{0,2})(\.(0|[1-9]\d{0,2})){3}$/.test(value)) return null
  const parts = value.split('.').map(Number)
  if (parts.some((part) => part > 255)) return null
  return parts.reduce((n, part) => ((n << 8) | part) >>> 0, 0)
}

function managementProblem(form: ManagementInput, dns: string[]) {
  if (form.commVlan !== 'untagged' && (!/^[1-9]\d{0,3}$/.test(form.commVlan) || Number(form.commVlan) > 4094)) return 'Management VLAN must be 1–4094, or native / untagged.'
  if (form.mode === 'dhcp') return null
  const ip = ipv4(form.ipv4),
    mask = ipv4(form.netmask),
    gateway = ipv4(form.gateway)
  if (ip === null) return 'Enter a valid IPv4 address.'
  if (mask === null) return 'Enter a valid subnet mask.'
  const inverse = ~mask >>> 0
  if ((inverse & (inverse + 1)) !== 0 || mask < 0xff000000 || mask > 0xfffffffc) return 'Subnet mask must be contiguous, from /8 to /30.'
  const network = (ip & mask) >>> 0,
    broadcast = (network | inverse) >>> 0
  if (ip === network || ip === broadcast || ip >>> 24 === 127 || ip >>> 24 >= 224 || ip === 0) return 'Use a host address, not a network, broadcast, loopback or multicast address.'
  if (gateway === null || (gateway & mask) >>> 0 !== network || gateway === ip || gateway === network || gateway === broadcast)
    return 'Gateway must be another host address in the same subnet.'
  if (!dns[0]) return 'Enter a primary DNS server.'
  if (dns.some((value) => value && ipv4(value) === null)) return 'Enter valid IPv4 DNS server addresses.'
  if (form.dnsSearch && (form.dnsSearch.length > 253 || !/^[a-z\d](?:[a-z\d-]{0,61}[a-z\d])?(?:\.[a-z\d](?:[a-z\d-]{0,61}[a-z\d])?)*$/i.test(form.dnsSearch)))
    return 'Enter a valid DNS search domain.'
  return null
}

function ManagementDialog({ management: m, onClose, onSaved }: { management: Management; onClose: () => void; onSaved: (input: ManagementInput) => void }) {
  const [form, setForm] = useState<ManagementInput>({
    commVlan: m.commVlan,
    mode: m.mode === 'dhcp' ? 'dhcp' : 'static',
    ipv4: m.ipv4,
    netmask: m.netmask || '255.255.255.0',
    gateway: m.gateway,
    dns: [],
    dnsSearch: m.dnsSearch,
  })
  const [dns, setDNS] = useState([m.dns?.[0] ?? '', m.dns?.[1] ?? '', m.dns?.[2] ?? ''])
  const { busy, run } = useSystemAction()
  const problem = managementProblem(form, dns)
  const update = (key: keyof ManagementInput, value: string) => setForm((current) => ({ ...current, [key]: value }))
  const save = async () => {
    if (problem || busy) return
    const input = { ...form, dns: dns.filter(Boolean) }
    let needsRestart = false
    if (
      await run(
        async () => {
          needsRestart = (await api.updateManagement(input)).rebootRequired
        },
        () => (needsRestart ? 'Management settings saved. Restart the AP to apply them.' : 'Management settings are already up to date.'),
      )
    ) {
      if (needsRestart) onSaved(input)
      else onClose()
    }
  }
  return (
    <Dialog
      title="Management network"
      description="Save the AP’s address and DNS settings, then restart when ready."
      onClose={() => {
        if (!busy) onClose()
      }}
      wide
    >
      <form
        onSubmit={(event) => {
          event.preventDefault()
          void save()
        }}
      >
        <fieldset disabled={busy} className="grid gap-3 sm:grid-cols-2">
          <Field label="Address mode">
            <Select value={form.mode} onChange={(e) => update('mode', e.target.value)}>
              <option value="static">Static IPv4</option>
              <option value="dhcp">Automatic (DHCP client)</option>
            </Select>
          </Field>
          <Field label="Management VLAN" help="Keep native / untagged unless your switch port sends the AP's management traffic tagged in a specific VLAN.">
            <div className="flex gap-2">
              <Select
                aria-label="Management VLAN mode"
                value={form.commVlan === 'untagged' ? 'untagged' : 'tagged'}
                onChange={(e) => update('commVlan', e.target.value === 'untagged' ? 'untagged' : '99')}
              >
                <option value="untagged">Native / untagged</option>
                <option value="tagged">Tagged VLAN</option>
              </Select>
              {form.commVlan !== 'untagged' && (
                <Input aria-label="Management VLAN ID" className="max-w-24" inputMode="numeric" value={form.commVlan} onChange={(e) => update('commVlan', e.target.value)} />
              )}
            </div>
          </Field>
          {form.mode === 'static' ? (
            <>
              <Field label="IPv4 address">
                <Input value={form.ipv4} autoComplete="off" onChange={(e) => update('ipv4', e.target.value.trim())} />
              </Field>
              <Field label="Subnet mask">
                <Input value={form.netmask} onChange={(e) => update('netmask', e.target.value.trim())} />
              </Field>
              <Field label="Gateway IP">
                <Input value={form.gateway} onChange={(e) => update('gateway', e.target.value.trim())} />
              </Field>
              <Field label="DNS search domain" hint="Optional, e.g. example.lan">
                <Input value={form.dnsSearch} onChange={(e) => update('dnsSearch', e.target.value.trim())} />
              </Field>
              {dns.map((value, index) => (
                <Field key={index} label={`${['Primary', 'Secondary', 'Tertiary'][index]} DNS`} hint={index ? 'Optional.' : undefined}>
                  <Input value={value} onChange={(e) => setDNS((current) => current.map((entry, i) => (i === index ? e.target.value.trim() : entry)))} />
                </Field>
              ))}
            </>
          ) : (
            <p className="text-[12px] leading-5 text-muted sm:col-span-2">
              The router supplies the AP’s address, gateway and DNS. The AP remains a DHCP client; it does not provide DHCP to wireless clients.
            </p>
          )}
        </fieldset>
        <p className="mt-4 text-[12px] leading-5 text-muted">
          Changing the IP or management VLAN can move this interface to a different address or network after restart. Keep the switch port’s VLAN configuration in mind.
        </p>
        {problem && (
          <p role="alert" className="mt-2 text-[12px] text-warn">
            {problem}
          </p>
        )}
        <DialogActions>
          <Button disabled={busy} onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" disabled={busy || Boolean(problem)}>
            {busy ? <Spinner /> : <Save size={13} />}Save network settings
          </Button>
        </DialogActions>
      </form>
    </Dialog>
  )
}

export function DisplaySettingsPanel({ state }: { state: ApState }) {
  const [editing, setEditing] = useState(false)
  return (
    <Panel
      title="Names and labels"
      actions={
        <Button size="sm" onClick={() => setEditing(true)}>
          <Pencil size={12} />
          Edit
        </Button>
      }
    >
      <KeyValue
        items={[
          { label: 'Device name', value: state.device.siteName || state.device.hostname },
          ...Object.entries(state.vlanNames ?? {})
            .sort(([a], [b]) => Number(a) - Number(b))
            .map(([id, name]) => ({ label: `VLAN ${id}`, value: name })),
        ]}
      />
      {editing && <LabelsDialog state={state} onClose={() => setEditing(false)} />}
    </Panel>
  )
}

function LabelsDialog({ state, onClose }: { state: ApState; onClose: () => void }) {
  const [name, setName] = useState(state.device.siteName)
  const [rows, setRows] = useState(Object.entries(state.vlanNames ?? {}).map(([id, label]) => ({ id, label })))
  const { busy, run } = useSystemAction()
  const ids = rows.map((row) => row.id)
  const problem =
    name.length > 48
      ? 'Device name: up to 48 characters.'
      : rows.some((row) => !/^[1-9]\d{0,3}$/.test(row.id) || Number(row.id) > 4094 || row.label.length > 24)
        ? 'VLAN IDs: 1–4094. Labels: up to 24 characters.'
        : new Set(ids).size !== ids.length
          ? 'Each VLAN ID can appear only once.'
          : null
  return (
    <Dialog
      title="Names and labels"
      description="These names are displayed in this interface."
      onClose={() => {
        if (!busy) onClose()
      }}
    >
      <form
        onSubmit={(event) => {
          event.preventDefault()
          if (!problem && !busy)
            void run(() => api.updateSettings(name, Object.fromEntries(rows.map((row) => [row.id, row.label]))), 'Names and labels saved.').then((ok) => {
              if (ok) onClose()
            })
        }}
      >
        <fieldset disabled={busy} className="space-y-3">
          <Field label="Device name" hint="Leave empty to display the AP hostname.">
            <Input maxLength={48} value={name} onChange={(e) => setName(e.target.value)} />
          </Field>
          {rows.map((row, index) => (
            <div key={index} className="flex items-end gap-2">
              <Field label="VLAN ID" className="w-20">
                <Input
                  inputMode="numeric"
                  value={row.id}
                  onChange={(e) => setRows((current) => current.map((entry, i) => (i === index ? { ...entry, id: e.target.value } : entry)))}
                />
              </Field>
              <Field label="Label" className="flex-1">
                <Input
                  maxLength={24}
                  value={row.label}
                  onChange={(e) => setRows((current) => current.map((entry, i) => (i === index ? { ...entry, label: e.target.value } : entry)))}
                />
              </Field>
              <Button aria-label={`Remove VLAN label ${row.id || index + 1}`} onClick={() => setRows((current) => current.filter((_, i) => i !== index))}>
                <Trash2 size={13} />
              </Button>
            </div>
          ))}
          <Button onClick={() => setRows((current) => [...current, { id: '', label: '' }])}>
            <Plus size={13} />
            Add VLAN label
          </Button>
        </fieldset>
        {problem && (
          <p role="alert" className="mt-3 text-[12px] text-warn">
            {problem}
          </p>
        )}
        <DialogActions>
          <Button disabled={busy} onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" disabled={busy || Boolean(problem)}>
            {busy && <Spinner />}Save labels
          </Button>
        </DialogActions>
      </form>
    </Dialog>
  )
}

export function MaintenancePanel({ state }: { state: ApState }) {
  const { busy, run } = useSystemAction()
  const [dialog, setDialog] = useState<'ssh' | 'reboot' | null>(null)
  const [minutes, setMinutes] = useState('1')
  return (
    <Panel title="Access and maintenance">
      <div className="flex items-center justify-between gap-3">
        <div>
          <p className="flex items-center gap-1.5 text-[13px] text-ink">
            SSH access <Badge tone={state.device.sshEnabled ? 'ok' : 'neutral'}>{state.device.sshEnabled ? 'Enabled' : 'Disabled'}</Badge>
            <HelpTip label="SSH access">Command-line access to the AP for administration and recovery. Disabling it also closes open SSH sessions.</HelpTip>
          </p>
        </div>
        <Button disabled={busy} onClick={() => setDialog('ssh')}>
          {state.device.sshEnabled ? 'Disable SSH' : 'Enable SSH'}
        </Button>
      </div>
      <div className="mt-4 border-t border-line pt-3">
        <p className="mb-2 flex items-center gap-1.5 text-[13px] text-ink">
          Locate this AP
          <HelpTip label="Locate">Blinks the AP's LEDs so you can find it on the ceiling or in a rack.</HelpTip>
        </p>
        <div className="flex flex-wrap items-end gap-2">
          <Field label="Blink duration">
            <Select value={minutes} onChange={(e) => setMinutes(e.target.value)}>
              {[1, 5, 10, 30].map((n) => (
                <option key={n} value={n}>
                  {n} {n === 1 ? 'minute' : 'minutes'}
                </option>
              ))}
            </Select>
          </Field>
          <Button disabled={busy} onClick={() => void run(() => api.locate(Number(minutes)), 'AP location LEDs started.')}>
            <Lightbulb size={13} />
            Blink LEDs
          </Button>
          <Button disabled={busy} onClick={() => void run(api.stopLocate, 'AP location LEDs stopped.')}>
            Stop blinking
          </Button>
        </div>
      </div>
      <div className="mt-4 flex items-center justify-between gap-3 border-t border-line pt-3">
        <p className="flex items-center gap-1.5 text-[13px] text-ink">
          Restart access point
          <HelpTip label="Restart">
            Applies saved management network settings. Before restarting, the AP's boot safety check runs; the restart is refused if the firmware would erase local changes.
          </HelpTip>
        </p>
        <Button disabled={busy} onClick={() => setDialog('reboot')}>
          <Power size={13} />
          Restart AP
        </Button>
      </div>
      {dialog === 'ssh' && (
        <Dialog
          title={state.device.sshEnabled ? 'Disable SSH access?' : 'Enable SSH access?'}
          description={
            state.device.sshEnabled
              ? 'Existing SSH connections may close, and SSH management will be unavailable until you enable it again.'
              : 'Enable the AP’s SSH service using its existing account configuration.'
          }
          onClose={() => {
            if (!busy) setDialog(null)
          }}
        >
          <DialogActions>
            <Button disabled={busy} onClick={() => setDialog(null)}>
              Cancel
            </Button>
            <Button
              variant={state.device.sshEnabled ? 'danger' : 'primary'}
              disabled={busy}
              onClick={() =>
                void run(() => api.updateSSH(!state.device.sshEnabled), 'SSH setting saved. Applying the change may briefly restart Wi-Fi.').then((ok) => {
                  if (ok) setDialog(null)
                })
              }
            >
              {busy && <Spinner />}Confirm SSH change
            </Button>
          </DialogActions>
        </Dialog>
      )}
      {dialog === 'reboot' && <RebootDialog management={state.management} onClose={() => setDialog(null)} />}
    </Panel>
  )
}

function RebootDialog({ management, onClose }: { management: Management; onClose: () => void }) {
  const [check, setCheck] = useState<TrustCheck | null>(null)
  const [error, setError] = useState('')
  const { busy, run } = useSystemAction()
  useEffect(() => {
    let stopped = false
    void api
      .trust()
      .then((result) => {
        if (!stopped) setCheck(result)
      })
      .catch((err: unknown) => {
        if (!stopped) setError(err instanceof Error ? err.message : String(err))
      })
    return () => {
      stopped = true
    }
  }, [])
  return (
    <Dialog
      title="Restart access point?"
      description="Wi-Fi and this interface will be unavailable while the AP restarts."
      onClose={() => {
        if (!busy) onClose()
      }}
    >
      <div className="space-y-2 text-[12px] leading-5 text-muted">
        {!check && !error && (
          <p className="flex items-center gap-2">
            <Spinner />
            Checking whether the AP can restart safely…
          </p>
        )}
        {error && (
          <p role="alert" className="text-danger">
            {error}
          </p>
        )}
        {check?.ok && <p>The AP’s boot checks passed.</p>}
        {check && !check.ok && (
          <div role="alert" className="text-danger">
            <p>Restart blocked: the firmware would erase the local installation.</p>
            <ul className="mt-2 list-disc pl-4">
              {check.problems?.map((problem) => (
                <li key={problem}>{problem}</li>
              ))}
            </ul>
          </div>
        )}
        {management?.mode === 'static' ? (
          <p>
            After restart, open{' '}
            <a className="text-accent-text underline" href={`http://${management.ipv4}/`}>
              {management.ipv4}
            </a>
            . Your computer must be able to reach the management VLAN.
          </p>
        ) : (
          <p>With DHCP, check your router’s leases for the AP’s address after restart.</p>
        )}
      </div>
      <DialogActions>
        <Button disabled={busy} onClick={onClose}>
          Cancel
        </Button>
        <Button
          variant="danger"
          disabled={busy || !check?.ok}
          onClick={() =>
            void run(api.reboot, 'AP restart requested. Reconnect at its configured management address.').then((ok) => {
              if (ok) onClose()
            })
          }
        >
          {busy ? <Spinner /> : <Power size={13} />}Restart now
        </Button>
      </DialogActions>
    </Dialog>
  )
}

export function HardwarePanel({ state }: { state: ApState }) {
  const hw = state.hardware
  return (
    <Panel title="Hardware">
      <KeyValue
        items={[
          { label: 'Serial number', value: hw?.serial || '—', mono: true },
          { label: 'Power source', value: hw?.powerSource || '—' },
          { label: '6 GHz power class', value: hw?.radioPower || '—' },
          { label: 'Clock sync', value: hw?.ntpSynced === null || hw?.ntpSynced === undefined ? 'Unknown' : hw.ntpSynced ? 'Synchronised (NTP)' : 'Not synchronised' },
        ]}
      />
    </Panel>
  )
}
