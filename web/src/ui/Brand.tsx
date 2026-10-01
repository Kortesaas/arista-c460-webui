import logoUrl from '@/assets/brand-logo.png'
import hatUrl from '@/assets/brand-hat.png'
import { cn } from '@/ui/cn'

const art = {
  full: { url: logoUrl, ratio: 1000 / 315, label: 'ARRR-ISTA C460' },
  hat: { url: hatUrl, ratio: 256 / 155, label: 'ARRR-ISTA' },
}

/**
 * The parody brand mark, drawn as a CSS mask so it takes the current text
 * colour: navy on light surfaces, white on the navy top bar or in dark mode.
 */
export function Brand({ variant = 'full', className, height }: { variant?: keyof typeof art; className?: string; height: number }) {
  const { url, ratio, label } = art[variant]
  const mask = `url(${url}) center / contain no-repeat`
  return (
    <span
      role="img"
      aria-label={label}
      className={cn('inline-block shrink-0 bg-current', className)}
      style={{ height, width: Math.round(height * ratio), mask, WebkitMask: mask }}
    />
  )
}
