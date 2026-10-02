import { useEffect, useState } from 'react'
import { RefreshCw, Save } from 'lucide-react'
import { api } from '@/api'
import { useApp } from '@/stores/app'
import type { SsidFeatures } from '@/types'
import { Badge, Button, Dialog, DialogActions, Field, Select, Spinner } from '@/ui/kit'

const choice = (v: boolean | null) => (v === null ? 'default' : v ? 'enabled' : 'disabled')
const value = (v: string) => (v === 'default' ? null : v === 'enabled')
const operating = (v: boolean | null) => (v === null ? 'Unavailable' : v ? 'Enabled' : 'Disabled')
export function WirelessFeaturesDialog({ name, onClose }: { name: string; onClose: () => void }) {
  const [settings, setSettings] = useState<SsidFeatures | null>(null)
  const [rrm, setRrm] = useState('default')
  const [load, setLoad] = useState('default')
  const [busy, setBusy] = useState(false)
  const [reading, setReading] = useState(true)
  const [error, setError] = useState('')
  const change = useApp((s) => s.change)
  const read = async () => {
    setReading(true)
    setError('')
    try {
      const data = await api.ssidFeatures(name)
      setSettings(data)
      setRrm(choice(data.rrm))
      setLoad(choice(data.load))
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setReading(false)
    }
  }
  useEffect(() => {
    void read()
  }, [name])
  const save = async () => {
    if (busy || reading || !settings) return
    setBusy(true)
    const ok = await change(`Saved advanced settings for ${name}`, () => api.updateSsidFeatures(name, value(rrm), value(load)))
    setBusy(false)
    if (ok) onClose()
  }
  const unchanged = settings && choice(settings.rrm) === rrm && choice(settings.load) === load
  return (
    <Dialog
      title={`Advanced · ${name}`}
      description="Radio measurements and load information for compatible clients."
      onClose={() => {
        if (!busy) onClose()
      }}
    >
      {error && (
        <p role="alert" className="mb-3 text-[12px] text-danger">
          {error}
        </p>
      )}
      {reading ? (
        <div className="flex items-center gap-2 py-5 text-[12px] text-muted">
          <Spinner size={14} /> Reading wireless settings…
        </div>
      ) : (
        settings && (
          <>
            <fieldset disabled={busy} className="space-y-4">
              <FeatureChoice label="802.11k radio measurements" hint="Allows clients to request radio measurements to help with roaming." selected={rrm} onChange={setRrm} />
              <FeatureChoice label="BSS load advertising" hint="Advertises client count and channel utilisation so clients can choose an AP." selected={load} onChange={setLoad} />
            </fieldset>
            <div className="mt-4 overflow-hidden rounded border border-line">
              <div className="flex items-center justify-between border-b border-line bg-surface-2 px-3 py-2">
                <span className="text-[11px] font-medium text-muted">Currently running</span>
                <Badge>Driver readback</Badge>
              </div>
              {settings.interfaces.length ? (
                <table className="w-full text-left text-[12px]">
                  <thead className="text-faint">
                    <tr>
                      <th className="px-3 py-2 font-medium">Band</th>
                      <th className="px-3 py-2 font-medium">802.11k</th>
                      <th className="px-3 py-2 font-medium">BSS load</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-line">
                    {settings.interfaces.map((row) => (
                      <tr key={row.interface}>
                        <td className="px-3 py-2 text-muted">{row.band ? `${row.band} GHz` : row.interface}</td>
                        <td className="px-3 py-2 text-muted">{operating(row.rrm)}</td>
                        <td className="px-3 py-2 text-muted">{operating(row.load)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              ) : (
                <p className="p-3 text-[12px] text-muted">No active interfaces could be read. The network may be disabled or restarting.</p>
              )}
              {settings.interfaces.some((row) => row.error) && <p className="px-3 pb-3 text-[11px] text-warn">Some driver values could not be read. Refresh to try again.</p>}
            </div>
            <p className="mt-3 text-[11px] leading-4 text-faint">
              Firmware default keeps the AP’s own choice. Saving briefly restarts Wi-Fi; running values update after the radios return.
            </p>
          </>
        )
      )}
      <DialogActions>
        <Button disabled={busy || reading} onClick={() => void read()}>
          <RefreshCw size={13} /> Refresh
        </Button>
        <div className="flex-1" />
        <Button disabled={busy} onClick={onClose}>
          Cancel
        </Button>
        <Button variant="primary" disabled={busy || reading || !settings || Boolean(error) || Boolean(unchanged)} onClick={() => void save()}>
          {busy ? <Spinner size={13} /> : <Save size={13} />} Save
        </Button>
      </DialogActions>
    </Dialog>
  )
}
function FeatureChoice({ label, hint, selected, onChange }: { label: string; hint: string; selected: string; onChange: (v: string) => void }) {
  return (
    <Field label={label} hint={hint}>
      <Select value={selected} onChange={(e) => onChange(e.target.value)}>
        <option value="default">Firmware default</option>
        <option value="enabled">Enabled</option>
        <option value="disabled">Disabled</option>
      </Select>
    </Field>
  )
}
