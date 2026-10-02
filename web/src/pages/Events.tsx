import { Page } from '@/app/Page'
import { WirelessEventsPanel } from '@/components/NetworkDiagnostics'

export function EventsPage() {
  return (
    <Page title="Events" description="Recent wireless activity: clients joining and leaving, channel changes and radar detection.">
      <WirelessEventsPanel />
    </Page>
  )
}
