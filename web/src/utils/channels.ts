import { realRssi } from '@/components/Charts'
import type { Band, Neighbor, Radio } from '@/types'
import { isDfs } from '@/utils/format'

export interface ChannelAdvice {
  channel: number
  score: number
  networks: number
  currentScore: number
  currentNetworks: number
  /** True when the suggested channel is clearly better than the current one. */
  better: boolean
}

// A loud neighbour hurts more than a faint one.
const weight = (rssi: number | null) => (rssi === null ? 0.5 : rssi >= -60 ? 3 : rssi >= -70 ? 2 : rssi >= -80 ? 1 : 0.4)

/** Channel numbers within this distance share spectrum with a channel of the given width. */
function overlaps(band: Band, a: number, b: number, width: number) {
  if (band === '2.4') return Math.abs(a - b) <= 4
  return Math.abs(a - b) < (4 * Math.max(width, 20)) / 20
}

/**
 * Suggests the quietest channel for a radio from the RF scan. On 2.4 GHz only
 * the non-overlapping 1/6/11 are considered; on 5 GHz radar (DFS) channels get
 * a small penalty because the radio must leave them when radar appears.
 */
export function recommendChannel(radio: Radio, neighbors: Neighbor[], width = radio.width): ChannelAdvice | null {
  const heard = neighbors.filter((n) => n.band === radio.band)
  let candidates = radio.allowedChannels.length ? radio.allowedChannels : [radio.channel]
  if (radio.band === '2.4') {
    const preferred = candidates.filter((c) => c === 1 || c === 6 || c === 11)
    if (preferred.length) candidates = preferred
  }
  const score = (channel: number) => {
    let s = 0
    let n = 0
    for (const nb of heard) {
      if (overlaps(radio.band, nb.primaryChannel || nb.channel, channel, width)) {
        s += weight(realRssi(nb.rssi))
        n++
      }
    }
    if (isDfs(radio.band, channel)) s += 0.75
    return { s, n }
  }
  if (!candidates.length) return null
  const ranked = candidates.map((channel) => ({ channel, ...score(channel) })).sort((a, b) => a.s - b.s || a.channel - b.channel)
  const best = ranked[0]!
  const current = score(radio.channel)
  return {
    channel: best.channel,
    score: best.s,
    networks: best.n,
    currentScore: current.s,
    currentNetworks: current.n,
    // Clearly better only: scans fluctuate, and advice that flips between
    // refreshes is worse than none.
    better: best.channel !== radio.channel && current.s - best.s >= 2 && best.s <= current.s * 0.75,
  }
}
