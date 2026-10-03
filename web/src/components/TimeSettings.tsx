import { useEffect, useState } from 'react'
import { Pencil, RefreshCw, Save } from 'lucide-react'
import { api } from '@/api'
import { useApp } from '@/stores/app'
import type { TimeSettings } from '@/types'
import { Badge, Button, Dialog, DialogActions, Field, Input, KeyValue, Panel, Select, Spinner } from '@/ui/kit'

// Every IANA zone the browser knows; the AP embeds the same database.
const ZONES: string[] = (() => {
  try {
    return ['UTC', ...(Intl as unknown as { supportedValuesOf(k: string): string[] }).supportedValuesOf('timeZone').filter((z) => z !== 'UTC')]
  } catch {
    return ['UTC', 'Europe/Berlin', 'Europe/London', 'America/New_York', 'America/Los_Angeles', 'Asia/Tokyo']
  }
})()
const browserZone = Intl.DateTimeFormat().resolvedOptions().timeZone

export function TimePanel() {
  const timeZone = useApp((s) => s.state?.timeZone) ?? 'UTC'
  const now = useApp((s) => s.now)
  const [settings, setSettings] = useState<TimeSettings | null>(null)
  const [error, setError] = useState('')
  const [editing, setEditing] = useState(false)
  const [loading, setLoading] = useState(false)
  const load = async () => {
    setLoading(true)
    try {
      setSettings(await api.time())
      setError('')
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setLoading(false)
    }
  }
  useEffect(() => {
    let active = true
    api
      .time()
      .then((s) => {
        if (active) setSettings(s)
      })
      .catch((e) => {
        if (active) setError(String(e.message))
      })
    return () => {
      active = false
    }
  }, [])
  return (
    <Panel
      title="Time synchronisation"
      help="An accurate clock keeps event times, logs and network schedules correct. Changing servers restarts time synchronisation only, Wi-Fi keeps running. The time zone is used for schedules."
      actions={
        <>
          <Button size="sm" disabled={loading} aria-label="Refresh time settings" onClick={() => void load()}>
            {loading ? <Spinner size={12} /> : <RefreshCw size={12} />}
          </Button>
          <Button size="sm" write disabled={!settings || loading || Boolean(error)} onClick={() => setEditing(true)}>
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
        <p className="text-[12px] text-muted">Reading time settings…</p>
      ) : (
        <>
          <KeyValue
            items={[
              { label: 'Primary server', value: settings.primary, mono: true },
              { label: 'Secondary server', value: settings.secondary || '—', mono: true },
              {
                label: 'Clock',
                value: <Badge tone={settings.synced ? 'ok' : 'warn'}>{settings.synced === null ? 'Unknown' : settings.synced ? 'Synchronised' : 'Not synchronised'}</Badge>,
              },
              { label: 'NTP service', value: settings.running ? 'Running' : 'Stopped' },
              {
                label: 'Time zone',
                value: (
                  <span>
                    {timeZone} <span className="tabular text-faint">· {new Date(now).toLocaleTimeString([], { timeZone, hour: '2-digit', minute: '2-digit' })}</span>
                  </span>
                ),
              },
            ]}
          />
        </>
      )}
      {editing && settings && (
        <TimeDialog
          settings={settings}
          timeZone={timeZone}
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
function TimeDialog({ settings, timeZone, onClose, onSaved }: { settings: TimeSettings; timeZone: string; onClose: () => void; onSaved: () => void }) {
  const [primary, setPrimary] = useState(settings.primary)
  const [secondary, setSecondary] = useState(settings.secondary)
  const [zone, setZone] = useState(timeZone)
  const [busy, setBusy] = useState(false)
  const toast = useApp((s) => s.toast)
  const refresh = useApp((s) => s.refresh)
  const gateway = useApp((s) => s.state?.device.gateway)
  const save = async () => {
    setBusy(true)
    try {
      if (primary.trim() !== settings.primary || secondary.trim() !== settings.secondary) await api.updateTime(primary.trim(), secondary.trim())
      if (zone !== timeZone) await api.updateTimeZone(zone)
      toast('Time settings saved.', 'ok')
      void refresh()
      onSaved()
    } catch (e) {
      toast(e instanceof Error ? e.message : String(e), 'danger')
    } finally {
      setBusy(false)
    }
  }
  return (
    <Dialog
      title="Time synchronisation"
      description="Use a reachable NTP server on your network or the internet."
      onClose={() => {
        if (!busy) onClose()
      }}
    >
      <form
        onSubmit={(e) => {
          e.preventDefault()
          if (!busy) void save()
        }}
        className="space-y-3"
      >
        <Field label="Primary NTP server" hint="Hostname or IP address. Most routers answer NTP; that works without internet access.">
          <div className="flex gap-1.5">
            <Input required maxLength={253} autoCapitalize="none" spellCheck={false} value={primary} onChange={(e) => setPrimary(e.target.value)} />
            {gateway && primary.trim() !== gateway && (
              <Button write={false} onClick={() => setPrimary(gateway)} title={`Use the default gateway ${gateway} as time server`}>
                Use router
              </Button>
            )}
          </div>
        </Field>
        <Field label="Secondary NTP server" hint="Optional fallback">
          <Input maxLength={253} autoCapitalize="none" spellCheck={false} value={secondary} onChange={(e) => setSecondary(e.target.value)} />
        </Field>
        <Field label="Time zone" hint={zone !== browserZone && ZONES.includes(browserZone) ? undefined : 'Used for network schedules.'}>
          <div className="flex gap-1.5">
            <Select value={zone} onChange={(e) => setZone(e.target.value)}>
              {(ZONES.includes(zone) ? ZONES : [zone, ...ZONES]).map((z) => (
                <option key={z} value={z}>
                  {z.replaceAll('_', ' ')}
                </option>
              ))}
            </Select>
            {zone !== browserZone && ZONES.includes(browserZone) && (
              <Button write={false} onClick={() => setZone(browserZone)} title="Use the time zone of this computer">
                Use {browserZone.split('/').pop()?.replaceAll('_', ' ')}
              </Button>
            )}
          </div>
        </Field>
        <DialogActions>
          <Button disabled={busy} onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" disabled={busy || !primary.trim()}>
            {busy ? <Spinner size={13} /> : <Save size={13} />} Save
          </Button>
        </DialogActions>
      </form>
    </Dialog>
  )
}
