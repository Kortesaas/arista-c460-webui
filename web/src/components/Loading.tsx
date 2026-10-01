import { WifiOff } from 'lucide-react'
import { useApp } from '@/stores/app'
import { Button, EmptyState, Spinner } from '@/ui/kit'

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
  return (
    <div className="flex h-full min-h-[40vh] flex-col items-center justify-center gap-3 text-muted">
      <Spinner size={20} className="text-accent" />
      <p className="text-[13px]">Reading the access point…</p>
    </div>
  )
}
