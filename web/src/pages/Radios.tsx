import { useState } from 'react'
import { Lightbulb, RotateCcw, Save } from 'lucide-react'
import { Page } from '@/app/Page'
import { LoadingState } from '@/components/Loading'
import { Wifi7Settings } from '@/components/Wifi7Settings'
import { BandChip, Meter } from '@/components/status'
import { ChannelChart } from '@/components/Charts'
import { api } from '@/api'
import { useApp } from '@/stores/app'
import { useStaging } from '@/stores/staging'
import { recommendChannel } from '@/utils/channels'
import type { Band, Radio, RadioInput } from '@/types'
import { Badge, Button, Field, KeyValue, Panel, Segmented, Select, Spinner, Toggle } from '@/ui/kit'
import { isDfs, plural } from '@/utils/format'

const WIDTHS: Record<Band, number[]> = { '2.4': [20, 40], '5': [20, 40, 80, 160], '6': [20, 40, 80, 160] }

export function RadiosPage() {
  const state = useApp((store) => store.state)
  if (!state) return <LoadingState />
  return (
    <Page
      title="Radios"
      description="Channel, width and power for each band. Bars under each radio show how busy its channels are nearby."
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
  const neighbors = useApp((store) => store.state?.neighbors) ?? []
  const stage = useStaging((s) => s.stage)
  const pending = useStaging((s) => s.changes.some((c) => c.kind === 'radio' && c.id === radio.id))
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
  const effectiveWidth = radio.wifi7?.saved?.enabled ? radio.wifi7.saved.width : form.width
  const advice = radio.enabled && neighbors.length ? recommendChannel(radio, neighbors, effectiveWidth) : null

  return (
    <Panel
      title={
        <span className="flex items-center gap-2">
          <BandChip band={radio.band} /> radio
        </span>
      }
      actions={
        <span className="flex gap-1">
          {pending && <Badge tone="accent">pending change</Badge>}
          <Badge tone={radio.enabled ? 'ok' : 'neutral'}>{radio.enabled ? 'On' : 'Off'}</Badge>
        </span>
      }
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
          <p className="tabular text-[15px] font-semibold text-ink">{radio.wifi7?.operatingWidth || radio.width} MHz</p>
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
        <div className="pt-1">
          <p className="mb-1 flex items-center justify-between text-[11px] text-faint">
            <span>Nearby networks per channel</span>
            <span className="tabular">{radio.neighbors} heard</span>
          </p>
          <ChannelChart band={radio.band} neighbors={neighbors} ours={radio.channel} channels={radio.allowedChannels} height={44} compact />
        </div>
      </div>

      {advice && (
        <div className={`flex items-start gap-2 rounded border px-2.5 py-2 text-[12px] leading-4 ${advice.better ? 'border-accent bg-accent-soft' : 'border-line bg-surface-2'}`}>
          <Lightbulb size={14} className={`mt-px shrink-0 ${advice.better ? 'text-accent-text' : 'text-faint'}`} />
          {advice.better ? (
            <p className="min-w-0 flex-1 text-ink">
              Channel <span className="font-semibold">{advice.channel}</span> looks quieter: {plural(advice.networks, 'nearby network')} overlap there, {advice.currentNetworks} on channel {radio.channel}.
              {isDfs(radio.band, advice.channel) && ' It is a radar (DFS) channel.'}
              {radio.dca && ' Automatic channel is on; using it switches to manual.'}
            </p>
          ) : (
            <p className="min-w-0 flex-1 text-muted">Channel {radio.channel} is already among the quietest here ({plural(advice.currentNetworks, 'overlapping network')}).</p>
          )}
          {advice.better && form.channel !== advice.channel && (
            <Button size="sm" write onClick={() => setForm((f) => ({ ...f, channel: advice.channel, dca: false }))}>
              Use {advice.channel}
            </Button>
          )}
        </div>
      )}

      <div className="space-y-3 border-t border-line pt-3">
        <Toggle checked={form.enabled} onChange={(value) => set('enabled', value)} label="Radio enabled" help="Turning a radio off stops every wireless network on this band." />
        <div className="grid grid-cols-2 gap-3">
          <Field
            label="Channel"
            help={
              <>
                Channels marked DFS (5 GHz 52–144) must be checked for radar for about a minute before the radio starts, and the AP leaves the channel if radar
                appears. Use the chart below to pick a quiet channel.
              </>
            }
          >
            <Select value={form.channel} onChange={(event) => set('channel', Number(event.target.value))}>
              {channels.map((channel) => (
                <option key={channel} value={channel}>
                  {channel}
                  {isDfs(radio.band, channel) ? ' (DFS)' : ''}
                </option>
              ))}
            </Select>
          </Field>
          <Field
            label="Transmit power"
            help={`The power you request. The AP limits it to the regulatory and hardware maximum for the channel (${radio.maxEirp ?? '—'} dBm EIRP here); the value actually used is shown as EIRP above.`}
          >
            <div className="flex items-center gap-2">
              <input type="range" min={1} max={powerCeiling} value={form.power} onChange={(event) => set('power', Number(event.target.value))} className="min-w-0 flex-1 accent-[var(--accent)]" aria-label="Transmit power" />
              <span className="tabular w-14 text-right text-[12px] text-ink">{form.power} dBm</span>
            </div>
          </Field>
        </div>
        <Field
          label="Channel width"
          help={radio.band === '6'
            ? 'Wider channels can increase speed. Enable Wi-Fi 7 below for 320 MHz. These widths are used in Wi-Fi 6E mode.'
            : 'Wider channels can increase speed but overlap more neighbours. 20 MHz is most robust on 2.4 GHz; 40–80 MHz suits 5 GHz.'}
        >
          {radio.wifi7?.saved?.enabled ? (
            <p className="rounded border border-line bg-surface-2 px-3 py-2 text-[12px] text-muted">Wi-Fi 7 controls the width: {radio.wifi7.saved.width} MHz. Change it below.</p>
          ) : <Segmented value={form.width} onChange={(value) => set('width', value)} options={WIDTHS[radio.band].map((width) => ({ value: width, label: `${width}` }))} className="w-full" />}
        </Field>
        <div>
          <Toggle checked={form.dca} onChange={(value) => set('dca', value)} label="Automatic channel" help="Dynamic channel assignment (DCA): the AP moves to a quieter channel on its own." />
          <Toggle checked={form.dtp} onChange={(value) => set('dtp', value)} label="Automatic power" help="Dynamic transmit power (DTP): the AP adjusts its power to the surroundings." />
        </div>
        <div className="flex justify-end gap-2">
          <Button disabled={!dirty || busy} onClick={() => setForm(initial)}>
            <RotateCcw size={13} /> Reset
          </Button>
          <Button write disabled={!dirty || busy} onClick={() => stage({ kind: 'radio', id: radio.id, radio: form })} title="Collect this with other changes and apply them together">
            Add to pending
          </Button>
          <Button variant="primary" disabled={!dirty || busy} onClick={() => void save()}>
            {busy ? <Spinner size={12} /> : <Save size={13} />} Apply now
          </Button>
        </div>
      </div>

      <Wifi7Settings key={`${radio.wifi7?.saved?.enabled}-${radio.wifi7?.saved?.width}`} radio={radio} />

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
