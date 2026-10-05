import { useEffect, useState } from 'react'
import { RefreshCw, Save, Trash2 } from 'lucide-react'
import { api } from '@/api'
import { useApp } from '@/stores/app'
import type { TrafficLimits, TrafficPolicy, TrafficQoS, TrafficStatus } from '@/types'
import { Badge, Button, Dialog, DialogActions, Field, Input, SectionLabel, Select, Spinner, Toggle, useReadOnly } from '@/ui/kit'
import { formatBytes } from '@/utils/format'

const emptyLimits = (): TrafficLimits => ({ uploadKbps: null, downloadKbps: null })
const defaultQoS = (): TrafficQoS => ({ priority: 'best-effort', mode: 'ceiling', mapping: 'dscp', markDSCP: false, mark8021p: false })
const priorityLabels = { voice: 'Voice / audio / control', video: 'Video', 'best-effort': 'Best effort', background: 'Background' }
const validMAC = (v: string) => /^([0-9a-f]{2}:){5}[0-9a-f]{2}$/.test(v) && (parseInt(v.slice(0, 2), 16) & 1) === 0 && v !== '00:00:00:00:00:00'

export function TrafficLimitsHint() {
  return <p className="text-[12px] leading-5 text-muted"><strong className="font-medium text-ink">Bandwidth shaping:</strong> 32–1,000,000 Kbps per direction; blank means unlimited. This AP can shape traffic on up to eight SSIDs at once. Each SSID cap is shared across all its bands and devices.</p>
}

function RateFields({ value, onChange, disabled, prefix }: { value: TrafficLimits; onChange: (value: TrafficLimits) => void; disabled: boolean; prefix: string }) {
  return <div className="grid gap-3 sm:grid-cols-2">{(['downloadKbps', 'uploadKbps'] as const).map((key) => <Field key={key} label={key === 'downloadKbps' ? 'Download · Kbps' : 'Upload · Kbps'}>
    <Input aria-label={`${prefix} ${key === 'downloadKbps' ? 'download' : 'upload'} Kbps`} type="number" min={32} max={1000000} step={1} placeholder="Unlimited" value={value[key] ?? ''} disabled={disabled} onChange={(e) => onChange({ ...value, [key]: e.target.value === '' ? null : Number(e.target.value) })} />
  </Field>)}</div>
}

export function TrafficDialog({ name, clientMAC, onClose }: { name: string; clientMAC?: string; onClose: () => void }) {
  const [data, setData] = useState<TrafficStatus | null>(null)
  const [form, setForm] = useState<TrafficPolicy | null>(null)
  const [view, setView] = useState<'network' | 'devices' | 'queues'>(clientMAC ? 'devices' : 'network')
  const [mac, setMAC] = useState(clientMAC ?? '')
  const [draft, setDraft] = useState<TrafficLimits>(emptyLimits)
  const [busy, setBusy] = useState(false)
  const [reading, setReading] = useState(false)
  const [error, setError] = useState('')
  const readOnly = useReadOnly()
  const change = useApp((s) => s.change)
  const clients = useApp((s) => s.state?.clients ?? [])
  useEffect(() => {
    let active = true
    api.traffic(name).then((v) => { if (active) { setData(v); setForm(v.settings); setDraft(v.settings.perClient) } }).catch((e: unknown) => { if (active) setError(e instanceof Error ? e.message : String(e)) })
    return () => { active = false }
  }, [name])
  const refresh = async () => {
    setReading(true)
    try { setData(await api.traffic(name)); setError('') } catch (e) { setError(e instanceof Error ? e.message : String(e)) }
    finally { setReading(false) }
  }
  const disabled = busy || readOnly || !data?.supported
  const changed = form && data && JSON.stringify(form) !== JSON.stringify(data.settings)
  const limits = form ? [form.bandwidth, form.perClient, ...Object.values(form.clients)] : []
  const invalidRate = limits.some((l) => Object.values(l).some((n) => n !== null && (!Number.isInteger(n) || n < 32 || n > 1000000)))
  const cleanMAC = mac.trim().toLowerCase().replaceAll('-', ':')
  const addOverride = () => { if (!form || !validMAC(cleanMAC)) return; setForm({ ...form, clients: { ...form.clients, [cleanMAC]: draft } }); setMAC(''); setDraft(emptyLimits()) }
  const removeOverride = (address: string) => { if (!form) return; const next = { ...form.clients }; delete next[address]; setForm({ ...form, clients: next }) }
  const save = async () => {
    if (!form) return
    setBusy(true)
    await change(`Saved bandwidth and QoS for ${name}`, async () => { const v = await api.updateTraffic(name, form); setData(v); setForm(v.settings) })
    setBusy(false)
  }
  const qos = form?.qos
  const setQoS = (value: Partial<TrafficQoS>) => { if (form && qos) setForm({ ...form, qos: { ...qos, ...value } }) }
  return <Dialog wide title={`Bandwidth and QoS · ${name}`} description="Limit speeds from the device’s perspective. 1,000 Kbps = 1 Mbps; blank means unlimited." onClose={() => !busy && onClose()}>
    {error && <p role="alert" className="mb-3 text-[12px] text-danger">{error}</p>}
    {!form || !data ? <p className="flex items-center gap-2 py-4 text-[12px] text-muted"><Spinner size={13} /> Reading traffic controls…</p> : <div className="space-y-4">
      <TrafficLimitsHint />
      <div className="flex flex-wrap items-center gap-2">
        {(['network', 'devices', 'queues'] as const).map((v) => <Button key={v} variant={view === v ? 'primary' : 'default'} onClick={() => setView(v)}>{v === 'network' ? 'Network limits & QoS' : v === 'devices' ? 'Device limits' : 'Live queues'}</Button>)}
      </div>
      {data.error && <p role="alert" className="text-[12px] text-warn">{data.error}</p>}
      {!data.supported && <p className="text-[12px] text-warn">Traffic controls are unavailable on this firmware.</p>}
      {view === 'network' && <>
        <div><SectionLabel>Whole network · shared across all bands and devices</SectionLabel><div className="mt-2"><RateFields prefix="Network" value={form.bandwidth} disabled={disabled} onChange={(bandwidth) => setForm({ ...form, bandwidth })} /></div></div>
        <div className="border-t border-line pt-3"><SectionLabel>Default limit for each device</SectionLabel><p className="my-2 text-[12px] text-muted">Each connected device gets its own cap. Device overrides replace this default; the whole network cap still applies.</p><RateFields prefix="Default device" value={form.perClient} disabled={disabled} onChange={(perClient) => setForm({ ...form, perClient })} /></div>
        <div className="space-y-3 border-t border-line pt-3">
          <Toggle checked={qos !== null} disabled={disabled} label="Customize traffic priority" hint="Uses Wi-Fi WMM priorities. Off restores the firmware’s QoS settings." onChange={(on) => setForm({ ...form, qos: on ? defaultQoS() : null })} />
          {qos && <>
            <div className="grid gap-3 sm:grid-cols-2">
              <Field label="Network priority"><Select aria-label="Network priority" value={qos.priority} disabled={disabled} onChange={(e) => setQoS({ priority: e.target.value as TrafficQoS['priority'] })}>{Object.entries(priorityLabels).map(([v, label]) => <option key={v} value={v}>{label}</option>)}</Select></Field>
              <Field label="Priority mode" hint="Ceiling allows lower priority traffic. Fixed assigns this priority to every packet."><Select aria-label="Priority mode" value={qos.mode} disabled={disabled} onChange={(e) => setQoS({ mode: e.target.value as TrafficQoS['mode'] })}><option value="ceiling">Priority ceiling</option><option value="fixed">Fixed priority</option></Select></Field>
            </div>
            <Field label="Download priority mapping" hint="DSCP uses the IP packet’s traffic class; 802.1p uses VLAN priority."><Select aria-label="Download priority mapping" value={qos.mapping} disabled={disabled} onChange={(e) => setQoS({ mapping: e.target.value as TrafficQoS['mapping'] })}><option value="dscp">DSCP</option><option value="8021p">802.1p</option><option value="tos">Legacy TOS</option></Select></Field>
            <Toggle checked={qos.markDSCP} disabled={disabled} label="Mark upstream DSCP" onChange={(markDSCP) => setQoS({ markDSCP })} />
            <Toggle checked={qos.mark8021p} disabled={disabled} label="Mark upstream VLAN priority" hint="Relevant to networks with a tagged VLAN." onChange={(mark8021p) => setQoS({ mark8021p })} />
          </>}
          <p className="text-[12px] leading-5 text-muted">Priorities matter when traffic competes for airtime. Choose fixed voice for a dedicated audio/control network, or DSCP with a voice ceiling to retain packet priorities. The driver accepts these settings but exposes no operating QoS readback.</p>
        </div>
        <p className="border-t border-line pt-3 text-[12px] leading-5 text-muted">Bandwidth caps help test slow connections. They do not introduce weak signal, latency, jitter or packet loss.</p>
      </>}
      {view === 'devices' && <>
        <p className="text-[12px] leading-5 text-muted">Overrides use the device’s Wi-Fi MAC address on this network, including a phone’s private address. Both directions replace the default. Remove an override to inherit the default again.</p>
        <div className="space-y-3 rounded border border-line p-3">
          <Field label="Add device override"><Input aria-label="Device Wi-Fi MAC address" list="traffic-connected-devices" spellCheck={false} value={mac} placeholder="02:00:00:46:00:01" disabled={disabled} onChange={(e) => setMAC(e.target.value)} /><datalist id="traffic-connected-devices">{clients.filter((c) => c.ssid === name).map((c) => <option key={c.mac} value={c.mac}>{c.hostname || c.ipv4 || 'Connected device'}</option>)}</datalist></Field>
          <RateFields prefix="New device" value={draft} disabled={disabled} onChange={setDraft} />
          <Button disabled={disabled || !validMAC(cleanMAC) || Object.keys(form.clients).length >= 128} onClick={addOverride}>Add / replace override</Button>
        </div>
        {Object.keys(form.clients).length === 0 ? <p className="text-[12px] text-muted">No device overrides. Devices inherit the default limits.</p> : Object.entries(form.clients).sort(([a], [b]) => a.localeCompare(b)).map(([address, value]) => <div key={address} className="space-y-2 border-t border-line pt-3">
          <div className="flex items-center justify-between gap-2"><span className="font-mono text-[12px]">{address}</span><Button disabled={disabled} onClick={() => removeOverride(address)}><Trash2 size={12} /> Inherit default</Button></div>
          <RateFields prefix={address} value={value} disabled={disabled} onChange={(v) => setForm({ ...form, clients: { ...form.clients, [address]: v } })} />
        </div>)}
      </>}
      {view === 'queues' && <>
        <div className="flex flex-wrap items-center justify-between gap-2"><Badge tone={data.applied ? 'ok' : 'neutral'}>{data.pending ? 'Waiting for active network' : data.applied ? 'Limits verified in kernel' : data.managed ? 'Limits need restoring' : 'Using firmware defaults'}</Badge><Button disabled={reading || busy} onClick={() => void refresh()}>{reading ? <Spinner size={12} /> : <RefreshCw size={12} />} Refresh counters</Button></div>
        <p className="text-[12px] leading-5 text-muted">Cumulative counters since queues were created. Overlimits count scheduling deferrals; they do not mean packets were dropped. Saving or rebuilding limits resets these counters.</p>
        {data.queues.length === 0 ? <p className="text-[12px] text-muted">No active bandwidth queues.</p> : <div className="overflow-x-auto"><table className="w-full text-left text-[12px]"><thead className="text-faint"><tr>{['Scope / direction', 'Limit', 'Transferred', 'Packets', 'Drops', 'Overlimits'].map((v) => <th key={v} className="px-2 py-2 font-medium">{v}</th>)}</tr></thead><tbody className="divide-y divide-line">{data.queues.map((q) => <tr key={`${q.interface}:${q.mac ?? 'ssid'}`}><td className="px-2 py-2"><span className="font-mono">{q.mac || 'Whole network'}</span><span className="block text-muted">{q.direction}</span></td><td className="px-2 py-2">{q.limitKbps?.toLocaleString()} Kbps</td><td className="px-2 py-2">{formatBytes(q.bytes)}</td><td className="px-2 py-2">{q.packets.toLocaleString()}</td><td className="px-2 py-2">{q.drops.toLocaleString()}</td><td className="px-2 py-2">{q.overlimits.toLocaleString()}</td></tr>)}</tbody></table></div>}
        {form.qos && <p className="text-[12px] text-muted">QoS: {data.qosStatus === 'configured-no-driver-readback' ? 'Configuration accepted; operating priority cannot be read from this driver.' : data.qosStatus}</p>}
      </>}
      {invalidRate && <p role="alert" className="text-[12px] text-warn">Use whole numbers between 32 and 1,000,000 Kbps, or leave a limit blank.</p>}
    </div>}
    <DialogActions><Button disabled={busy} onClick={onClose}>Close</Button><Button variant="primary" disabled={disabled || !changed || invalidRate} onClick={() => void save()}>{busy ? <Spinner size={12} /> : <Save size={13} />} Save limits & QoS</Button></DialogActions>
  </Dialog>
}
