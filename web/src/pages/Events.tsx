import { useSearchParams } from 'react-router-dom'
import { Page } from '@/app/Page'
import { ChangeLogPanel } from '@/components/ChangeLog'
import { WirelessEventsPanel } from '@/components/NetworkDiagnostics'
import { Segmented } from '@/ui/kit'

type Tab = 'wireless' | 'changes'

export function EventsPage() {
  const [params, setParams] = useSearchParams()
  const tab: Tab = params.get('tab') === 'changes' ? 'changes' : 'wireless'
  return (
    <Page
      title="Events"
      description={tab === 'wireless' ? 'Recent wireless activity: clients joining and leaving, channel changes and radar detection.' : 'Who changed which setting, and when.'}
      actions={
        <Segmented
          value={tab}
          onChange={(v) => setParams(v === 'changes' ? { tab: 'changes' } : {}, { replace: true })}
          options={[
            { value: 'wireless', label: 'Wi-Fi events' },
            { value: 'changes', label: 'Configuration changes' },
          ]}
        />
      }
    >
      {tab === 'wireless' ? <WirelessEventsPanel /> : <ChangeLogPanel />}
    </Page>
  )
}
