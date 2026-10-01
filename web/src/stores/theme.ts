import { create } from 'zustand'

export type Theme = 'light' | 'dark' | 'system'
const KEY = 'c460-theme'

const stored = (): Theme => {
  try {
    return (localStorage.getItem(KEY) as Theme | null) ?? 'light'
  } catch {
    return 'light'
  }
}

export const useThemeStore = create<{ theme: Theme; setTheme: (theme: Theme) => void }>((set) => ({
  theme: stored(),
  setTheme: (theme) => {
    try {
      localStorage.setItem(KEY, theme)
    } catch {
      /* private mode */
    }
    set({ theme })
    applyTheme(theme)
  },
}))

export function applyTheme(theme: Theme) {
  const dark = theme === 'dark' || (theme === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches)
  document.documentElement.classList.toggle('dark', dark)
}
