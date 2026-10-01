import { useState } from 'react'
import { RotateCcw, Save } from 'lucide-react'
import { Page } from '@/app/Page'
import { LoadingState } from '@/components/Loading'
import { BandChip, Meter } from '@/components/status'
import { api } from '@/api'
import { useApp } from '@/stores/app'
import type { Band, Radio, RadioInput } from '@/types'
import { Badge, Button, Field, KeyValue, Panel, Segmented, Select, Spinner, Toggle } from '@/ui/kit'
import { isDfs, plural } from '@/utils/format'

const WIDTHS: Record<Band, number[]> = { '2.4': [20, 40], '5': [20, 40, 80, 160], '6': [20, 40, 80, 160, 320] }

export function RadiosPage() {
  const state = useApp((store) => store.state)
  if (!state) return <LoadingState />
  return (
    <Page
      title="Radios"
      description="Channel, width and transmit power per band. The AP caps power at the regulatory and hardware limit for the selected channel; the effective EIRP is shown after applying."
    >
      <div className="grid gap-3 xl:grid-cols-3">
        {state.radios.map((radio) => (
          <RadioCard key={`${radio.id}-${radio.channel}-${radio.width}-${radio.powerRequested}-${radio.enabled}-${radio.dca}-${radio.dtp}`} radio={radio} />
        ))}
      </div>
    </Page>
  )
}

function inputOf(radio: Radio): RadioInput {
  return { enabled: radio.enabled, channel: radio.channel, width: radio.width, power: radio.powerRequested, dca: radio.dca, dtp: radio.dtp }
}

function RadioCard({ radio }: { radio: Radio }) {
  const change = useApp((store) => store.change)
  const [form, setForm] = useState<RadioInput>(() => inputOf(radio))
  const [busy, setBusy] = useState(false)
  const initial = inputOf(radio)
  const dirty = JSON.stringify(form) !== JSON.stringify(initial)
  const set = <K extends keyof RadioInput>(key: K, value: RadioInput[K]) => setForm((current) => ({ ...current, [key]: value }))
  const channels = radio.allowedChannels.length ? radio.allowedChannels : [radio.channel]
  const powerCeiling = Math.max(radio.maxEirp ?? 30, form.power)

  const save = async () => {
    setBusy(true)
    await change(`${radio.band} GHz radio updated`, () => api.updateRadio(radio.id, form))
    setBusy(false)
  }

  return (
    <Panel
      title={
        <span className="flex items-center gap-2">
          <BandChip band={radio.band} /> radio
        </span>
      }
      actions={<Badge tone={radio.enabled ? 'ok' : 'neutral'}>{radio.enabled ? 'On' : 'Off'}</Badge>}
      bodyClassName="p-3 space-y-4"
    >
      <div className="grid grid-cols-3 gap-2 rounded border border-line bg-surface-2 p-2.5 text-center">
        <div>
          <p className="text-2xs uppercase tracking-wider text-faint">Channel</p>
          <p className="tabular text-[15px] font-semibold text-ink">
            {radio.channel}
            {isDfs(radio.band, radio.channel) && <span className="ml-1 text-2xs text-warn">DFS</span>}
          </p>
        </div>
        <div>
          <p className="text-2xs uppercase tracking-wider text-faint">Width</p>
          <p className="tabular text-[15px] font-semibold text-ink">{radio.width} MHz</p>
        </div>
        <div>
          <p className="text-2xs uppercase tracking-wider text-faint">EIRP</p>
          <p className="tabular text-[15px] font-semibold text-ink">{radio.eirp ?? '—'} dBm</p>
        </div>
      </div>

      <div className="space-y-2">
        <Meter value={radio.utilization} label="Channel utilisation" />
        <div className="grid grid-cols-2 gap-3">
          <Meter value={radio.rxUtilization} label="Receive" />
          <Meter value={radio.txUtilization} label="Transmit" />
        </div>
      </div>

      <div className="space-y-3 border-t border-line pt-3">
        <Toggle checked={form.enabled} onChange={(value) => set('enabled', value)} label="Radio enabled" hint="Turning a radio off stops every network on this band." />
        <div className="grid grid-cols-2 gap-3">
          <Field label="Channel" hint={isDfs(radio.band, form.channel) ? 'DFS: radar check before broadcasting' : undefined}>
            <Select value={form.channel} onChange={(event) => set('channel', Number(event.target.value))}>
              {channels.map((channel) => (
                <option key={channel} value={channel}>
                  {channel}
                  {isDfs(radio.band, channel) ? ' (DFS)' : ''}
                </option>
              ))}
            </Select>
          </Field>
          <Field label="Transmit power" hint={`Requested; limit ${radio.maxEirp ?? '—'} dBm EIRP`}>
            <div className="flex items-center gap-2">
              <input type="range" min={1} max={powerCeiling} value={form.power} onChange={(event) => set('power', Number(event.target.value))} className="min-w-0 flex-1 accent-[var(--accent)]" aria-label="Transmit power" />
              <span className="tabular w-14 text-right text-[12px] text-ink">{form.power} dBm</span>
            </div>
          </Field>
        </div>
        <Field label="Channel width">
          <Segmented value={form.width} onChange={(value) => set('width', value)} options={WIDTHS[radio.band].map((width) => ({ value: width, label: `${width}` }))} className="w-full" />
        </Field>
        <div>
          <Toggle checked={form.dca} onChange={(value) => set('dca', value)} label="Automatic channel" hint="Let the AP pick the channel (DCA)." />
          <Toggle checked={form.dtp} onChange={(value) => set('dtp', value)} label="Automatic power" hint="Let the AP adjust transmit power (DTP)." />
        </div>
        <div className="flex justify-end gap-2">
          <Button disabled={!dirty || busy} onClick={() => setForm(initial)}>
            <RotateCcw size={13} /> Reset
          </Button>
          <Button variant="primary" disabled={!dirty || busy} onClick={() => void save()}>
            {busy ? <Spinner size={12} /> : <Save size={13} />} Apply
          </Button>
        </div>
      </div>

      <div className="border-t border-line pt-3">
        <KeyValue
          items={[
            { label: 'Noise floor', value: radio.noiseFloor === null ? '—' : `${radio.noiseFloor} dBm` },
            { label: 'Max EIRP / TX', value: `${radio.maxEirp ?? '—'} / ${radio.maxTxPower ?? '—'} dBm` },
            { label: 'Networks', value: plural(radio.bssids, 'BSSID') },
            { label: 'Clients', value: radio.clients },
            { label: 'Nearby APs', value: radio.neighbors },
            { label: 'Radio MAC', value: radio.baseMac || '—', mono: true },
          ]}
        />
      </div>
    </Panel>
  )
}
