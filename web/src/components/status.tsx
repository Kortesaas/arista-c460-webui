import type { ReactNode } from 'react'
import type { Band } from '@/types'
import { cn } from '@/ui/cn'
import { Badge, type Tone } from '@/ui/kit'
import { formatAge, signalQuality } from '@/utils/format'
import { useApp } from '@/stores/app'

/** "12s ago", ticking with the store clock. */
export function Age({ iso, className, prefix }: { iso: string | null | undefined; className?: string; prefix?: string }) {
  const now = useApp((store) => store.now)
  return (
    <span className={cn('tabular whitespace-nowrap', className)} title={iso ? new Date(iso).toLocaleString() : undefined}>
      {prefix}
      {formatAge(iso, now)}
    </span>
  )
}

export function Dot({ tone, pulse, className }: { tone: Tone; pulse?: boolean; className?: string }) {
  const color: Record<Tone, string> = { ok: 'bg-ok', warn: 'bg-warn', danger: 'bg-danger', accent: 'bg-accent', neutral: 'bg-faint' }
  return <span className={cn('inline-block h-2 w-2 shrink-0 rounded-full', color[tone], pulse && tone === 'ok' && 'pulse-ok', className)} />
}

/** Big number used in overview tiles. */
export function Stat({ label, value, detail, tone = 'neutral', icon }: { label: string; value: ReactNode; detail?: ReactNode; tone?: Tone; icon?: ReactNode }) {
  const valueColor: Record<Tone, string> = { neutral: 'text-ink', accent: 'text-accent-text', ok: 'text-ok', warn: 'text-warn', danger: 'text-danger' }
  return (
    <div className="min-w-0 rounded-lg border border-line bg-surface p-3">
      <div className="flex items-center justify-between gap-2">
        <span className="truncate text-[12px] font-medium text-muted">{label}</span>
        {icon && <span className="text-faint">{icon}</span>}
      </div>
      <p className={cn('tabular mt-1.5 text-2xl font-semibold leading-none', valueColor[tone])}>{value}</p>
      {detail && <p className="mt-1.5 truncate text-[11px] text-faint">{detail}</p>}
    </div>
  )
}

const bandColor: Record<Band, string> = { '2.4': '#f0b357', '5': '#5b93ff', '6': '#3ecf8e' }

export function BandChip({ band, muted }: { band: Band | ''; muted?: boolean }) {
  if (!band) return <span className="text-faint">—</span>
  return (
    <span
      className={cn('inline-flex items-center gap-1 whitespace-nowrap rounded-sm border px-1.5 text-2xs font-semibold leading-4', muted ? 'border-line text-faint' : 'border-transparent')}
      style={muted ? undefined : { background: `${bandColor[band]}22`, color: bandColor[band] }}
    >
      {band} GHz
    </span>
  )
}

// A stable, readable colour per VLAN id (no site-specific palette is assumed).
const vlanPalette = ['#2f80ed', '#4caf50', '#c8393a', '#9c37b5', '#e08e0b', '#0f9d9a', '#d1477a', '#6d6af0', '#7a8b2c', '#b5651d']
export function vlanColor(vlan: number) {
  return vlanPalette[vlan % vlanPalette.length] ?? '#64748b'
}

export function VlanChip({ vlan, className }: { vlan: number | null; className?: string }) {
  const names = useApp((store) => store.state?.vlanNames)
  if (vlan === null)
    return (
      <span title="Untagged on the AP's management/native network" className={cn('inline-flex items-center whitespace-nowrap rounded-sm border border-line px-1.5 text-2xs font-semibold leading-4 text-muted', className)}>
        untagged
      </span>
    )
  const name = names?.[String(vlan)]
  const color = vlanColor(vlan)
  return (
    <span title={`VLAN ${vlan}${name ? ` · ${name}` : ''}`} className={cn('inline-flex items-center gap-1 whitespace-nowrap rounded-sm px-1.5 text-2xs font-semibold leading-4 text-white', className)} style={{ background: color }}>
      <span className="tabular">VLAN {vlan}</span>
      {name && <span className="uppercase tracking-wide opacity-90">{name}</span>}
    </span>
  )
}

export function SignalBars({ rssi }: { rssi: number | null }) {
  const quality = signalQuality(rssi)
  const color: Record<string, string> = { ok: 'bg-ok', warn: 'bg-warn', danger: 'bg-danger', neutral: 'bg-faint' }
  return (
    <span className="inline-flex items-center gap-1.5" title={rssi === null ? 'No signal reading' : `${rssi} dBm · ${quality.label}`}>
      <span className="flex h-3 items-end gap-[2px]">
        {[1, 2, 3, 4].map((bar) => (
          <span key={bar} className={cn('w-[3px] rounded-[1px]', bar <= quality.bars ? color[quality.tone] : 'bg-surface-3')} style={{ height: `${bar * 25}%` }} />
        ))}
      </span>
      <span className="tabular text-[12px] text-muted">{rssi === null ? '—' : `${rssi} dBm`}</span>
    </span>
  )
}

/** Horizontal utilisation meter, 0–100 %. */
export function Meter({ value, label, warnAt = 50, dangerAt = 80 }: { value: number | null; label?: string; warnAt?: number; dangerAt?: number }) {
  const pct = Math.max(0, Math.min(100, value ?? 0))
  const tone = value === null ? 'bg-faint' : pct >= dangerAt ? 'bg-danger' : pct >= warnAt ? 'bg-warn' : 'bg-ok'
  return (
    <div className="min-w-0">
      {label && (
        <div className="mb-1 flex items-center justify-between text-[11px]">
          <span className="text-faint">{label}</span>
          <span className="tabular text-muted">{value === null ? '—' : `${Math.round(pct)} %`}</span>
        </div>
      )}
      <div className="h-1.5 overflow-hidden rounded-full bg-surface-3">
        <div className={cn('h-full rounded-full transition-[width]', tone)} style={{ width: `${pct}%` }} />
      </div>
    </div>
  )
}

export function OnOff({ on, onLabel = 'Enabled', offLabel = 'Disabled' }: { on: boolean; onLabel?: string; offLabel?: string }) {
  return (
    <Badge tone={on ? 'ok' : 'neutral'}>
      <Dot tone={on ? 'ok' : 'neutral'} />
      {on ? onLabel : offLabel}
    </Badge>
  )
}
