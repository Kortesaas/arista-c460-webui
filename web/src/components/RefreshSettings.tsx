import { useEffect, useState } from 'react'
import { Save } from 'lucide-react'
import { api } from '@/api'
import { useApp } from '@/stores/app'
import { Button, Field, Panel, Select, Spinner } from '@/ui/kit'
export function RefreshPanel() {
  const current = useApp((s) => s.state?.pollSeconds ?? 5)
  const [seconds, setSeconds] = useState(current)
  const [busy, setBusy] = useState(false)
  const toast = useApp((s) => s.toast)
  const refresh = useApp((s) => s.refresh)
  useEffect(() => {
    setSeconds(current)
  }, [current])
  const save = async () => {
    if (busy) return
    setBusy(true)
    try {
      await api.updateRefresh(seconds)
      await refresh()
      toast('Update interval saved.', 'ok')
    } catch (e) {
      toast(e instanceof Error ? e.message : String(e), 'danger')
    } finally {
      setBusy(false)
    }
  }
  return (
    <Panel title="Live updates" help="How often the AP is sampled. All open browsers share the same samples, and updates pause in background tabs. Changing it does not interrupt Wi-Fi.">
      <div className="flex items-end gap-2">
        <Field label="AP data update interval" className="min-w-0 flex-1">
          <Select value={seconds} disabled={busy} onChange={(e) => setSeconds(Number(e.target.value))}>
            {[...new Set([1, 2, 5, 10, 30, 60, current])]
              .sort((a, b) => a - b)
              .map((n) => (
                <option key={n} value={n}>
                  {n} {n === 1 ? 'second' : 'seconds'}
                  {n === 5 ? ' · recommended' : ''}
                </option>
              ))}
          </Select>
        </Field>
        <Button write disabled={busy || seconds === current} onClick={() => void save()}>
          {busy ? <Spinner size={13} /> : <Save size={13} />} Save
        </Button>
      </div>
      {seconds < 5 && <p className="mt-2 text-[11px] leading-4 text-warn">Fast sampling increases AP CPU load. Return to five seconds when done.</p>}
    </Panel>
  )
}
