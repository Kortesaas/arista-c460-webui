import { useState } from 'react'
import { Link } from 'react-router-dom'
import { Plus, Trash2 } from 'lucide-react'
import { api } from '@/api'
import { useApp } from '@/stores/app'
import type { ScheduleStatus, ScheduleWindow, SsidSchedule } from '@/types'
import { cn } from '@/ui/cn'
import { Button, Dialog, DialogActions, IconButton, Input, Spinner, Toggle } from '@/ui/kit'

// Monday first, as on most European calendars; values are JS weekdays.
const DAYS = [
  { d: 1, short: 'Mo' },
  { d: 2, short: 'Tu' },
  { d: 3, short: 'We' },
  { d: 4, short: 'Th' },
  { d: 5, short: 'Fr' },
  { d: 6, short: 'Sa' },
  { d: 0, short: 'Su' },
]
const dayName = (d: number) => DAYS.find((x) => x.d === d)?.short ?? '?'

export function describeWindow(w: ScheduleWindow) {
  const sorted = DAYS.filter((x) => w.days.includes(x.d)).map((x) => x.d)
  let days = sorted.map(dayName).join(', ')
  if (sorted.length === 7) days = 'Every day'
  else if (sorted.join() === '1,2,3,4,5') days = 'Mon–Fri'
  else if (sorted.join() === '6,0') days = 'Weekends'
  const overnight = w.end <= w.start
  return `${days} ${w.start}–${w.end}${overnight ? ' (next day)' : ''}`
}

/** "on until 18:00" / "off until Mon 08:00" in the AP's time zone. */
export function scheduleSummary(s: ScheduleStatus | undefined, timeZone: string) {
  if (!s?.enabled) return null
  if (s.waiting) return 'Schedule waiting for clock sync'
  if (!s.next) return s.active ? 'Scheduled, always on' : 'Scheduled, always off'
  const next = new Date(s.next)
  const sameDay = new Intl.DateTimeFormat('en-CA', { timeZone, dateStyle: 'short' })
  const when =
    sameDay.format(next) === sameDay.format(new Date())
      ? next.toLocaleTimeString([], { timeZone, hour: '2-digit', minute: '2-digit' })
      : next.toLocaleString([], { timeZone, weekday: 'short', hour: '2-digit', minute: '2-digit' })
  return `${s.active ? 'On' : 'Off'} until ${when}`
}

export function ScheduleDialog({ name, onClose }: { name: string; onClose: () => void }) {
  const { state, toast, refresh } = useApp()
  const current = state?.schedules?.[name]
  const timeZone = state?.timeZone ?? 'UTC'
  const [form, setForm] = useState<SsidSchedule>(() => ({
    enabled: current?.enabled ?? true,
    windows: current?.windows?.length ? current.windows : [{ days: [1, 2, 3, 4, 5], start: '08:00', end: '18:00' }],
  }))
  const [busy, setBusy] = useState(false)
  const update = (i: number, w: Partial<ScheduleWindow>) => setForm((f) => ({ ...f, windows: f.windows.map((x, j) => (j === i ? { ...x, ...w } : x)) }))
  const problems = form.enabled
    ? [
        ...(form.windows.length === 0 ? ['Add at least one time window.'] : []),
        ...form.windows.flatMap((w, i) => (w.days.length === 0 ? [`Window ${i + 1}: select at least one day.`] : [])),
      ]
    : []

  const save = async () => {
    setBusy(true)
    try {
      await api.updateSchedule(name, form)
      toast(form.enabled ? `Schedule saved for “${name}”.` : `Schedule turned off for “${name}”.`, 'ok')
      void refresh()
      onClose()
    } catch (e) {
      toast(e instanceof Error ? e.message : String(e), 'danger')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog title={`Schedule for “${name}”`} description="The network broadcasts only during these times. Switching it on or off by hand lasts until the next scheduled change." onClose={() => !busy && onClose()} wide>
      <Toggle checked={form.enabled} onChange={(enabled) => setForm((f) => ({ ...f, enabled }))} label="Use a schedule" />
      <div className={cn('mt-3 space-y-2', !form.enabled && 'pointer-events-none opacity-40')}>
        {form.windows.map((w, i) => (
          <div key={i} className="flex flex-wrap items-center gap-2 rounded border border-line bg-surface-2 p-2">
            <div className="flex gap-1" role="group" aria-label={`Days for window ${i + 1}`}>
              {DAYS.map(({ d, short }) => {
                const on = w.days.includes(d)
                return (
                  <button
                    key={d}
                    type="button"
                    aria-pressed={on}
                    onClick={() => update(i, { days: on ? w.days.filter((x) => x !== d) : [...w.days, d] })}
                    className={cn(
                      'h-7 w-8 rounded border text-[11px] font-semibold transition-colors',
                      on ? 'border-accent bg-accent-soft text-accent-text' : 'border-line bg-surface text-muted hover:text-ink',
                    )}
                  >
                    {short}
                  </button>
                )
              })}
            </div>
            <div className="flex items-center gap-1.5">
              <Input type="time" aria-label="From" value={w.start} onChange={(e) => update(i, { start: e.target.value })} className="h-7 w-[6.5rem]" />
              <span className="text-faint">–</span>
              <Input type="time" aria-label="Until" value={w.end} onChange={(e) => update(i, { end: e.target.value })} className="h-7 w-[6.5rem]" />
            </div>
            {w.end <= w.start && <span className="text-[11px] text-faint">ends the next day</span>}
            <IconButton label="Remove window" size="md" className="ml-auto hover:text-danger" onClick={() => setForm((f) => ({ ...f, windows: f.windows.filter((_, j) => j !== i) }))}>
              <Trash2 size={13} />
            </IconButton>
          </div>
        ))}
        {form.windows.length < 14 && (
          <Button size="sm" variant="ghost" onClick={() => setForm((f) => ({ ...f, windows: [...f.windows, { days: [6, 0], start: '10:00', end: '22:00' }] }))}>
            <Plus size={13} /> Add time window
          </Button>
        )}
      </div>
      <p className="mt-3 text-[12px] text-muted">
        Times are in the AP's time zone, <span className="font-medium text-ink">{timeZone}</span>.{' '}
        <Link to="/network" className="text-accent-text hover:underline" onClick={onClose}>
          Change
        </Link>
        {current?.waiting && <span className="mt-1 block text-warn">{current.waiting}.</span>}
      </p>
      {problems.length > 0 && (
        <ul className="mt-2 text-[12px] text-warn">
          {problems.map((p) => (
            <li key={p}>{p}</li>
          ))}
        </ul>
      )}
      <DialogActions>
        <Button disabled={busy} onClick={onClose}>
          Cancel
        </Button>
        <Button variant="primary" disabled={busy || problems.length > 0} onClick={() => void save()}>
          {busy && <Spinner size={12} />} Save schedule
        </Button>
      </DialogActions>
    </Dialog>
  )
}
