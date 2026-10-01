import {
  cloneElement,
  isValidElement,
  useEffect,
  useId,
  useRef,
  type ButtonHTMLAttributes,
  type InputHTMLAttributes,
  type ReactNode,
  type SelectHTMLAttributes,
  type TextareaHTMLAttributes,
} from 'react'
import { X } from 'lucide-react'
import { cn } from '@/ui/cn'

/* ------------------------------------------------------------------ button */

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: 'default' | 'primary' | 'ghost' | 'danger'
  size?: 'sm' | 'md'
}

const buttonStyles = {
  default: 'bg-surface border-line text-ink hover:bg-surface-2 hover:border-line-strong',
  primary: 'bg-accent border-accent text-white hover:bg-accent-hover hover:border-accent-hover',
  ghost: 'bg-transparent border-transparent text-muted hover:bg-surface-2 hover:text-ink',
  danger: 'bg-transparent border-line text-danger hover:bg-danger-soft hover:border-danger',
}

export function Button({ variant = 'default', size = 'md', className, type = 'button', ...props }: ButtonProps) {
  return (
    <button
      type={type}
      {...props}
      className={cn(
        'inline-flex shrink-0 items-center justify-center gap-1.5 rounded border font-medium transition-colors disabled:pointer-events-none disabled:opacity-40',
        size === 'sm' ? 'h-7 px-2 text-xs' : 'h-8 px-2.5 text-[13px]',
        buttonStyles[variant],
        className,
      )}
    />
  )
}

const iconButtonSizes = { sm: 'h-6 w-6', md: 'h-7 w-7', lg: 'h-8 w-8' }

export function IconButton({
  className,
  label,
  size = 'lg',
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & { label: string; size?: keyof typeof iconButtonSizes }) {
  return (
    <button
      type="button"
      {...props}
      aria-label={label}
      title={label}
      className={cn(
        'grid shrink-0 place-items-center rounded text-muted transition-colors hover:bg-surface-2 hover:text-ink disabled:pointer-events-none disabled:opacity-30',
        iconButtonSizes[size],
        className,
      )}
    />
  )
}

/* ------------------------------------------------------------- form fields */

const fieldBase =
  'w-full rounded border border-line bg-surface px-2 text-[13px] text-ink placeholder:text-faint transition-colors hover:border-line-strong focus:border-accent focus:outline-none disabled:opacity-50'

export function Input({ className, ...props }: InputHTMLAttributes<HTMLInputElement>) {
  return <input {...props} className={cn(fieldBase, 'h-8', className)} />
}

export function Select({ className, ...props }: SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select
      {...props}
      className={cn(fieldBase, 'h-8 cursor-pointer appearance-none pr-6', className)}
      style={{
        backgroundImage:
          "url(\"data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 16 16' fill='none' stroke='%238a94a8' stroke-width='1.6'%3E%3Cpath d='M4 6.5 8 10.5 12 6.5'/%3E%3C/svg%3E\")",
        backgroundRepeat: 'no-repeat',
        backgroundPosition: 'right 4px center',
        backgroundSize: '14px',
      }}
    />
  )
}

export function Textarea({ className, ...props }: TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return <textarea rows={3} {...props} className={cn(fieldBase, 'py-1.5', className)} />
}

export function Field({ label, hint, children, className }: { label: string; hint?: string; children: ReactNode; className?: string }) {
  const id = useId()
  const control = isValidElement<{ id?: string }>(children) && !children.props.id ? cloneElement(children, { id }) : children
  return (
    <div className={cn('min-w-0', className)}>
      <label htmlFor={id} className="mb-1 block text-2xs font-semibold uppercase tracking-wider text-faint">
        {label}
      </label>
      {control}
      {hint && <p className="mt-1 text-[11px] leading-4 text-faint">{hint}</p>}
    </div>
  )
}

export function Toggle({ checked, onChange, label, hint }: { checked: boolean; onChange: (value: boolean) => void; label: string; hint?: string }) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      onClick={() => onChange(!checked)}
      className="flex w-full items-center justify-between gap-3 rounded px-1 py-1 text-left text-[13px] text-ink hover:bg-surface-2"
    >
      <span className="min-w-0">
        <span className="block truncate">{label}</span>
        {hint && <span className="block text-[11px] leading-4 text-faint">{hint}</span>}
      </span>
      <span className={cn('relative h-[18px] w-8 shrink-0 rounded-full transition-colors', checked ? 'bg-accent' : 'bg-surface-3')}>
        <span className={cn('absolute top-[2px] h-[14px] w-[14px] rounded-full bg-white transition-all', checked ? 'left-[16px]' : 'left-[2px]')} />
      </span>
    </button>
  )
}

export function Segmented<T extends string | number>({
  value,
  options,
  onChange,
  className,
}: {
  value: T
  options: { value: T; label: ReactNode }[]
  onChange: (value: T) => void
  className?: string
}) {
  return (
    <div className={cn('inline-flex shrink-0 items-center gap-0.5 rounded border border-line bg-surface-2 p-0.5', className)}>
      {options.map((option) => (
        <button
          key={String(option.value)}
          type="button"
          onClick={() => onChange(option.value)}
          aria-pressed={value === option.value}
          className={cn(
            'h-6 flex-1 basis-0 whitespace-nowrap rounded-sm px-2 text-xs font-medium transition-colors',
            value === option.value ? 'bg-surface text-ink shadow-card' : 'text-muted hover:text-ink',
          )}
        >
          {option.label}
        </button>
      ))}
    </div>
  )
}

/* ----------------------------------------------------------------- surface */

export function Panel({
  title,
  actions,
  children,
  className,
  bodyClassName,
}: {
  title?: ReactNode
  actions?: ReactNode
  children: ReactNode
  className?: string
  bodyClassName?: string
}) {
  return (
    <section className={cn('flex min-w-0 flex-col overflow-hidden rounded-lg border border-line bg-surface', className)}>
      {(title || actions) && (
        <header className="flex h-10 shrink-0 items-center justify-between gap-3 border-b border-line px-3">
          <h2 className="truncate text-[13px] font-semibold text-ink">{title}</h2>
          {actions && <div className="flex shrink-0 items-center gap-1.5">{actions}</div>}
        </header>
      )}
      <div className={cn('min-h-0 flex-1', bodyClassName ?? 'p-3')}>{children}</div>
    </section>
  )
}

export function SectionLabel({ children, className }: { children: ReactNode; className?: string }) {
  return <p className={cn('mb-2 text-2xs font-semibold uppercase tracking-wider text-faint', className)}>{children}</p>
}

export type Tone = 'neutral' | 'accent' | 'ok' | 'warn' | 'danger'

const badgeTones: Record<Tone, string> = {
  neutral: 'bg-surface-2 text-muted border-line',
  accent: 'bg-accent-soft text-accent-text border-transparent',
  ok: 'bg-ok-soft text-ok border-transparent',
  warn: 'bg-warn-soft text-warn border-transparent',
  danger: 'bg-danger-soft text-danger border-transparent',
}

export function Badge({ children, tone = 'neutral', className, title }: { children: ReactNode; tone?: Tone; className?: string; title?: string }) {
  return (
    <span title={title} className={cn('inline-flex items-center gap-1 whitespace-nowrap rounded-sm border px-1.5 py-0.5 text-2xs font-semibold', badgeTones[tone], className)}>
      {children}
    </span>
  )
}

export function EmptyState({ icon, title, description, action }: { icon?: ReactNode; title: string; description: string; action?: ReactNode }) {
  return (
    <div className="flex flex-col items-center justify-center px-6 py-12 text-center">
      {icon && <div className="mb-3 text-faint">{icon}</div>}
      <p className="text-sm font-semibold text-ink">{title}</p>
      <p className="mt-1 max-w-sm text-[13px] leading-5 text-muted">{description}</p>
      {action && <div className="mt-4">{action}</div>}
    </div>
  )
}

/** Key/value rows used in inspectors and detail pages. */
export function KeyValue({ items, className }: { items: { label: string; value: ReactNode; mono?: boolean }[]; className?: string }) {
  return (
    <dl className={cn('grid grid-cols-[minmax(80px,auto)_1fr] gap-x-3 gap-y-1 text-[12px]', className)}>
      {items.map((item) => (
        <div key={item.label} className="contents">
          <dt className="truncate text-faint">{item.label}</dt>
          <dd className={cn('min-w-0 break-words text-ink', item.mono && 'mono')}>{item.value ?? '—'}</dd>
        </div>
      ))}
    </dl>
  )
}

/* ------------------------------------------------------------------ dialog */

export function Dialog({
  title,
  description,
  onClose,
  children,
  wide,
}: {
  title: string
  description?: string
  onClose: () => void
  children: ReactNode
  wide?: boolean
}) {
  const bodyRef = useRef<HTMLDivElement>(null)
  const closeRef = useRef(onClose)
  useEffect(() => {
    closeRef.current = onClose
  }, [onClose])
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') closeRef.current()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])
  useEffect(() => {
    bodyRef.current?.querySelector<HTMLElement>('input, select, textarea')?.focus()
  }, [])
  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-black/50 p-4 pt-[8vh] backdrop-blur-sm"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) onClose()
      }}
    >
      <div role="dialog" aria-modal="true" aria-label={title} className={cn('w-full overflow-hidden rounded-lg border border-line bg-surface shadow-pop', wide ? 'max-w-2xl' : 'max-w-md')}>
        <header className="flex items-start justify-between gap-4 border-b border-line px-4 py-3">
          <div className="min-w-0">
            <h2 className="text-sm font-semibold text-ink">{title}</h2>
            {description && <p className="mt-0.5 text-[12px] leading-4 text-muted">{description}</p>}
          </div>
          <IconButton label="Close" onClick={onClose}>
            <X size={16} />
          </IconButton>
        </header>
        <div ref={bodyRef} className="p-4">
          {children}
        </div>
      </div>
    </div>
  )
}

export function DialogActions({ children }: { children: ReactNode }) {
  return <div className="mt-5 flex items-center justify-end gap-2 border-t border-line pt-4">{children}</div>
}

/** Tiny inline spinner for buttons and status rows. */
export function Spinner({ size = 12, className }: { size?: number; className?: string }) {
  return (
    <svg className={cn('spin', className)} width={size} height={size} viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <circle cx="12" cy="12" r="9" stroke="currentColor" strokeOpacity="0.25" strokeWidth="3" />
      <path d="M21 12a9 9 0 0 0-9-9" stroke="currentColor" strokeWidth="3" strokeLinecap="round" />
    </svg>
  )
}
