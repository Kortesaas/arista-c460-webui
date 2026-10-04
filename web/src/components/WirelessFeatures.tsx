import { useEffect, useState } from 'react'
import { Save } from 'lucide-react'
import { api } from '@/api'
import { useApp } from '@/stores/app'
import type { FeatureSettings, SsidFeatures } from '@/types'
import { Button, Dialog, DialogActions, Spinner } from '@/ui/kit'
import { changedSettings, FeatureEditor, type FeatureGroup } from '@/components/FeatureEditor'

const groups: FeatureGroup[] = [
  {
    title: 'Roaming between access points',
    items: [
      {
        key: 'fastRoaming',
        label: 'Fast roaming (802.11r)',
        help: 'Phones move between APs without a full new login, which keeps calls and streams running. Devices without 802.11r still connect. The roaming domain is derived from the network name, so it matches on every AP running this interface.',
      },
      { key: 'bssTransition', label: 'Roaming hints (802.11v)', help: 'The AP can suggest a better AP or band to a client that supports it.' },
      { key: 'rrm', label: 'Neighbour reports (802.11k)', help: 'Clients can ask which other APs are nearby, so they roam to a good one faster.' },
      { key: 'okc', label: 'Key caching (OKC)', help: 'Opportunistic key caching speeds up reconnecting for clients that support it.' },
    ],
  },
  {
    title: 'Band and load',
    items: [
      { key: 'bandSteering', label: 'Band steering', help: 'Encourages dual-band clients to use 5 or 6 GHz instead of the crowded 2.4 GHz band.' },
      { key: 'load', label: 'Advertise load', help: 'Announces client count and channel use, so clients can pick a less busy AP.' },
      { key: 'advertiseName', label: 'Advertise AP name', help: 'Includes this AP’s name in its beacons, so Wi-Fi scanner apps show which AP is which.' },
    ],
  },
  {
    title: 'Traffic',
    items: [
      { key: 'multicastFilter', label: 'Multicast optimisation', help: 'Reduces multicast traffic on the air, which saves airtime with many clients.' },
      {
        key: 'broadcastFilter',
        label: 'Broadcast filtering (proxy ARP)',
        help: 'The AP answers ARP requests for its clients instead of broadcasting them. Saves airtime; rarely, discovery protocols on isolated networks rely on broadcasts.',
      },
    ],
  },
]

export function WirelessFeaturesDialog({ name, onClose }: { name: string; onClose: () => void }) {
  const [data, setData] = useState<SsidFeatures | null>(null)
  const [values, setValues] = useState<FeatureSettings>({})
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const change = useApp((s) => s.change)

  useEffect(() => {
    api
      .ssidFeatures(name)
      .then((d) => {
        setData(d)
        setValues(d.settings ?? {})
      })
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [name])

  const diff = data ? changedSettings(data.settings ?? {}, values) : {}
  const save = async () => {
    setBusy(true)
    const ok = await change(`Saved advanced settings for ${name}`, () => api.updateSsidFeatures(name, diff))
    setBusy(false)
    if (ok) onClose()
  }

  return (
    <Dialog wide title={`Advanced · ${name}`} description="Roaming, band steering and airtime settings. “Default” keeps the firmware’s own choice; “now” shows what is running." onClose={() => !busy && onClose()}>
      {error ? (
        <p role="alert" className="text-[12px] text-danger">
          {error}
        </p>
      ) : !data ? (
        <p className="flex items-center gap-2 py-4 text-[12px] text-muted">
          <Spinner size={13} /> Reading settings…
        </p>
      ) : (
        <FeatureEditor groups={groups} values={values} native={data.native ?? {}} disabled={busy} onChange={(k, v) => setValues((s) => ({ ...s, [k]: v }))} />
      )}
      <DialogActions>
        <Button disabled={busy} onClick={onClose}>
          Cancel
        </Button>
        <Button variant="primary" disabled={busy || !data || Object.keys(diff).length === 0} onClick={() => void save()}>
          {busy ? <Spinner size={12} /> : <Save size={13} />} Save
        </Button>
      </DialogActions>
    </Dialog>
  )
}
