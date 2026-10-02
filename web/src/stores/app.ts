import { create } from 'zustand'
import { api, ApiError } from '@/api'
import type { ApState } from '@/types'

export type Connection = 'connecting' | 'live' | 'reconnecting' | 'offline'
export type Auth = 'unknown' | 'signed-out' | 'signed-in'

interface Toast {
  id: number
  tone: 'ok' | 'danger' | 'accent'
  text: string
}

interface AppStore {
  auth: Auth
  configured: boolean
  username: string
  state: ApState | null
  connection: Connection
  error: string | null
  now: number
  toasts: Toast[]
  init: () => () => void
  refresh: () => Promise<void>
  signIn: (username: string, password: string) => Promise<void>
  signOut: () => Promise<void>
  toast: (text: string, tone?: Toast['tone']) => void
  /** Runs a configuration change, reports the outcome and refreshes state. */
  change: (label: string, run: () => Promise<unknown>) => Promise<boolean>
}

let toastId = 0

export const useApp = create<AppStore>((set, get) => ({
  auth: 'unknown',
  configured: true,
  username: '',
  state: null,
  connection: 'connecting',
  error: null,
  now: Date.now(),
  toasts: [],

  init: () => {
    let stopped = false
    let timer: ReturnType<typeof setTimeout> | undefined
    const clock = setInterval(() => set({ now: Date.now() }), 1000)
    const loop = async () => {
      if (stopped) return
      if (get().auth === 'signed-in') await get().refresh()
      const seconds = get().state?.pollSeconds ?? 5
      timer = setTimeout(loop, Math.max(2, seconds) * 1000)
    }
    void api
      .session()
      .then((session) => {
        set({ auth: session.authenticated ? 'signed-in' : 'signed-out', configured: session.configured, username: session.username })
        void loop()
      })
      .catch((error: unknown) => set({ auth: 'signed-out', connection: 'offline', error: String(error) }))
    return () => {
      stopped = true
      clearInterval(clock)
      if (timer) clearTimeout(timer)
    }
  },

  refresh: async () => {
    try {
      const state = await api.state()
      // During boot, the poller returns null collections until its first successful read.
      // Keep the loading screen (or the last good snapshot) instead of rendering that response.
      if (![state.radios, state.ssids, state.clients, state.neighbors, state.interfaces].every(Array.isArray)) {
        set({ connection: get().state ? 'reconnecting' : 'connecting', error: state.error ?? null })
        return
      }
      set({ state, connection: 'live', error: state.error ?? null })
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) {
        set({ auth: 'signed-out', state: null })
        return
      }
      set({ connection: get().state ? 'reconnecting' : 'offline', error: error instanceof Error ? error.message : String(error) })
    }
  },

  signIn: async (username, password) => {
    await api.login(username, password)
    set({ auth: 'signed-in', connection: 'connecting', username })
    await get().refresh()
  },

  signOut: async () => {
    await api.logout().catch(() => undefined)
    set({ auth: 'signed-out', state: null, username: '' })
  },

  toast: (text, tone = 'ok') => {
    const id = ++toastId
    set((store) => ({ toasts: [...store.toasts, { id, tone, text }] }))
    setTimeout(() => set((store) => ({ toasts: store.toasts.filter((toast) => toast.id !== id) })), tone === 'danger' ? 9000 : 5000)
  },

  change: async (label, run) => {
    try {
      await run()
      get().toast(`${label} — applying. Wi-Fi on this AP restarts for a few seconds.`, 'ok')
      setTimeout(() => void get().refresh(), 1500)
      setTimeout(() => void get().refresh(), 6000)
      return true
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) set({ auth: 'signed-out', state: null })
      get().toast(`${label} failed: ${error instanceof Error ? error.message : String(error)}`, 'danger')
      return false
    }
  },
}))
