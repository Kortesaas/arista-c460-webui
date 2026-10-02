import { useEffect, useState } from 'react'
import { Pencil, RefreshCw, Save } from 'lucide-react'
import { api } from '@/api'
import { useApp } from '@/stores/app'
import type { LldpState, LldpTiming } from '@/types'
import { Badge, Button, Dialog, DialogActions, Field, Input, KeyValue, Panel, Spinner } from '@/ui/kit'

export function LldpPanel() {
  const [settings, setSettings] = useState<LldpState | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [editing, setEditing] = useState(false)
  const load = async () => {
    setLoading(true)
    try {
      setSettings(await api.lldp())
      setError('')
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setLoading(false)
    }
  }
  useEffect(() => {
    void load()
  }, [])
  return (
    <Panel
      title="Switch discovery · LLDP"
      actions={
        <>
          <Button size="sm" disabled={loading} aria-label="Refresh LLDP" onClick={() => void load()}>
            {loading ? <Spinner size={12} /> : <RefreshCw size={12} />}
          </Button>
          <Button size="sm" disabled={!settings || loading || Boolean(error)} onClick={() => setEditing(true)}>
            <Pencil size={12} /> Edit
          </Button>
        </>
      }
    >
      {error ? (
        <p role="alert" className="text-[12px] text-danger">
          {error}
        </p>
      ) : !settings ? (
        <p className="text-[12px] text-muted">Reading switch discovery…</p>
      ) : (
        <>
          <KeyValue
            items={[
              { label: 'Advertisement interval', value: `${settings.interval} seconds` },
              { label: 'Hold multiplier', value: `${settings.hold} ×` },
              { label: 'Advertised lifetime', value: `${settings.interval * settings.hold} seconds` },
              { label: 'Timing settings', value: <Badge tone={settings.saved ? 'accent' : 'neutral'}>{settings.saved ? 'Saved on AP' : 'Firmware default'}</Badge> },
            ]}
          />
          <div className="mt-3 border-t border-line pt-3">
            {settings.neighborError ? (
              <p role="alert" className="text-[12px] text-warn">
                {settings.neighborError}
              </p>
            ) : !settings.neighbors.length ? (
              <p className="text-[12px] text-muted">No LLDP neighbours detected. The connected switch must advertise LLDP.</p>
            ) : (
              <div className="space-y-3">
                {settings.neighbors.map((peer, i) => (
                  <div key={`${peer.interface}-${peer.chassisId}-${i}`} className="rounded border border-line bg-surface-2 p-3">
                    <div className="mb-2 flex items-center justify-between gap-2">
                      <span className="min-w-0 break-all text-[13px] font-medium text-ink">{peer.name || peer.chassisId || 'Switch'}</span>
                      <Badge>{peer.interface}</Badge>
                    </div>
                    <KeyValue
                      items={[
                        { label: 'Switch port', value: peer.portDescription || peer.portId || '—' },
                        { label: 'Management address', value: peer.addresses.join(', ') || '—', mono: true },
                        { label: 'Chassis ID', value: peer.chassisId || '—', mono: true },
                        { label: 'Last change', value: peer.age || '—' },
                        { label: 'Lifetime', value: peer.ttl ? `${peer.ttl} seconds` : '—' },
                      ]}
                    />
                    {peer.description && <p className="mt-2 break-words text-[11px] leading-4 text-faint">{peer.description}</p>}
                  </div>
                ))}
              </div>
            )}
          </div>
          <p className="mt-3 text-[11px] leading-4 text-faint">LLDP identifies the neighbouring switch and port. Saved timing is restored when the local service starts.</p>
        </>
      )}
      {editing && settings && (
        <LldpDialog
          settings={settings}
          onClose={() => setEditing(false)}
          onSaved={() => {
            setEditing(false)
            void load()
          }}
        />
      )}
    </Panel>
  )
}
function LldpDialog({ settings, onClose, onSaved }: { settings: LldpTiming; onClose: () => void; onSaved: () => void }) {
  const [interval, setInterval] = useState(String(settings.interval))
  const [hold, setHold] = useState(String(settings.hold))
  const [busy, setBusy] = useState(false)
  const toast = useApp((s) => s.toast)
  const timing = { interval: Number(interval), hold: Number(hold) }
  const valid = Number.isInteger(timing.interval) && timing.interval >= 5 && timing.interval <= 3600 && Number.isInteger(timing.hold) && timing.hold >= 2 && timing.hold <= 10
  const save = async () => {
    if (!valid || busy) return
    setBusy(true)
    try {
      await api.updateLldp(timing)
      toast('LLDP timing saved.', 'ok')
      onSaved()
    } catch (e) {
      toast(e instanceof Error ? e.message : String(e), 'danger')
    } finally {
      setBusy(false)
    }
  }
  return (
    <Dialog
      title="Switch discovery timing"
      description="Set how often the AP advertises itself and how long the switch keeps its information."
      onClose={() => {
        if (!busy) onClose()
      }}
    >
      <form
        onSubmit={(e) => {
          e.preventDefault()
          void save()
        }}
      >
        <fieldset disabled={busy} className="grid gap-3 sm:grid-cols-2">
          <Field label="Advertisement interval" hint="5–3600 seconds">
            <Input type="number" min={5} max={3600} required value={interval} onChange={(e) => setInterval(e.target.value)} />
          </Field>
          <Field label="Hold multiplier" hint="2–10 advertisements">
            <Input type="number" min={2} max={10} required value={hold} onChange={(e) => setHold(e.target.value)} />
          </Field>
        </fieldset>
        <div className="mt-4 rounded border border-line bg-surface-2 px-3 py-2 text-[12px] text-muted">
          Advertised lifetime <span className="tabular float-right font-medium text-ink">{valid ? `${timing.interval * timing.hold} seconds` : '—'}</span>
        </div>
        <p className="mt-3 text-[11px] leading-4 text-faint">Applies immediately and is saved for future starts.</p>
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
