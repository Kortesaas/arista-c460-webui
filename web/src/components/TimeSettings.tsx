import { useEffect, useState } from 'react'
import { Pencil, RefreshCw, Save } from 'lucide-react'
import { api } from '@/api'
import { useApp } from '@/stores/app'
import type { TimeSettings } from '@/types'
import { Badge, Button, Dialog, DialogActions, Field, Input, KeyValue, Panel, Spinner } from '@/ui/kit'

export function TimePanel() {
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
      help="An accurate clock keeps event times and logs correct. Changing servers restarts time synchronisation only, Wi-Fi keeps running."
      actions={
        <>
          <Button size="sm" disabled={loading} aria-label="Refresh time settings" onClick={() => void load()}>
            {loading ? <Spinner size={12} /> : <RefreshCw size={12} />}
          </Button>
          <Button size="sm" disabled={!settings || loading || Boolean(error)} onClick={() => setEditing(true)}>
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
            ]}
          />
        </>
      )}
      {editing && settings && (
        <TimeDialog
          settings={settings}
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
function TimeDialog({ settings, onClose, onSaved }: { settings: TimeSettings; onClose: () => void; onSaved: () => void }) {
  const [primary, setPrimary] = useState(settings.primary)
  const [secondary, setSecondary] = useState(settings.secondary)
  const [busy, setBusy] = useState(false)
  const toast = useApp((s) => s.toast)
  const save = async () => {
    setBusy(true)
    try {
      await api.updateTime(primary.trim(), secondary.trim())
      toast('Time servers saved.', 'ok')
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
        <Field label="Primary NTP server" hint="Hostname or IP address">
          <Input required maxLength={253} autoCapitalize="none" spellCheck={false} value={primary} onChange={(e) => setPrimary(e.target.value)} />
        </Field>
        <Field label="Secondary NTP server" hint="Optional fallback">
          <Input maxLength={253} autoCapitalize="none" spellCheck={false} value={secondary} onChange={(e) => setSecondary(e.target.value)} />
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
