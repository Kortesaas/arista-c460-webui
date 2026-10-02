import { WifiOff } from 'lucide-react'
import { useApp } from '@/stores/app'
import { Brand } from '@/ui/Brand'
import { Button, EmptyState } from '@/ui/kit'

export function PirateLoading() {
  return (
    <div className="pirate-loading" role="status" aria-label="Loading the access point" aria-live="polite">
      <span aria-hidden="true">
        <Brand variant="hat" height={40} className="pirate-loading-hat" />
      </span>
    </div>
  )
}

/** Shown until the first state arrives, or when the AP service cannot be reached. */
export function LoadingState() {
  const { connection, error, refresh } = useApp()
  if (connection === 'offline')
    return (
      <div className="p-6">
        <EmptyState
          icon={<WifiOff size={28} />}
          title="The access point service is not reachable"
          description={error ?? 'Check that you are connected to the management network.'}
          action={<Button onClick={() => void refresh()}>Try again</Button>}
        />
      </div>
    )
  return <PirateLoading />
}
