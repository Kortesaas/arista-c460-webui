import { useState } from 'react'
import { Save, Wifi } from 'lucide-react'
import { api } from '@/api'
import { useApp } from '@/stores/app'
import type { Radio, WiFi7Settings } from '@/types'
import { Badge, Button, Field, Segmented, Spinner, Toggle } from '@/ui/kit'

export function Wifi7Settings({ radio }: { radio: Radio }) {
  const state = radio.wifi7
  const change = useApp((s) => s.change)
  const [form, setForm] = useState<WiFi7Settings>(() => state?.saved ?? { enabled: false, width: 320 })
  const [busy, setBusy] = useState(false)
  if (!state?.supported) return null
  const initial = state.saved ?? { enabled: false, width: 320 }
  const dirty = form.enabled !== initial.enabled || form.width !== initial.width
  const active = state.operatingMode.startsWith('11AEHT')
  const save = async () => {
    setBusy(true)
    await change('6 GHz Wi-Fi mode updated', () => api.updateWiFi7(radio.id, form))
    setBusy(false)
  }
  return (
    <div className="space-y-3 rounded border border-line bg-surface-2 p-3">
      <div className="flex items-center justify-between gap-2">
        <h3 className="flex items-center gap-2 text-[12px] font-semibold text-ink"><Wifi size={14} /> Wi-Fi 7</h3>
        <Badge tone={state.error ? 'warn' : active ? 'ok' : 'neutral'}>
          {state.error ? 'Needs attention' : state.status === 'pending' ? 'Applying' : active ? 'Active' : 'Wi-Fi 6E'}
        </Badge>
      </div>
      <p className="text-[11px] leading-4 text-muted">Applies to every network on 6 GHz. Use a 6 GHz-only network to test without switching bands.</p>
      <Toggle checked={form.enabled} onChange={(enabled) => setForm((f) => ({ ...f, enabled }))} label="Enable Wi-Fi 7" help="Saved on the AP and restored after configuration changes and at startup. Switching mode briefly interrupts 6 GHz Wi-Fi." />
      {form.enabled && (
        <Field label="Wi-Fi 7 channel width" help="320 MHz offers the highest peak speed near the AP. 160 MHz uses less spectrum and may perform better with interference.">
          <Segmented value={form.width} onChange={(width) => setForm((f) => ({ ...f, width }))} options={[{ value: 160, label: '160 MHz' }, { value: 320, label: '320 MHz · maximum' }]} className="w-full" />
        </Field>
      )}
      <p className="text-[11px] text-muted">Operating now: <span className="font-medium text-ink">{state.operatingMode ? `${active ? 'Wi-Fi 7' : 'Wi-Fi 6E'} · ${state.operatingWidth} MHz` : 'Radio unavailable'}</span></p>
      {state.error && <p role="alert" className="text-[11px] leading-4 text-danger">{state.error}</p>}
      <div className="flex justify-end">
        <Button write variant="primary" size="sm" disabled={(!dirty && !state.error) || busy || !radio.enabled} onClick={() => void save()}>
          {busy ? <Spinner size={12} /> : <Save size={12} />} {busy ? 'Verifying radio…' : 'Apply Wi-Fi mode'}
        </Button>
      </div>
      {!radio.enabled && <p className="text-[11px] text-muted">Enable the radio above before changing its Wi-Fi mode.</p>}
    </div>
  )
}
