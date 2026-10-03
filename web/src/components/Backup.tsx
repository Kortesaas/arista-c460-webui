import { useRef, useState } from 'react'
import { Download, Upload } from 'lucide-react'
import { api } from '@/api'
import { useApp } from '@/stores/app'
import type { ConfigBackup, ManagementInput, RestoreResult, RestoreSections } from '@/types'
import { Button, Dialog, DialogActions, Field, Input, Panel, Spinner, Toggle } from '@/ui/kit'
import { BandChip, VlanChip } from '@/components/status'
import { formatAge } from '@/utils/format'

/** Export the AP configuration to a file, or apply a file to this AP. */
export function BackupPanel() {
  const toast = useApp((s) => s.toast)
  const hostname = useApp((s) => s.state?.device.hostname) ?? 'ap'
  const [passphrase, setPassphrase] = useState('')
  const [busy, setBusy] = useState(false)
  const [loaded, setLoaded] = useState<ConfigBackup | null>(null)
  const file = useRef<HTMLInputElement>(null)

  const exportBackup = async () => {
    if (passphrase && passphrase.length < 8) {
      toast('The passphrase needs at least 8 characters.', 'danger')
      return
    }
    setBusy(true)
    try {
      const backup = await api.backup(passphrase)
      const url = URL.createObjectURL(new Blob([JSON.stringify(backup, null, 2)], { type: 'application/json' }))
      const a = document.createElement('a')
      a.href = url
      a.download = `c460-${hostname}-${new Date().toISOString().slice(0, 10)}.json`
      document.body.appendChild(a)
      a.click()
      a.remove()
      setTimeout(() => URL.revokeObjectURL(url), 1000)
      toast(passphrase ? 'Backup downloaded, Wi-Fi passwords encrypted with your passphrase.' : 'Backup downloaded without Wi-Fi passwords.')
      setPassphrase('')
    } catch (error) {
      toast(error instanceof Error ? error.message : String(error), 'danger')
    } finally {
      setBusy(false)
    }
  }

  const open = async (input: HTMLInputElement) => {
    const f = input.files?.[0]
    input.value = ''
    if (!f) return
    try {
      if (f.size > 1_000_000) throw new Error('This file is too large to be a backup.')
      const parsed = JSON.parse(await f.text()) as ConfigBackup
      if (parsed?.format !== 'c460-webui-backup') throw new Error('This is not a backup file from this web interface.')
      setLoaded(parsed)
    } catch (error) {
      toast(error instanceof Error ? error.message : 'The file could not be read.', 'danger')
    }
  }

  return (
    <Panel
      title="Backup and restore"
      help="A backup contains wireless networks, radios, management network, names, time servers and LLDP timing. Use it to restore this AP or to set up another C-460 the same way. Wi-Fi passwords are only included when you set a passphrase, and then encrypted with it."
    >
      <div className="grid gap-3 sm:grid-cols-[1fr_auto] sm:items-end">
        <Field label="Passphrase for Wi-Fi passwords" hint="Optional. Leave empty to export without passwords.">
          <Input type="password" autoComplete="new-password" value={passphrase} onChange={(e) => setPassphrase(e.target.value)} placeholder="At least 8 characters" />
        </Field>
        <Button variant="primary" disabled={busy} onClick={() => void exportBackup()}>
          {busy ? <Spinner size={12} /> : <Download size={13} />} Download backup
        </Button>
      </div>
      <div className="mt-3 flex items-center justify-between gap-3 border-t border-line pt-3">
        <p className="text-[13px] text-ink">Restore or copy from a file</p>
        <input ref={file} type="file" accept="application/json,.json" className="hidden" onChange={(e) => void open(e.target)} />
        <Button onClick={() => file.current?.click()}>
          <Upload size={13} /> Choose file…
        </Button>
      </div>
      {loaded && <RestoreDialog backup={loaded} onClose={() => setLoaded(null)} />}
    </Panel>
  )
}

function RestoreDialog({ backup, onClose }: { backup: ConfigBackup; onClose: () => void }) {
  const { toast, refresh, state } = useApp()
  const sameAp = backup.source.hostname === state?.device.hostname
  const [sections, setSections] = useState<RestoreSections>({ wireless: true, radios: true, management: false, labels: sameAp, time: true, lldp: true })
  const [passphrase, setPassphrase] = useState('')
  const [removeOthers, setRemoveOthers] = useState(false)
  const [ipv4, setIpv4] = useState(sameAp ? (backup.management?.ipv4 ?? '') : '')
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<RestoreResult | null>(null)
  const set = (key: keyof RestoreSections, value: boolean) => setSections((s) => ({ ...s, [key]: value }))
  const wpaWithoutSecret = !backup.secrets && backup.ssids.some((s) => (s.opmode === 'WPA3_SAE' || s.opmode === 'WPA2_PERSONAL') && !state?.ssids.some((own) => own.name === s.name))

  const apply = async () => {
    setBusy(true)
    try {
      const management: ManagementInput | undefined = sections.management && backup.management ? { ...backup.management, ipv4: ipv4.trim() || backup.management.ipv4 } : undefined
      const r = await api.restore(backup, passphrase, sections, removeOthers, management)
      setResult(r)
      toast(r.ok ? 'Configuration applied. Wi-Fi restarts for a few seconds.' : 'Configuration partly applied, see the details.', r.ok ? 'ok' : 'danger')
      void refresh()
    } catch (error) {
      toast(error instanceof Error ? error.message : String(error), 'danger')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog
      wide
      title={sameAp ? 'Restore backup' : 'Copy configuration to this AP'}
      description={`From ${backup.source.hostname} (${backup.source.model}, firmware ${backup.source.firmware}), created ${formatAge(backup.createdAt)}.`}
      onClose={() => !busy && onClose()}
    >
      {result ? (
        <div className="space-y-2 text-[13px]">
          <p className="text-ink">Applied: {result.applied.join(', ') || 'nothing'}.</p>
          {result.problems?.map((p) => (
            <p key={p} className="text-danger">
              {p}
            </p>
          ))}
          {result.rebootRequired && <p className="text-warn">The management network changes when the AP restarts. Use Restart under System when ready.</p>}
          <DialogActions>
            <Button variant="primary" onClick={onClose}>
              Done
            </Button>
          </DialogActions>
        </div>
      ) : (
        <>
          <div className="max-h-44 overflow-auto rounded border border-line">
            <table className="w-full text-left text-[12px]">
              <tbody className="divide-y divide-line">
                {backup.ssids.map((s) => (
                  <tr key={s.name}>
                    <td className="px-2 py-1.5 font-medium text-ink">{s.name}</td>
                    <td className="px-2 py-1.5">
                      <VlanChip vlan={s.vlan} />
                    </td>
                    <td className="px-2 py-1.5">
                      <span className="flex gap-1">
                        {s.bands.map((b) => (
                          <BandChip key={b} band={b} />
                        ))}
                      </span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <div className="mt-3 grid gap-x-6 sm:grid-cols-2">
            <Toggle checked={sections.wireless} onChange={(v) => set('wireless', v)} label={`Wireless networks (${backup.ssids.length})`} />
            <Toggle checked={sections.radios} onChange={(v) => set('radios', v)} label="Radio channels and power" />
            <Toggle checked={sections.time} onChange={(v) => set('time', v)} label="Time servers" />
            <Toggle checked={sections.lldp} onChange={(v) => set('lldp', v)} label="LLDP timing" />
            <Toggle checked={sections.labels} onChange={(v) => set('labels', v)} label="Device name and VLAN labels" help="Usually only wanted when restoring the same AP; a copied AP should keep its own name." />
            <Toggle
              checked={sections.management}
              onChange={(v) => set('management', v)}
              label="Management network"
              help="Address, gateway and DNS. Off by default so a copy does not take over the other AP's IP address. Applies after a restart."
            />
          </div>
          {sections.wireless && (
            <div className="mt-2">
              <Toggle checked={removeOthers} onChange={setRemoveOthers} label="Remove networks that are not in the backup" />
            </div>
          )}
          {sections.management && backup.management && (
            <Field label="IPv4 address for this AP" className="mt-3" hint={`Backup: ${backup.management.mode === 'dhcp' ? 'DHCP' : backup.management.ipv4}`}>
              <Input value={ipv4} onChange={(e) => setIpv4(e.target.value)} placeholder={backup.management.ipv4} />
            </Field>
          )}
          {sections.wireless && Boolean(backup.secrets) && (
            <Field label="Backup passphrase" className="mt-3" hint="Needed to restore the Wi-Fi passwords. Without it, existing networks keep their current password.">
              <Input type="password" autoComplete="off" value={passphrase} onChange={(e) => setPassphrase(e.target.value)} />
            </Field>
          )}
          {sections.wireless && wpaWithoutSecret && (
            <p className="mt-3 text-[12px] text-warn">This backup has no passwords, and some networks do not exist on this AP yet. Restore would fail for them; export the backup with a passphrase.</p>
          )}
          <DialogActions>
            <Button disabled={busy} onClick={onClose}>
              Cancel
            </Button>
            <Button variant="primary" disabled={busy || !Object.values(sections).some(Boolean)} onClick={() => void apply()}>
              {busy && <Spinner size={12} />}
              Apply to this AP
            </Button>
          </DialogActions>
        </>
      )}
    </Dialog>
  )
}
