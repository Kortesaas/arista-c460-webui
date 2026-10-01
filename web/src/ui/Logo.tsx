/** Neutral access-point mark: radio waves above a ceiling-mount body. */
export function LogoMark({ size = 16, className }: { size?: number; className?: string }) {
  return (
    <svg viewBox="0 0 24 24" width={size} height={size} fill="none" aria-hidden="true" className={className}>
      <g stroke="currentColor" strokeWidth={1.9} strokeLinecap="round">
        <path d="M5.6 10.2a9 9 0 0 1 12.8 0" />
        <path d="M8.4 13a5 5 0 0 1 7.2 0" />
      </g>
      <circle cx="12" cy="15.8" r="1.6" fill="currentColor" />
      <rect x="4.5" y="19" width="15" height="2" rx="1" fill="currentColor" />
    </svg>
  )
}
