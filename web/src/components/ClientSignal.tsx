import { useEffect, useMemo, useState } from 'react'
import { api } from '@/api'
import { LineChart } from '@/components/LineChart'
import { bandColor } from '@/components/status'
import type { Band, ClientSample } from '@/types'

/** Signal over the last two hours, one line colour per band so roaming is visible. */
export function ClientSignal({ mac, sampledAt }: { mac: string; sampledAt?: string }) {
  const [samples, setSamples] = useState<ClientSample[] | null>(null)
  const minute = sampledAt ? Math.floor(Date.parse(sampledAt) / 60000) : 0
  useEffect(() => {
    let active = true
    api
      .clientHistory(mac)
      .then((r) => active && setSamples(r.samples))
      .catch(() => active && setSamples([]))
    return () => {
      active = false
    }
  }, [mac, minute])

  const { times, series, roams } = useMemo(() => {
    const list = samples ?? []
    const bands = [...new Set(list.map((s) => s.band).filter((b): b is Band => b !== ''))]
    const times = list.map((s) => s.t)
    const series = bands.map((b) => ({
      label: `${b} GHz`,
      color: bandColor(b),
      // A real reading of 0 dBm does not exist; it is a placeholder.
      values: list.map((s, i) => (s.band === b || (list[i + 1]?.band === b && s.band !== b) ? (s.rssi !== null && s.rssi < 0 ? s.rssi : null) : null)),
    }))
    const roams: { t: number; text: string }[] = []
    list.forEach((s, i) => {
      const prev = list[i - 1]
      if (prev && (prev.band !== s.band || prev.ssid !== s.ssid))
        roams.push({ t: s.t, text: prev.ssid !== s.ssid ? `moved from “${prev.ssid}” to “${s.ssid}”` : `moved from ${prev.band} GHz to ${s.band} GHz` })
    })
    return { times, series, roams: roams.slice(-5).reverse() }
  }, [samples])

  if (!samples) return <p className="text-[12px] text-muted">Loading signal history…</p>
  return (
    <div>
      <LineChart
        times={times}
        series={series}
        height={96}
        min={-90}
        max={-30}
        format={(v) => `${Math.round(v)} dBm`}
        empty="Signal history starts when the client has been connected for a minute."
      />
      {roams.length > 0 && (
        <ul className="mt-2 space-y-0.5 text-[12px] text-muted">
          {roams.map((r) => (
            <li key={r.t}>
              <span className="tabular text-faint">{new Date(r.t * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}</span> {r.text}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
