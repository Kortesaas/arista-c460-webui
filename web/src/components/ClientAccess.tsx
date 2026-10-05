import { useEffect, useState } from 'react'
import { Save } from 'lucide-react'
import { api } from '@/api'
import { useApp } from '@/stores/app'
import type { SsidPolicy, SsidPolicyStatus } from '@/types'
import { Badge, Button, Dialog, DialogActions, Field, Input, SectionLabel, Select, Spinner, Textarea, Toggle, useReadOnly } from '@/ui/kit'

const modeLabel = { off: 'No MAC filtering', allow: 'Allow only listed devices', deny: 'Block listed devices' }

export function ClientAccessDialog({ name, onClose }: { name: string; onClose: () => void }) {
  const [data, setData] = useState<SsidPolicyStatus | null>(null)
  const [mode, setMode] = useState<SsidPolicy['macFilter']['mode']>('off')
  const [addresses, setAddresses] = useState('')
  const [limited, setLimited] = useState(false)
  const [limit, setLimit] = useState('127')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const readOnly = useReadOnly()
  const change = useApp((s) => s.change)
  useEffect(() => {
    let active = true
    api.ssidPolicy(name).then((value) => {
      if (!active) return
      setData(value)
      setMode(value.settings.macFilter.mode)
      setAddresses(value.settings.macFilter.addresses.join('\n'))
      setLimited(value.settings.maxClients !== null)
      setLimit(String(value.settings.maxClients ?? 127))
    }).catch((e: unknown) => {
      if (active) setError(e instanceof Error ? e.message : String(e))
    })
    return () => { active = false }
  }, [name])

  const macs = addresses.split(/[\s,]+/).filter(Boolean).map((v) => v.toLowerCase().replaceAll('-', ':')).sort()
  const policy: SsidPolicy = { macFilter: { mode, addresses: macs }, maxClients: limited ? Number(limit) : null }
  const problems: string[] = []
  if (mode === 'allow' && macs.length === 0) problems.push('Add at least one device to the allow list.')
  if (macs.length > 128) problems.push('Use at most 128 MAC addresses.')
  if (macs.some((v) => !/^([0-9a-f]{2}:){5}[0-9a-f]{2}$/.test(v) || (parseInt(v.slice(0, 2), 16) & 1) !== 0 || v === '00:00:00:00:00:00')) problems.push('Enter a valid Wi-Fi MAC address for each device.')
  if (new Set(macs).size !== macs.length) problems.push('Remove repeated MAC addresses.')
  if (limited && (!Number.isInteger(policy.maxClients) || !limit || Number(limit) < 1 || Number(limit) > 127)) problems.push('The client limit must be between 1 and 127.')
  const changed = data && JSON.stringify(policy) !== JSON.stringify(data.settings)
  const disabled = busy || readOnly || !data?.supported
  const save = async () => {
    setBusy(true)
    const ok = await change(`Saved client access for ${name}`, () => api.updateSsidPolicy(name, policy))
    setBusy(false)
    if (ok) onClose()
  }
  return (
    <Dialog wide title={`Client access · ${name}`} description="Choose which devices may connect and how many can join each band." onClose={() => !busy && onClose()}>
      {error ? <p role="alert" className="text-[12px] text-danger">{error}</p> : !data ? (
        <p className="flex items-center gap-2 py-4 text-[12px] text-muted"><Spinner size={13} /> Reading client access…</p>
      ) : (
        <div className="space-y-4">
          {data.error && <p role="alert" className="text-[12px] text-danger">{data.error}</p>}
          {!data.supported && <p className="text-[12px] text-warn">Client access controls are unavailable on this firmware.</p>}
          <Field label="Device access" hint="MAC filtering supplements the network password. Phones may use a private Wi-Fi MAC address; use the address shown for this network.">
            <Select value={mode} disabled={disabled} onChange={(e) => setMode(e.target.value as typeof mode)}>
              {Object.entries(modeLabel).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
            </Select>
          </Field>
          <Field label={mode === 'allow' ? 'Allowed Wi-Fi MAC addresses' : mode === 'deny' ? 'Blocked Wi-Fi MAC addresses' : 'Wi-Fi MAC addresses'} hint="One address per line, or separated by commas. Lists are kept when filtering is off.">
            <Textarea aria-label="Wi-Fi MAC addresses" rows={5} className="font-mono" value={addresses} disabled={disabled} spellCheck={false} placeholder="02:00:00:46:00:01" onChange={(e) => setAddresses(e.target.value)} />
          </Field>
          {mode !== 'off' && <p className="text-[12px] text-warn">Changing access can disconnect devices. For an allow list, include the device you use to manage the AP.</p>}
          <div className="border-t border-line pt-3">
            <Toggle checked={limited} disabled={disabled} onChange={setLimited} label="Limit clients per band" hint="The limit applies separately to each band of this network. The firmware default is 127 per band." />
            {limited && <Field label="Maximum clients" className="mt-3"><Input aria-label="Maximum clients per band" type="number" min={1} max={127} value={limit} disabled={disabled} onChange={(e) => setLimit(e.target.value)} className="max-w-32" /></Field>}
          </div>
          <div className="border-t border-line pt-3">
            <SectionLabel>Operating now</SectionLabel>
            {data.interfaces.length === 0 ? <p className="mt-1 text-[12px] text-muted">No active band is available for operating readback. Saved settings apply when the network is enabled.</p> : (
              <ul className="mt-2 divide-y divide-line">
                {data.interfaces.map((iface) => <li key={iface.interface} className="flex flex-wrap items-center gap-2 py-2 text-[12px]">
                  <span className="w-14 font-medium">{iface.band || '—'} GHz</span>
                  <Badge>{iface.macMode === null ? 'Filter unknown' : modeLabel[iface.macMode as keyof typeof modeLabel]}</Badge>
                  <span className="text-muted">{iface.maxClients === null ? 'Limit unknown' : `${iface.maxClients} clients maximum`}</span>
                  {iface.macMode !== 'off' && <span className="text-muted">{iface.addresses === null ? 'Address list unknown' : `${iface.addresses.length} listed MAC addresses`}</span>}
                  {iface.error && <span className="text-warn">{iface.error}</span>}
                </li>)}
              </ul>
            )}
          </div>
          {problems.length > 0 && <ul className="space-y-1 text-[12px] text-warn">{problems.map((problem) => <li key={problem}>{problem}</li>)}</ul>}
        </div>
      )}
      <DialogActions>
        <Button disabled={busy} onClick={onClose}>Cancel</Button>
        <Button variant="primary" disabled={disabled || Boolean(data?.error) || !changed || problems.length > 0} onClick={() => void save()}>
          {busy ? <Spinner size={12} /> : <Save size={13} />} Save
        </Button>
      </DialogActions>
    </Dialog>
  )
}
