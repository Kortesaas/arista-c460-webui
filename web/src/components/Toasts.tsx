import { CheckCircle2, Info, XCircle } from 'lucide-react'
import { useApp } from '@/stores/app'
import { cn } from '@/ui/cn'

export function Toasts() {
  const toasts = useApp((store) => store.toasts)
  return (
    <div className="pointer-events-none fixed bottom-4 right-4 z-[60] flex w-[min(380px,calc(100vw-32px))] flex-col gap-2" aria-live="polite">
      {toasts.map((toast) => {
        const Icon = toast.tone === 'ok' ? CheckCircle2 : toast.tone === 'danger' ? XCircle : Info
        return (
          <div
            key={toast.id}
            className={cn(
              'pointer-events-auto flex items-start gap-2 rounded-lg border bg-surface px-3 py-2.5 text-[12px] leading-5 text-ink shadow-pop',
              toast.tone === 'danger' ? 'border-danger/40' : 'border-line',
            )}
          >
            <Icon size={15} className={cn('mt-0.5 shrink-0', toast.tone === 'ok' ? 'text-ok' : toast.tone === 'danger' ? 'text-danger' : 'text-accent')} />
            <span className="min-w-0">{toast.text}</span>
          </div>
        )
      })}
    </div>
  )
}
