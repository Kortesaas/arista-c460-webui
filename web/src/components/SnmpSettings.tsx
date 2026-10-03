import { useEffect, useState } from 'react'
import { Pencil, Save } from 'lucide-react'
import { api } from '@/api'
import { useApp } from '@/stores/app'
import type { SnmpSettings, SnmpStatus } from '@/types'
import { Badge, Button, Dialog, DialogActions, Field, Input, KeyValue, Panel, Spinner, Toggle } from '@/ui/kit'

export function SnmpPanel() {
  const [status, setStatus] = useState<SnmpStatus | null>(null)
  const [error, setError] = useState('')
  const [editing, setEditing] = useState(false)
  const mgmtIp = useApp((s) => s.state?.device.mgmtIp)

  useEffect(() => {
    api
      .snmp()
      .then(setStatus)
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [])

  return (
    <Panel
      title="Monitoring · SNMP"
      help="Lets network monitors poll this AP like a switch: name, uptime, location and both Ethernet ports with traffic counters (MIB-II system, ifTable and ifXTable). Read-only over SNMP v1 and v2c on UDP port 161; nothing can be changed through SNMP."
      actions={
        <Button size="sm" write disabled={!status} onClick={() => setEditing(true)}>
          <Pencil size={12} /> Edit
        </Button>
      }
    >
      {error ? (
        <p role="alert" className="text-[12px] text-danger">
          {error}
        </p>
      ) : !status ? (
        <p className="text-[12px] text-muted">Reading SNMP settings…</p>
      ) : (
        <>
          <KeyValue
            items={[
              {
                label: 'Agent',
                value: status.enabled ? (
                  <Badge tone={status.running ? 'ok' : 'danger'}>{status.running ? 'Running, read-only' : 'Not running'}</Badge>
                ) : (
                  <Badge>Off</Badge>
                ),
              },
              ...(status.enabled
                ? [
                    { label: 'Poll address', value: mgmtIp ? `udp://${mgmtIp}:161` : 'UDP port 161', mono: true },
                    { label: 'Community', value: '•'.repeat(Math.min(status.community.length, 12)) },
                    { label: 'Location', value: status.location || '—' },
                    { label: 'Contact', value: status.contact || '—' },
                  ]
                : []),
            ]}
          />
          {status.error && (
            <p role="alert" className="mt-2 text-[12px] text-danger">
              {status.error}
            </p>
          )}
        </>
      )}
      {editing && status && (
        <SnmpDialog
          settings={status}
          onClose={() => setEditing(false)}
          onSaved={(next) => {
            setStatus(next)
            setEditing(false)
          }}
        />
      )}
    </Panel>
  )
}

function SnmpDialog({ settings, onClose, onSaved }: { settings: SnmpSettings; onClose: () => void; onSaved: (status: SnmpStatus) => void }) {
  const toast = useApp((s) => s.toast)
  const [form, setForm] = useState<SnmpSettings>({ enabled: settings.enabled, community: settings.community, location: settings.location, contact: settings.contact })
  const [busy, setBusy] = useState(false)
  const set = <K extends keyof SnmpSettings>(key: K, value: SnmpSettings[K]) => setForm((f) => ({ ...f, [key]: value }))
  const valid = !form.enabled || (form.community.length > 0 && form.community.trim() === form.community)

  const save = async () => {
    if (!valid || busy) return
    setBusy(true)
    try {
      const next = await api.updateSnmp(form)
      if (next.error) toast(next.error, 'danger')
      else toast(form.enabled ? 'SNMP agent running.' : 'SNMP agent turned off.', 'ok')
      onSaved(next)
    } catch (e) {
      toast(e instanceof Error ? e.message : String(e), 'danger')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog title="SNMP monitoring" description="Read-only access for network monitors. Applies immediately and is kept across restarts." onClose={() => !busy && onClose()}>
      <form
        onSubmit={(e) => {
          e.preventDefault()
          void save()
        }}
      >
        <fieldset disabled={busy} className="space-y-3">
          <Toggle checked={form.enabled} onChange={(v) => set('enabled', v)} label="SNMP agent enabled" />
          <Field
            label="Community"
            help="Works like a shared password between the AP and the monitor. SNMP v1/v2c sends it unencrypted, so use the same value as your other devices and keep SNMP on the management network."
          >
            <Input value={form.community} maxLength={64} autoComplete="off" spellCheck={false} onChange={(e) => set('community', e.target.value)} placeholder="public" />
          </Field>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="Location" hint="Reported as sysLocation">
              <Input value={form.location} maxLength={255} onChange={(e) => set('location', e.target.value)} placeholder="Stage left, truss 2" />
            </Field>
            <Field label="Contact" hint="Reported as sysContact">
              <Input value={form.contact} maxLength={255} onChange={(e) => set('contact', e.target.value)} placeholder="noc@example.com" />
            </Field>
          </div>
        </fieldset>
        <DialogActions>
          <Button disabled={busy} onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" disabled={busy || !valid}>
            {busy ? <Spinner size={13} /> : <Save size={13} />} Save
          </Button>
        </DialogActions>
      </form>
    </Dialog>
  )
}
