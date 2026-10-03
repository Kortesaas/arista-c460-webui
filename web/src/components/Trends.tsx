import { useCallback, useEffect, useMemo, useState } from 'react'
import { api } from '@/api'
import { LineChart, type Series } from '@/components/LineChart'
import { bandColor, vlanColor } from '@/components/status'
import { useApp } from '@/stores/app'
import type { Band, HistoryPoint } from '@/types'
import { Panel, Segmented } from '@/ui/kit'

type Metric = 'clients' | 'traffic' | 'utilization' | 'temperature'
const RANGES = [
  { value: 1, label: '1 h' },
  { value: 6, label: '6 h' },
  { value: 24, label: '24 h' },
]

const rate = (bytesPerSecond: number) => {
  const bits = bytesPerSecond * 8
  if (bits >= 1e9) return `${(bits / 1e9).toFixed(1)} Gb/s`
  if (bits >= 1e6) return `${(bits / 1e6).toFixed(bits >= 1e7 ? 0 : 1)} Mb/s`
  if (bits >= 1e3) return `${Math.round(bits / 1e3)} kb/s`
  return `${Math.round(bits)} b/s`
}

function readStored<T>(key: string, fallback: T, valid: (v: unknown) => v is T): T {
  try {
    const raw = localStorage.getItem(key)
    const parsed: unknown = raw === null ? null : JSON.parse(raw)
    return valid(parsed) ? parsed : fallback
  } catch {
    return fallback
  }
}
function store(key: string, value: unknown) {
  try {
    localStorage.setItem(key, JSON.stringify(value))
  } catch {
    /* per-viewer convenience only */
  }
}

/** History graphs from the AP's in-memory buffer (one point per minute, 24 hours). */
export function TrendsPanel() {
  const generatedAt = useApp((s) => s.state?.generatedAt)
  const ssids = useApp((s) => s.state?.ssids)
  const [hours, setHours] = useState(() => readStored('c460-trend-hours', 6, (v): v is number => v === 1 || v === 6 || v === 24))
  const [metric, setMetric] = useState<Metric>(() =>
    readStored('c460-trend-metric', 'clients', (v): v is Metric => v === 'clients' || v === 'traffic' || v === 'utilization' || v === 'temperature'),
  )
  const [points, setPoints] = useState<HistoryPoint[] | null>(null)
  const [error, setError] = useState('')

  const load = useCallback(() => {
    api
      .history(hours)
      .then((r) => {
        setPoints(r.points)
        setError('')
      })
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [hours])

  // Refresh about once a minute, following the live sample clock.
  const minute = generatedAt ? Math.floor(Date.parse(generatedAt) / 60000) : 0
  useEffect(() => {
    load()
  }, [load, minute])

  const chart = useMemo(() => {
    const p = points ?? []
    const times = p.map((x) => x.t)
    let series: Series[] = []
    let format: (v: number) => string = (v) => String(Math.round(v))
    let min: number | undefined
    let max: number | undefined
    if (metric === 'clients') {
      const names = [...new Set(p.flatMap((x) => Object.keys(x.perSsid)))].sort()
      series = [{ label: 'All clients', color: 'var(--accent)', values: p.map((x) => x.clients) }]
      if (names.length > 1)
        series.push(
          ...names.map((name) => {
            const vlan = ssids?.find((s) => s.name === name)?.vlan
            return { label: name, color: vlan ? vlanColor(vlan) : 'var(--text-faint)', values: p.map((x) => x.perSsid[name] ?? 0) }
          }),
        )
      min = 0
      // Whole clients: keep the axis at even steps (0, 2, 4 …) instead of fractions.
      const peak = Math.max(0, ...p.map((x) => x.clients))
      max = Math.max(4, Math.ceil((peak * 1.15) / 2) * 2)
    } else if (metric === 'traffic') {
      series = [
        { label: 'Download (from the network)', color: 'var(--accent)', values: p.map((x) => x.rxBps) },
        { label: 'Upload (to the network)', color: 'var(--ok)', values: p.map((x) => x.txBps) },
      ]
      format = rate
      min = 0
    } else if (metric === 'utilization') {
      const bands = (['2.4', '5', '6'] as Band[]).filter((b) => p.some((x) => x.util[b] !== undefined))
      series = bands.map((b) => ({ label: `${b} GHz`, color: bandColor(b), values: p.map((x) => x.util[b] ?? null) }))
      format = (v) => `${Math.round(v)} %`
      min = 0
      max = 100
    } else {
      series = [{ label: 'Temperature', color: 'var(--warn)', values: p.map((x) => x.tempC) }]
      format = (v) => `${Math.round(v)} °C`
    }
    return { times, series, format, min, max }
  }, [points, metric, ssids])

  return (
    <Panel
      title="History"
      help="Kept in the AP's memory: one point per minute for the last 24 hours. It starts empty after the web interface restarts. Clients and temperature show the peak of each minute."
      actions={
        <Segmented
          value={hours}
          onChange={(v) => {
            setHours(v)
            store('c460-trend-hours', v)
          }}
          options={RANGES}
        />
      }
    >
      <Segmented
        value={metric}
        onChange={(v) => {
          setMetric(v)
          store('c460-trend-metric', v)
        }}
        options={[
          { value: 'clients', label: 'Clients' },
          { value: 'traffic', label: 'Traffic' },
          { value: 'utilization', label: 'Channel use' },
          { value: 'temperature', label: 'Temperature' },
        ]}
        className="mb-3 w-full"
      />
      {error ? (
        <p role="alert" className="text-[12px] text-danger">
          {error}
        </p>
      ) : !points ? (
        <p className="text-[12px] text-muted">Loading history…</p>
      ) : (
        <LineChart times={chart.times} series={chart.series} format={chart.format} min={chart.min} max={chart.max} height={140} />
      )}
    </Panel>
  )
}
