import { useEffect, useState } from 'react'
import { Save } from 'lucide-react'
import { api } from '@/api'
import { useApp } from '@/stores/app'
import type { FeatureSettings, Radio, RadioFeatures } from '@/types'
import { Button, Dialog, DialogActions, Spinner } from '@/ui/kit'
import { changedSettings, FeatureEditor, rangeProblems, type FeatureGroup } from '@/components/FeatureEditor'

const groups: FeatureGroup[] = [
  {
    title: 'Wi-Fi 6/7 efficiency',
    items: [
      { key: 'dlOfdma', label: 'OFDMA download', help: 'Serves several clients in one transmission. Helps most with many phones and small packets.' },
      { key: 'ulOfdma', label: 'OFDMA upload', help: 'Lets several clients send at the same time. Very old Wi-Fi 6 drivers sometimes misbehave with it.' },
      { key: 'dlMuMimo', label: 'MU-MIMO download', help: 'Sends to several clients at once using separate spatial streams.' },
      { key: 'ulMuMimo', label: 'MU-MIMO upload', help: 'Receives from several clients at once using separate spatial streams.' },
      { key: 'bssColoring', label: 'BSS colouring', help: 'Marks this AP’s frames so clients can ignore distant APs on the same channel. Useful when channels are shared.' },
      { key: 'spatialReuse', label: 'Spatial reuse', help: 'Allows transmitting while distant same-channel APs are active. Most useful in dense setups together with BSS colouring.' },
    ],
  },
  {
    title: 'Automatic power range',
    items: [
      { key: 'dtpMin', label: 'Lowest power', help: 'The lowest transmit power automatic power may choose.', range: [1, 30], unit: 'dBm' },
      { key: 'dtpMax', label: 'Highest power', help: 'The highest transmit power automatic power may choose.', range: [1, 30], unit: 'dBm' },
    ],
  },
]

export function RadioFeaturesDialog({ radio, onClose }: { radio: Radio; onClose: () => void }) {
  const [data, setData] = useState<RadioFeatures | null>(null)
  const [values, setValues] = useState<FeatureSettings>({})
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const change = useApp((s) => s.change)

  useEffect(() => {
    api
      .radioFeatures(radio.id)
      .then((d) => {
        setData(d)
        setValues(d.settings)
      })
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [radio.id])

  const diff = data ? changedSettings(data.settings, values) : {}
  const problems = rangeProblems(groups, values)
  const lo = typeof values.dtpMin === 'number' ? values.dtpMin : null
  const hi = typeof values.dtpMax === 'number' ? values.dtpMax : null
  if (lo !== null && hi !== null && lo > hi) problems.push('The lowest power must not be above the highest.')

  const save = async () => {
    setBusy(true)
    const ok = await change(`Saved advanced ${radio.band} GHz radio settings`, () => api.updateRadioFeatures(radio.id, diff))
    setBusy(false)
    if (ok) onClose()
  }

  return (
    <Dialog wide title={`Advanced · ${radio.band} GHz radio`} description="Saving restarts this radio for a few seconds. “Default” keeps the firmware’s choice; “now” shows what is running." onClose={() => !busy && onClose()}>
      {error ? (
        <p role="alert" className="text-[12px] text-danger">
          {error}
        </p>
      ) : !data ? (
        <p className="flex items-center gap-2 py-4 text-[12px] text-muted">
          <Spinner size={13} /> Reading settings…
        </p>
      ) : (
        <>
          <FeatureEditor groups={groups} values={values} native={data.native} disabled={busy} onChange={(k, v) => setValues((s) => ({ ...s, [k]: v }))} />
          <p className="mt-3 text-[11px] leading-4 text-faint">The automatic power range only matters while Automatic power is on. Gains from OFDMA and MU-MIMO depend on the clients supporting them.</p>
        </>
      )}
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
        <Button variant="primary" disabled={busy || !data || problems.length > 0 || Object.keys(diff).length === 0} onClick={() => void save()}>
          {busy ? <Spinner size={12} /> : <Save size={13} />} Save
        </Button>
      </DialogActions>
    </Dialog>
  )
}
