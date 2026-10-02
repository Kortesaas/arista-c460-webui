import { useState } from 'react'
import { Eye, EyeOff, Pencil, Plus, Trash2, Wifi, SlidersHorizontal } from 'lucide-react'
import { Page } from '@/app/Page'
import { WirelessFeaturesDialog } from '@/components/WirelessFeatures'
import { LoadingState } from '@/components/Loading'
import { BandChip, Dot, VlanChip } from '@/components/status'
import { api } from '@/api'
import { useApp } from '@/stores/app'
import type { Band, Ssid, SsidInput } from '@/types'
import { cn } from '@/ui/cn'
import { Badge, Button, Dialog, DialogActions, EmptyState, Field, IconButton, Input, Panel, Segmented, Select, Spinner, Toggle } from '@/ui/kit'
import { formatBytes, opModeLabel, opModeShort, plural } from '@/utils/format'

const BANDS: Band[] = ['2.4', '5', '6']
const SECURITY = ['WPA3_SAE', 'WPA2_PERSONAL', 'ENHANCED_OPEN', 'OPEN'] as const
const needsPassword = (mode: string) => mode === 'WPA3_SAE' || mode === 'WPA2_PERSONAL'
const allows6 = (mode: string) => mode === 'WPA3_SAE' || mode === 'ENHANCED_OPEN'

export function WirelessPage() {
  const state = useApp((store) => store.state)
  const [editing, setEditing] = useState<Ssid | 'new' | null>(null)
  const [advanced, setAdvanced] = useState<Ssid | null>(null)
  const [deleting, setDeleting] = useState<Ssid | null>(null)
  if (!state) return <LoadingState />

  return (
    <Page
      title="Wireless networks"
      description="SSIDs broadcast by this access point. Each network can be mapped to a VLAN on the uplink or stay untagged on the management network. DHCP comes from your router, not the AP."
      actions={
        <Button variant="primary" onClick={() => setEditing('new')}>
          <Plus size={14} /> Add network
        </Button>
      }
    >
      {state.ssids.length === 0 ? (
        <Panel>
          <EmptyState
            icon={<Wifi size={28} />}
            title="No wireless networks"
            description="Add a network to start broadcasting."
            action={<Button onClick={() => setEditing('new')}>Add network</Button>}
          />
        </Panel>
      ) : (
        <div className="overflow-x-auto rounded-lg border border-line bg-surface">
          <table className="w-full min-w-[760px] text-left text-[13px]">
            <thead className="border-b border-line text-2xs font-semibold uppercase tracking-wider text-faint">
              <tr>
                <th className="px-3 py-2">Network</th>
                <th className="px-3 py-2">Security</th>
                <th className="px-3 py-2">Bands</th>
                <th className="px-3 py-2">VLAN</th>
                <th className="px-3 py-2 text-right">Clients</th>
                <th className="px-3 py-2 text-right">Traffic</th>
                <th className="w-28 px-3 py-2" />
              </tr>
            </thead>
            <tbody className="divide-y divide-line">
              {state.ssids.map((ssid) => (
                <tr key={ssid.name} className="hover:bg-surface-2/50">
                  <td className="px-3 py-2.5">
                    <div className="flex items-center gap-2">
                      <Dot tone={ssid.enabled ? 'ok' : 'neutral'} />
                      <span className="font-medium text-ink">{ssid.name}</span>
                      {!ssid.enabled && <Badge>disabled</Badge>}
                      {ssid.hidden && <Badge>hidden</Badge>}
                      {ssid.isolation && <Badge tone="accent">isolated</Badge>}
                    </div>
                  </td>
                  <td className="px-3 py-2.5 text-muted">{opModeShort(ssid.opmode)}</td>
                  <td className="px-3 py-2.5">
                    <div className="flex gap-1">
                      {BANDS.map((band) => {
                        const configured = ssid.bands.includes(band)
                        const live = ssid.bssids.some((bssid) => bssid.band === band)
                        return configured ? (
                          <span key={band} title={live ? `Broadcasting on ${band} GHz` : `Configured for ${band} GHz but not broadcasting`} className={cn(!live && 'opacity-50')}>
                            <BandChip band={band} />
                          </span>
                        ) : null
                      })}
                    </div>
                  </td>
                  <td className="px-3 py-2.5">
                    <VlanChip vlan={ssid.vlan} />
                  </td>
                  <td className="tabular px-3 py-2.5 text-right text-muted">{ssid.clients}</td>
                  <td className="tabular px-3 py-2.5 text-right text-[12px] text-faint">
                    ↓ {formatBytes(ssid.rxBytes)} · ↑ {formatBytes(ssid.txBytes)}
                  </td>
                  <td className="px-3 py-2.5">
                    <div className="flex justify-end gap-0.5">
                      <IconButton label={`Advanced settings for ${ssid.name}`} onClick={() => setAdvanced(ssid)}>
                        <SlidersHorizontal size={14} />
                      </IconButton>
                      <IconButton label={`Edit ${ssid.name}`} onClick={() => setEditing(ssid)}>
                        <Pencil size={14} />
                      </IconButton>
                      <IconButton label={`Delete ${ssid.name}`} onClick={() => setDeleting(ssid)} className="hover:text-danger">
                        <Trash2 size={14} />
                      </IconButton>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <p className="mt-3 text-[12px] text-faint">
        {plural(state.ssids.length, 'network')} ·{' '}
        {plural(
          state.ssids.reduce((sum, ssid) => sum + ssid.bssids.length, 0),
          'BSSID',
        )}{' '}
        broadcasting. Saving a change restarts Wi-Fi on this AP for a few seconds.
      </p>

      {advanced && <WirelessFeaturesDialog name={advanced.name} onClose={() => setAdvanced(null)} />}
      {editing && <SsidDialog ssid={editing === 'new' ? null : editing} existing={state.ssids.map((ssid) => ssid.name)} onClose={() => setEditing(null)} />}
      {deleting && <DeleteDialog ssid={deleting} onClose={() => setDeleting(null)} />}
    </Page>
  )
}

function SsidDialog({ ssid, existing, onClose }: { ssid: Ssid | null; existing: string[]; onClose: () => void }) {
  const change = useApp((store) => store.change)
  const [form, setForm] = useState<SsidInput>(() => ({
    name: ssid?.name ?? '',
    enabled: ssid?.enabled ?? true,
    hidden: ssid?.hidden ?? false,
    opmode: ssid && (SECURITY as readonly string[]).includes(ssid.opmode) ? ssid.opmode : 'WPA3_SAE',
    password: '',
    bands: ssid?.bands ?? ['2.4', '5', '6'],
    vlan: ssid ? ssid.vlan : null,
    isolation: ssid?.isolation ?? false,
  }))
  const [vlanMode, setVlanMode] = useState<'tagged' | 'untagged'>(ssid && ssid.vlan === null ? 'untagged' : ssid ? 'tagged' : 'tagged')
  const [vlanText, setVlanText] = useState(ssid?.vlan ? String(ssid.vlan) : '')
  const [showPassword, setShowPassword] = useState(false)
  const [busy, setBusy] = useState(false)
  const set = <K extends keyof SsidInput>(key: K, value: SsidInput[K]) => setForm((current) => ({ ...current, [key]: value }))

  const vlan = vlanMode === 'untagged' ? null : Number(vlanText)
  const problems: string[] = []
  const name = form.name.trim()
  if (!name) problems.push('Enter a network name.')
  else if (new TextEncoder().encode(name).length > 32) problems.push('The network name can be at most 32 bytes.')
  else if (/[[\]/=\\"]/.test(name)) problems.push('The network name must not contain [ ] / = \\ or ".')
  else if (name !== ssid?.name && existing.includes(name)) problems.push('A network with this name already exists.')
  if (form.bands.length === 0) problems.push('Select at least one band.')
  if (form.bands.includes('6') && !allows6(form.opmode)) problems.push('6 GHz requires WPA3 Personal or Enhanced Open.')
  if (needsPassword(form.opmode)) {
    const keepExisting = ssid?.hasPassword && form.password === '' && needsPassword(ssid.opmode)
    if (!keepExisting && (form.password.length < 8 || form.password.length > 63)) problems.push('The password must be 8–63 characters.')
  }
  if (vlanMode === 'tagged' && (!Number.isInteger(vlan) || vlan === null || vlan < 1 || vlan > 4094)) problems.push('Enter a VLAN ID between 1 and 4094.')

  const save = async () => {
    setBusy(true)
    const input = { ...form, name, vlan }
    const ok = await change(ssid ? `Saved ${name}` : `Created ${name}`, () => (ssid ? api.updateSsid(ssid.name, input) : api.createSsid(input)))
    setBusy(false)
    if (ok) onClose()
  }

  return (
    <Dialog title={ssid ? `Edit ${ssid.name}` : 'Add wireless network'} description="Changes apply to this access point immediately after saving." onClose={onClose} wide>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Network name (SSID)" className="sm:col-span-2">
          <Input value={form.name} maxLength={32} onChange={(event) => set('name', event.target.value)} placeholder="My network" />
        </Field>

        <Field label="Security">
          <Select
            value={form.opmode}
            onChange={(event) => {
              const opmode = event.target.value
              setForm((current) => ({ ...current, opmode, bands: allows6(opmode) ? current.bands : current.bands.filter((band) => band !== '6') }))
            }}
          >
            {SECURITY.map((mode) => (
              <option key={mode} value={mode}>
                {opModeLabel[mode]}
              </option>
            ))}
          </Select>
        </Field>

        <Field label="Password" hint={ssid?.hasPassword && needsPassword(form.opmode) ? 'Leave empty to keep the current password.' : '8–63 characters.'}>
          <div className="relative">
            <Input
              type={showPassword ? 'text' : 'password'}
              autoComplete="new-password"
              value={form.password}
              disabled={!needsPassword(form.opmode)}
              onChange={(event) => set('password', event.target.value)}
              placeholder={needsPassword(form.opmode) ? (ssid?.hasPassword ? '••••••••' : '') : 'Not used'}
              className="pr-8"
            />
            <IconButton
              label={showPassword ? 'Hide password' : 'Show password'}
              size="md"
              onClick={() => setShowPassword((value) => !value)}
              className="absolute right-0.5 top-0.5"
              disabled={!needsPassword(form.opmode)}
            >
              {showPassword ? <EyeOff size={14} /> : <Eye size={14} />}
            </IconButton>
          </div>
        </Field>

        <Field label="Bands" hint={allows6(form.opmode) ? 'Broadcast on every selected band for the best coverage.' : '6 GHz needs WPA3 Personal or Enhanced Open.'}>
          <div className="flex gap-1.5">
            {BANDS.map((band) => {
              const active = form.bands.includes(band)
              const disabled = band === '6' && !allows6(form.opmode)
              return (
                <button
                  key={band}
                  type="button"
                  disabled={disabled}
                  aria-pressed={active}
                  onClick={() => set('bands', active ? form.bands.filter((item) => item !== band) : BANDS.filter((item) => item === band || form.bands.includes(item)))}
                  className={cn(
                    'h-8 flex-1 rounded border text-[12px] font-medium transition-colors disabled:opacity-30',
                    active ? 'border-accent bg-accent-soft text-accent-text' : 'border-line bg-surface text-muted hover:border-line-strong hover:text-ink',
                  )}
                >
                  {band} GHz
                </button>
              )
            })}
          </div>
        </Field>

        <Field label="VLAN" hint={vlanMode === 'untagged' ? "Clients join the AP's own management network." : 'Tagged on the uplink; the switch port must carry this VLAN.'}>
          <div className="flex gap-1.5">
            <Segmented
              value={vlanMode}
              onChange={setVlanMode}
              options={[
                { value: 'tagged', label: 'Tagged' },
                { value: 'untagged', label: 'Untagged' },
              ]}
            />
            <Input
              type="number"
              min={1}
              max={4094}
              inputMode="numeric"
              value={vlanMode === 'tagged' ? vlanText : ''}
              disabled={vlanMode === 'untagged'}
              onChange={(event) => setVlanText(event.target.value)}
              placeholder="ID"
              className="w-24"
            />
          </div>
        </Field>

        <div className="space-y-1 sm:col-span-2">
          <Toggle checked={form.enabled} onChange={(value) => set('enabled', value)} label="Enabled" hint="Disabled networks keep their settings but stop broadcasting." />
          <Toggle checked={form.hidden} onChange={(value) => set('hidden', value)} label="Hide network name" hint="Clients have to enter the SSID manually." />
          <Toggle
            checked={form.isolation}
            onChange={(value) => set('isolation', value)}
            label="Client isolation"
            hint="Clients on this network cannot talk to each other through the AP."
          />
        </div>
      </div>

      {problems.length > 0 && (
        <ul className="mt-4 space-y-0.5 text-[12px] text-warn">
          {problems.map((problem) => (
            <li key={problem}>{problem}</li>
          ))}
        </ul>
      )}

      <DialogActions>
        <Button onClick={onClose}>Cancel</Button>
        <Button variant="primary" disabled={busy || problems.length > 0} onClick={() => void save()}>
          {busy && <Spinner size={12} />}
          {ssid ? 'Save' : 'Create network'}
        </Button>
      </DialogActions>
    </Dialog>
  )
}

function DeleteDialog({ ssid, onClose }: { ssid: Ssid; onClose: () => void }) {
  const change = useApp((store) => store.change)
  const [busy, setBusy] = useState(false)
  return (
    <Dialog title={`Delete ${ssid.name}?`} description="The network stops broadcasting and its settings are removed from this access point." onClose={onClose}>
      {ssid.clients > 0 && <p className="text-[13px] text-warn">{plural(ssid.clients, 'client is', 'clients are')} connected right now and will be disconnected.</p>}
      <DialogActions>
        <Button onClick={onClose}>Cancel</Button>
        <Button
          variant="danger"
          disabled={busy}
          onClick={() => {
            setBusy(true)
            void change(`Deleted ${ssid.name}`, () => api.deleteSsid(ssid.name)).then((ok) => {
              setBusy(false)
              if (ok) onClose()
            })
          }}
        >
          {busy ? <Spinner size={12} /> : <Trash2 size={14} />}
          Delete network
        </Button>
      </DialogActions>
    </Dialog>
  )
}
