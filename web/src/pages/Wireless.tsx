import { useEffect, useState } from 'react'
import { Gauge, ShieldCheck } from 'lucide-react'
import { TrafficDialog, TrafficLimitsHint } from '@/components/Traffic'
import { ClientAccessDialog } from '@/components/ClientAccess'
import { useSearchParams } from 'react-router-dom'
import { CalendarClock, Eye, EyeOff, Pencil, Plus, QrCode, Trash2, Wifi, SlidersHorizontal } from 'lucide-react'
import { Page } from '@/app/Page'
import { WirelessFeaturesDialog } from '@/components/WirelessFeatures'
import { JoinCodeDialog } from '@/components/JoinCode'
import { ScheduleDialog, scheduleSummary } from '@/components/Schedule'
import { LoadingState } from '@/components/Loading'
import { BandChip, Dot, VlanChip } from '@/components/status'
import { api } from '@/api'
import { useApp } from '@/stores/app'
import { changeTarget, useStaging } from '@/stores/staging'
import type { Band, Ssid, SsidInput, StagedChange } from '@/types'
import { cn } from '@/ui/cn'
import { Badge, Button, Dialog, DialogActions, EmptyState, Field, IconButton, Input, Panel, Segmented, Select, Spinner, Toggle } from '@/ui/kit'
import { formatBytes, opModeLabel, opModeShort, plural } from '@/utils/format'

const BANDS: Band[] = ['2.4', '5', '6']
const SECURITY = ['WPA3_SAE', 'WPA2_WPA3_PERSONAL', 'WPA2_PERSONAL', 'ENHANCED_OPEN', 'OPEN'] as const
const needsPassword = (mode: string) => mode === 'WPA3_SAE' || mode === 'WPA2_WPA3_PERSONAL' || mode === 'WPA2_PERSONAL'
const allows6 = (mode: string) => mode === 'WPA3_SAE' || mode === 'ENHANCED_OPEN'
const securityHint: Record<string, string> = {
  WPA3_SAE: 'Most secure. Older devices without WPA3 support cannot join.',
  WPA2_WPA3_PERSONAL: 'Older devices join with WPA2, newer ones use WPA3. Works on 2.4 and 5 GHz; use a separate WPA3 network for 6 GHz.',
  WPA2_PERSONAL: 'Only for devices that fail with WPA2/WPA3 mixed. Not allowed on 6 GHz.',
  ENHANCED_OPEN: 'No password, but traffic is encrypted. Not every device supports it.',
  OPEN: 'No password and no encryption. Anyone nearby can join and read the traffic.',
}

/** Small status line for a mixed-mode network whose native setting is not active yet. */
function MixedBadge({ ssid }: { ssid: Ssid }) {
  if (ssid.opmode !== 'WPA2_WPA3_PERSONAL' || !ssid.mixedStatus || ssid.mixedStatus === 'applied') return null
  if (ssid.mixedStatus === 'pending')
    return (
      <Badge tone="warn" title="The AP is switching this network to WPA2/WPA3 mixed. Until then only WPA3 devices can join. This takes up to a minute.">
        activating mixed
      </Badge>
    )
  return (
    <Badge tone="danger" title={`${ssid.mixedStatus}. WPA3 devices can still join.`}>
      WPA2 inactive
    </Badge>
  )
}

export function WirelessPage() {
  const state = useApp((store) => store.state)
  const staged = useStaging((s) => s.changes)
  const [editing, setEditing] = useState<Ssid | 'new' | null>(null)
  // Links such as /wireless?edit=Office (from the Overview) open the editor.
  const [params, setParams] = useSearchParams()
  const editParam = params.get('edit')
  const ssidList = useApp((store) => store.state?.ssids)
  useEffect(() => {
    if (!editParam || !ssidList) return
    const target = editParam === 'new' ? 'new' : ssidList.find((s) => s.name === editParam)
    if (target) setEditing(target)
    setParams({}, { replace: true })
  }, [editParam, ssidList, setParams])
  const [advanced, setAdvanced] = useState<Ssid | null>(null)
  const [deleting, setDeleting] = useState<Ssid | null>(null)
  const [joining, setJoining] = useState<string | null>(null)
  const [scheduling, setScheduling] = useState<string | null>(null)
  const [traffic, setTraffic] = useState<string | null>(null)
  const [access, setAccess] = useState<string | null>(null)
  if (!state) return <LoadingState />
  const pendingFor = (name: string) => staged.find((c) => changeTarget(c) === `ssid:${name}`)
  const stagedNew = staged.filter((c): c is Extract<StagedChange, { kind: 'ssid-create' }> => c.kind === 'ssid-create')
  const actions = (ssid: Ssid) => (
    <div className="flex flex-wrap justify-end gap-0.5">
      <IconButton label={`Bandwidth and QoS for ${ssid.name}`} onClick={() => setTraffic(ssid.name)}><Gauge size={14} /></IconButton>
      <IconButton label={`Client access for ${ssid.name}`} onClick={() => setAccess(ssid.name)}><ShieldCheck size={14} /></IconButton>
      <IconButton label={`Join code for ${ssid.name}`} write onClick={() => setJoining(ssid.name)}>
        <QrCode size={14} />
      </IconButton>
      <IconButton label={`Schedule for ${ssid.name}`} write onClick={() => setScheduling(ssid.name)}>
        <CalendarClock size={14} />
      </IconButton>
      <IconButton label={`Advanced settings for ${ssid.name}`} onClick={() => setAdvanced(ssid)}>
        <SlidersHorizontal size={14} />
      </IconButton>
      <IconButton label={`Edit ${ssid.name}`} write onClick={() => setEditing(ssid)}>
        <Pencil size={14} />
      </IconButton>
      <IconButton label={`Delete ${ssid.name}`} write onClick={() => setDeleting(ssid)} className="hover:text-danger">
        <Trash2 size={14} />
      </IconButton>
    </div>
  )

  return (
    <Page
      title="Wireless networks"
      width="settings"
      description="Networks broadcast by this access point. Each one can use its own VLAN; addresses come from your router's DHCP, not the AP."
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
        <>
        <ul className="space-y-2 md:hidden">
          {state.ssids.map((ssid) => {
            const pending = pendingFor(ssid.name)
            const schedule = scheduleSummary(state.schedules?.[ssid.name], state.timeZone)
            return (
              <li key={ssid.name} className="rounded-lg border border-line bg-surface p-3">
                <div className="flex items-start gap-2">
                  <Dot tone={ssid.enabled ? 'ok' : 'neutral'} className="mt-1.5" />
                  <div className="min-w-0 flex-1">
                    <p className="break-words text-[14px] font-semibold text-ink">{ssid.name}</p>
                    <p className="mt-0.5 flex flex-wrap items-center gap-1.5 text-[12px] text-muted">
                      {opModeShort(ssid.opmode)}
                      <MixedBadge ssid={ssid} />
                      <span className="text-faint">·</span>
                      {plural(ssid.clients, 'client')}
                    </p>
                  </div>
                  <VlanChip vlan={ssid.vlan} />
                </div>
                <div className="mt-2 flex flex-wrap items-center gap-1">
                  {ssid.bands.map((band) => (
                    <BandChip key={band} band={band} />
                  ))}
                  {!ssid.enabled && <Badge>disabled</Badge>}
                  {ssid.hidden && <Badge>hidden</Badge>}
                  {ssid.isolation && <Badge tone="accent">isolated</Badge>}
                  {pending && <Badge tone="accent">{pending.kind === 'ssid-delete' ? 'pending delete' : 'pending change'}</Badge>}
                </div>
                {schedule && (
                  <button type="button" onClick={() => setScheduling(ssid.name)} className="mt-1.5 flex items-center gap-1 text-[11px] text-muted hover:text-accent-text">
                    <CalendarClock size={11} /> {schedule}
                  </button>
                )}
                <div className="mt-2 border-t border-line pt-1.5">{actions(ssid)}</div>
              </li>
            )
          })}
          {stagedNew.map((c) => (
            <li key={`new-${c.ssid.name}`} className="flex items-center gap-2 rounded-lg border border-dashed border-accent bg-accent-soft px-3 py-2.5 text-[13px]">
              <Plus size={13} className="text-accent-text" />
              <span className="font-medium text-ink">{c.ssid.name}</span>
              <Badge tone="accent">pending, new</Badge>
            </li>
          ))}
        </ul>
        <div className="hidden overflow-x-auto rounded-lg border border-line bg-surface md:block">
          <table className="w-full min-w-[1120px] table-fixed text-left text-[13px]">
            <colgroup>
              <col />
              <col className="w-40" />
              <col className="w-44" />
              <col className="w-28" />
              <col className="w-16" />
              <col className="w-36" />
              <col className="w-[272px]" />
            </colgroup>
            <thead className="border-b border-line text-2xs font-semibold uppercase tracking-wider text-faint">
              <tr>
                <th className="px-3 py-2">Network</th>
                <th className="px-3 py-2">Security</th>
                <th className="px-3 py-2">Bands</th>
                <th className="px-3 py-2">VLAN</th>
                <th className="px-3 py-2 text-right">Clients</th>
                <th className="px-3 py-2 text-right">Traffic</th>
                <th className="px-3 py-2"><span className="sr-only">Actions</span></th>
              </tr>
            </thead>
            <tbody className="divide-y divide-line">
              {state.ssids.map((ssid) => {
                const pending = pendingFor(ssid.name)
                const schedule = scheduleSummary(state.schedules?.[ssid.name], state.timeZone)
                return (
                <tr key={ssid.name} className="hover:bg-surface-2/50">
                  <td className="px-3 py-2.5">
                    <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                      <Dot tone={ssid.enabled ? 'ok' : 'neutral'} />
                      <span className="min-w-0 break-words font-medium text-ink">{ssid.name}</span>
                      {!ssid.enabled && <Badge>disabled</Badge>}
                      {ssid.hidden && <Badge>hidden</Badge>}
                      {ssid.isolation && <Badge tone="accent">isolated</Badge>}
                      {pending && <Badge tone="accent">{pending.kind === 'ssid-delete' ? 'pending delete' : 'pending change'}</Badge>}
                    </div>
                    {schedule && (
                      <button type="button" onClick={() => setScheduling(ssid.name)} className="ml-4 mt-0.5 flex items-center gap-1 text-[11px] text-muted hover:text-accent-text">
                        <CalendarClock size={11} /> {schedule}
                      </button>
                    )}
                  </td>
                  <td className="px-3 py-2.5 text-muted">
                    <div className="flex flex-wrap items-center gap-1.5">
                      {opModeShort(ssid.opmode)}
                      <MixedBadge ssid={ssid} />
                    </div>
                  </td>
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
                    <div className="space-y-0.5 whitespace-nowrap">
                      <div>↓ {formatBytes(ssid.rxBytes)}</div>
                      <div>↑ {formatBytes(ssid.txBytes)}</div>
                    </div>
                  </td>
                  <td className="px-3 py-2.5">
                    {actions(ssid)}
                  </td>
                </tr>
                )
              })}
              {stagedNew.map((c) => (
                <tr key={`new-${c.ssid.name}`} className="bg-accent-soft/40 text-muted">
                  <td className="px-3 py-2.5" colSpan={7}>
                    <span className="flex items-center gap-2">
                      <Plus size={13} className="text-accent-text" />
                      <span className="font-medium text-ink">{c.ssid.name}</span>
                      <Badge tone="accent">pending, new</Badge>
                      <span className="text-[12px]">{opModeShort(c.ssid.opmode)} · {c.ssid.bands.join('/')} GHz</span>
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        </>
      )}

      <div className="mt-3 flex items-start gap-2 rounded-lg border border-line bg-surface px-3 py-2.5">
        <Gauge size={14} className="mt-0.5 shrink-0 text-muted" />
        <TrafficLimitsHint />
      </div>

      <p className="mt-3 text-[12px] text-faint">
        {plural(state.ssids.length, 'network')} ·{' '}
        {plural(
          state.ssids.reduce((sum, ssid) => sum + ssid.bssids.length, 0),
          'BSSID',
        )}{' '}
        broadcasting. Saving network settings restarts Wi-Fi on this AP for a few seconds; use “Add to pending” to collect several changes and restart once.
      </p>

      {advanced && <WirelessFeaturesDialog name={advanced.name} onClose={() => setAdvanced(null)} />}
      {traffic && <TrafficDialog name={traffic} onClose={() => setTraffic(null)} />}
      {access && <ClientAccessDialog name={access} onClose={() => setAccess(null)} />}
      {joining && <JoinCodeDialog name={joining} onClose={() => setJoining(null)} />}
      {scheduling && <ScheduleDialog name={scheduling} onClose={() => setScheduling(null)} />}
      {editing && <SsidDialog ssid={editing === 'new' ? null : editing} existing={state.ssids.map((ssid) => ssid.name)} onClose={() => setEditing(null)} />}
      {deleting && <DeleteDialog ssid={deleting} onClose={() => setDeleting(null)} />}
    </Page>
  )
}

function SsidDialog({ ssid, existing, onClose }: { ssid: Ssid | null; existing: string[]; onClose: () => void }) {
  const change = useApp((store) => store.change)
  const stage = useStaging((s) => s.stage)
  const staged = useStaging((s) => s.changes)
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
  if (staged.some((c) => c.kind === 'ssid-create' && c.ssid.name === name) && name !== ssid?.name) problems.push('A pending new network already uses this name.')

  const input = (): SsidInput => ({ ...form, name, vlan })
  const save = async () => {
    setBusy(true)
    const ok = await change(ssid ? `Saved ${name}` : `Created ${name}`, () => (ssid ? api.updateSsid(ssid.name, input()) : api.createSsid(input())))
    setBusy(false)
    if (ok) onClose()
  }
  const later = () => {
    stage(ssid ? { kind: 'ssid-update', name: ssid.name, ssid: input() } : { kind: 'ssid-create', ssid: input() })
    onClose()
  }

  return (
    <Dialog title={ssid ? `Edit ${ssid.name}` : 'Add wireless network'} description="Changes apply to this access point immediately after saving." onClose={onClose} wide>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Network name (SSID)" className="sm:col-span-2">
          <Input value={form.name} maxLength={32} onChange={(event) => set('name', event.target.value)} placeholder="My network" />
        </Field>

        <Field label="Security" hint={securityHint[form.opmode]}>
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
        <Button write disabled={busy || problems.length > 0} onClick={later} title="Collect this with other changes and apply them together">
          Add to pending
        </Button>
        <Button variant="primary" disabled={busy || problems.length > 0} onClick={() => void save()}>
          {busy && <Spinner size={12} />}
          {ssid ? 'Save now' : 'Create now'}
        </Button>
      </DialogActions>
    </Dialog>
  )
}

function DeleteDialog({ ssid, onClose }: { ssid: Ssid; onClose: () => void }) {
  const change = useApp((store) => store.change)
  const stage = useStaging((s) => s.stage)
  const [busy, setBusy] = useState(false)
  return (
    <Dialog title={`Delete ${ssid.name}?`} description="The network stops broadcasting and its settings are removed from this access point." onClose={onClose}>
      {ssid.clients > 0 && <p className="text-[13px] text-warn">{plural(ssid.clients, 'client is', 'clients are')} connected right now and will be disconnected.</p>}
      <DialogActions>
        <Button onClick={onClose}>Cancel</Button>
        <Button
          write
          disabled={busy}
          onClick={() => {
            stage({ kind: 'ssid-delete', name: ssid.name })
            onClose()
          }}
        >
          Add to pending
        </Button>
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
          Delete now
        </Button>
      </DialogActions>
    </Dialog>
  )
}
