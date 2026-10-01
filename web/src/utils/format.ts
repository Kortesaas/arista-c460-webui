import type { Band, OpMode } from '@/types'

export function formatAge(iso: string | null | undefined, now = Date.now()): string {
  if (!iso) return 'never'
  const seconds = Math.max(0, Math.round((now - Date.parse(iso)) / 1000))
  if (seconds < 5) return 'just now'
  if (seconds < 60) return `${seconds}s ago`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m ago`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h ${minutes % 60}m ago`
  return `${Math.floor(hours / 24)}d ${hours % 24}h ago`
}

export function formatDuration(seconds: number | null | undefined): string {
  if (seconds === null || seconds === undefined) return '—'
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  if (d > 0) return `${d}d ${h}h`
  if (h > 0) return `${h}h ${m}m`
  return `${m}m ${Math.floor(seconds % 60)}s`
}

export function formatBytes(bytes: number | null | undefined): string {
  if (bytes === null || bytes === undefined) return '—'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let value = bytes
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit += 1
  }
  return `${value.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`
}

export const plural = (count: number, word: string, pluralWord = `${word}s`) => `${count} ${count === 1 ? word : pluralWord}`

export const bandLabel = (band: Band | '') => (band ? `${band} GHz` : '—')

export const opModeLabel: Record<string, string> = {
  WPA3_SAE: 'WPA3 Personal',
  WPA2_PERSONAL: 'WPA2 Personal',
  ENHANCED_OPEN: 'Enhanced Open (OWE)',
  OPEN: 'Open',
  WPA2_ENTERPRISE: 'WPA2 Enterprise',
  WPA3_ENTERPRISE: 'WPA3 Enterprise',
  WPA3_ENTERPRISE_192_BIT: 'WPA3 Enterprise 192-bit',
}

export const opModeShort = (mode: OpMode) => opModeLabel[mode] ?? mode.replaceAll('_', ' ')

/** 5 GHz channels 52–144 need radar detection (DFS) in the EU and US. */
export const isDfs = (band: Band, channel: number) => band === '5' && channel >= 52 && channel <= 144

export function signalQuality(rssi: number | null): { label: string; bars: number; tone: 'ok' | 'warn' | 'danger' | 'neutral' } {
  if (rssi === null) return { label: '—', bars: 0, tone: 'neutral' }
  if (rssi >= -55) return { label: 'Excellent', bars: 4, tone: 'ok' }
  if (rssi >= -67) return { label: 'Good', bars: 3, tone: 'ok' }
  if (rssi >= -75) return { label: 'Fair', bars: 2, tone: 'warn' }
  return { label: 'Weak', bars: 1, tone: 'danger' }
}
