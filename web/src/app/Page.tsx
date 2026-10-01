import type { ReactNode } from 'react'
import { cn } from '@/ui/cn'

/** Standard scrolling page: heading, description, actions, then content. */
export function Page({ title, description, actions, children, width = 'wide', dense }: { title: ReactNode; description?: ReactNode; actions?: ReactNode; children: ReactNode; width?: 'wide' | 'narrow' | 'full'; dense?: boolean }) {
  return (
    <div className={cn('p-3 sm:p-4', dense ? 'lg:p-4' : 'lg:p-6')}>
      <div className={cn('mx-auto', width === 'wide' ? 'max-w-[1500px]' : width === 'narrow' ? 'max-w-3xl' : '')}>
        <header className={cn('flex flex-col justify-between gap-3 sm:flex-row sm:items-start', dense ? 'mb-3' : 'mb-4 lg:mb-5')}>
          <div className="min-w-0">
            <h2 className="text-xl font-semibold tracking-tight text-ink">{title}</h2>
            {description && <div className="mt-1 max-w-2xl text-[13px] leading-5 text-muted">{description}</div>}
          </div>
          {actions && <div className="flex shrink-0 flex-wrap items-center gap-2">{actions}</div>}
        </header>
        {children}
      </div>
    </div>
  )
}

/** Full-height workspace: toolbar on top, optional inspector on the right. */
export function Workspace({ toolbar, right, children }: { toolbar?: ReactNode; right?: ReactNode; children: ReactNode }) {
  return (
    <div className="flex h-full min-h-0 flex-col">
      {toolbar && <div className="flex min-h-11 shrink-0 flex-wrap items-center gap-2 border-b border-line bg-surface px-3 py-1.5">{toolbar}</div>}
      <div className="flex min-h-0 flex-1 overflow-hidden">
        <div className="min-h-0 min-w-0 flex-1 overflow-auto">{children}</div>
        {right && <aside className="hidden w-[320px] shrink-0 overflow-y-auto border-l border-line bg-surface lg:block">{right}</aside>}
      </div>
    </div>
  )
}
