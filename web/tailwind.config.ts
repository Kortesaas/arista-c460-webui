import type { Config } from 'tailwindcss'

export default {
  darkMode: 'class',
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        bg: 'var(--bg)',
        surface: 'var(--surface)',
        'surface-2': 'var(--surface-2)',
        'surface-3': 'var(--surface-3)',
        line: 'var(--border)',
        'line-strong': 'var(--border-strong)',
        ink: 'var(--text)',
        muted: 'var(--text-muted)',
        faint: 'var(--text-faint)',
        accent: 'var(--accent)',
        'accent-hover': 'var(--accent-hover)',
        'accent-soft': 'var(--accent-soft)',
        'accent-text': 'var(--accent-text)',
        ok: 'var(--ok)',
        'ok-soft': 'var(--ok-soft)',
        warn: 'var(--warn)',
        'warn-soft': 'var(--warn-soft)',
        danger: 'var(--danger)',
        'danger-soft': 'var(--danger-soft)',
        canvas: 'var(--canvas)',
        'canvas-grid': 'var(--canvas-grid)',
      },
      borderRadius: { xs: '3px', sm: '4px', DEFAULT: '6px', md: '6px', lg: '8px', xl: '12px' },
      boxShadow: { card: 'var(--shadow)', pop: 'var(--shadow-lg)' },
      fontSize: { '2xs': ['10px', { lineHeight: '14px', letterSpacing: '0.04em' }] },
    },
  },
  plugins: [],
} satisfies Config
