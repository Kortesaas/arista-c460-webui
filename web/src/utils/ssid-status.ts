import type { ApState, Band, Ssid } from '@/types'

/** An immediate preview, retained until a fresh AP sample confirms the save. */
export interface SsidApplication {
  previousName: string
  ssid: Ssid
  sampleAt: string | undefined
  pending: boolean
}

export function appliedSsid(state: ApState, application: SsidApplication) {
  if (state.generatedAt === application.sampleAt) return undefined
  const expected = application.ssid
  return state.ssids.find((ssid) =>
    ssid.name === expected.name && ssid.enabled === expected.enabled &&
    ssid.hidden === expected.hidden && ssid.opmode === expected.opmode &&
    ssid.vlan === expected.vlan && ssid.isolation === expected.isolation &&
    ssid.bands.length === expected.bands.length && expected.bands.every((band) => ssid.bands.includes(band)),
  )
}

export function ssidBandStatus(ssid: Ssid, band: Band, state: ApState) {
  if (!ssid.enabled) return 'disabled'
  if (state.radios.find((radio) => radio.band === band)?.enabled === false) return 'radio off'
  const schedule = state.schedules?.[ssid.name]
  if (schedule?.enabled && !schedule.active && !schedule.waiting) return 'scheduled off'
  return ssid.bssids.some((bssid) => bssid.band === band) ? 'broadcasting' : 'starting'
}
