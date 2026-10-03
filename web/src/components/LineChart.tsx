import { useMemo, useRef, useState } from 'react'
import { cn } from '@/ui/cn'

export interface Series {
  label: string
  color: string
  values: (number | null)[]
}

const clock = (t: number) => new Date(t * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })

/**
 * A small time-series chart: one line per series, gaps where values are
 * missing, and a hover readout. `times` are Unix seconds.
 */
export function LineChart({
  times,
  series,
  height = 120,
  format = (v) => String(Math.round(v)),
  min,
  max,
  className,
  empty = 'Collecting data… the first point appears after a minute.',
}: {
  times: number[]
  series: Series[]
  height?: number
  format?: (value: number) => string
  min?: number
  max?: number
  className?: string
  empty?: string
}) {
  const box = useRef<HTMLDivElement>(null)
  const [hover, setHover] = useState<number | null>(null)
  const W = 1000

  const { lo, hi, paths } = useMemo(() => {
    const all = series.flatMap((s) => s.values.filter((v): v is number => v !== null))
    let lo = min ?? Math.min(0, ...all)
    let hi = max ?? Math.max(...all, lo + 1)
    if (max === undefined) hi = hi + (hi - lo) * 0.08
    if (hi <= lo) hi = lo + 1
    const t0 = times[0] ?? 0
    const span = Math.max(1, (times[times.length - 1] ?? 0) - t0)
    const x = (t: number) => ((t - t0) / span) * W
    const y = (v: number) => height - ((v - lo) / (hi - lo)) * height
    const paths = series.map((s) => {
      let d = ''
      let pen = false
      s.values.forEach((v, i) => {
        const t = times[i]
        if (v === null || t === undefined) {
          pen = false
          return
        }
        d += `${pen ? 'L' : 'M'}${x(t).toFixed(1)},${y(v).toFixed(1)}`
        pen = true
      })
      return d
    })
    return { lo, hi, paths }
  }, [series, times, height, min, max])

  if (times.length < 2) return <p className="flex items-center justify-center rounded border border-dashed border-line text-[12px] text-faint" style={{ height }}>{empty}</p>

  const t0 = times[0]!
  const span = Math.max(1, times[times.length - 1]! - t0)
  const pick = (clientX: number) => {
    const rect = box.current?.getBoundingClientRect()
    if (!rect) return
    const t = t0 + ((clientX - rect.left) / rect.width) * span
    let best = 0
    times.forEach((ti, i) => {
      if (Math.abs(ti - t) < Math.abs(times[best]! - t)) best = i
    })
    setHover(best)
  }
  const hoverLeft = hover === null ? 0 : ((times[hover]! - t0) / span) * 100

  return (
    <div className={cn('min-w-0', className)}>
      <div className="flex gap-2">
        <div className="tabular flex w-10 shrink-0 flex-col justify-between text-right text-[10px] leading-3 text-faint" style={{ height }}>
          <span>{format(hi)}</span>
          <span>{format(lo + (hi - lo) / 2)}</span>
          <span>{format(lo)}</span>
        </div>
        <div
          ref={box}
          className="relative min-w-0 flex-1 touch-none"
          style={{ height }}
          onPointerMove={(e) => pick(e.clientX)}
          onPointerDown={(e) => pick(e.clientX)}
          onPointerLeave={() => setHover(null)}
        >
          <svg viewBox={`0 0 ${W} ${height}`} preserveAspectRatio="none" className="absolute inset-0 h-full w-full overflow-visible" aria-hidden>
            {[0, 0.5, 1].map((f) => (
              <line key={f} x1={0} x2={W} y1={f * height} y2={f * height} stroke="var(--border)" strokeWidth={1} vectorEffect="non-scaling-stroke" />
            ))}
            {paths.map((d, i) => (
              <path key={series[i]!.label} d={d} fill="none" stroke={series[i]!.color} strokeWidth={1.75} strokeLinejoin="round" vectorEffect="non-scaling-stroke" />
            ))}
          </svg>
          {hover !== null && (
            <>
              <div className="pointer-events-none absolute inset-y-0 w-px bg-line-strong" style={{ left: `${hoverLeft}%` }} />
              <div
                className="pointer-events-none absolute top-0 z-10 min-w-[8rem] rounded border border-line bg-surface px-2 py-1.5 text-[11px] shadow-pop"
                style={hoverLeft > 60 ? { right: `${100 - hoverLeft}%`, marginRight: 8 } : { left: `${hoverLeft}%`, marginLeft: 8 }}
              >
                <p className="tabular mb-0.5 font-semibold text-ink">{clock(times[hover]!)}</p>
                {series.map((s) => (
                  <p key={s.label} className="flex items-center gap-1.5 whitespace-nowrap text-muted">
                    <span className="h-2 w-2 rounded-full" style={{ background: s.color }} />
                    <span className="truncate">{s.label}</span>
                    <span className="tabular ml-auto pl-2 font-medium text-ink">{s.values[hover] === null || s.values[hover] === undefined ? '—' : format(s.values[hover]!)}</span>
                  </p>
                ))}
              </div>
            </>
          )}
        </div>
      </div>
      <div className="tabular ml-12 mt-1 flex justify-between text-[10px] text-faint">
        <span>{clock(t0)}</span>
        <span>{clock(t0 + span / 2)}</span>
        <span>{clock(t0 + span)}</span>
      </div>
      {series.length > 1 && (
        <div className="ml-12 mt-1.5 flex flex-wrap gap-x-3 gap-y-0.5 text-[11px] text-muted">
          {series.map((s) => (
            <span key={s.label} className="flex items-center gap-1.5">
              <span className="h-0.5 w-3 rounded" style={{ background: s.color }} />
              {s.label}
            </span>
          ))}
        </div>
      )}
    </div>
  )
}
