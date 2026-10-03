import { Page } from '@/app/Page'
import { LoadingState } from '@/components/Loading'
import { ManagementPanel } from '@/components/SystemSettings'
import { LldpPanel } from '@/components/LldpSettings'
import { SnmpPanel } from '@/components/SnmpSettings'
import { MetricsPanel } from '@/components/MetricsSettings'
import { TimePanel } from '@/components/TimeSettings'
import { PortCards } from '@/components/Ports'
import { useApp } from '@/stores/app'
import { Panel } from '@/ui/kit'

/** How the AP is attached to the wired network: address, ports, switch, time. */
export function NetworkPage() {
  const state = useApp((store) => store.state)
  if (!state) return <LoadingState />
  return (
    <Page title="Network" description="Management address, Ethernet ports, switch discovery, time and monitoring.">
      <div className="grid gap-3 lg:grid-cols-2">
        <div className="space-y-3">
          <ManagementPanel state={state} />
          <TimePanel />
          <SnmpPanel />
          <MetricsPanel />
        </div>
        <div className="space-y-3">
          <Panel
            title="Ethernet ports"
            help="The AP uses one socket as uplink and keeps the other as backup. If the uplink loses its link, the AP restarts on the backup socket."
          >
            <PortCards interfaces={state.interfaces} />
          </Panel>
          <LldpPanel />
        </div>
      </div>
    </Page>
  )
}
