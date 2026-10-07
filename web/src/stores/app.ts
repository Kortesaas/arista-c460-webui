import { create } from 'zustand'
import { api, ApiError } from '@/api'
import type { ApState, Role, SsidInput } from '@/types'
import { appliedSsid, type SsidApplication } from '@/utils/ssid-status'

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
  role: Role
  /** Read-only account name; administrators only. */
  viewer: string
  state: ApState | null
  connection: Connection
  error: string | null
  now: number
  toasts: Toast[]
  ssidApplications: SsidApplication[]
  init: () => () => void
  refresh: () => Promise<void>
  signIn: (username: string, password: string) => Promise<void>
  signOut: () => Promise<void>
  toast: (text: string, tone?: Toast['tone']) => void
  setViewer: (viewer: string) => void
  /** Runs a configuration change, reports the outcome and refreshes state. */
  change: (label: string, run: () => Promise<unknown>, ssids?: { previousName?: string; input: SsidInput }[]) => Promise<boolean>
}

let toastId = 0
let stateRequest: Promise<void> | null = null
let sessionGeneration = 0
const pageVisible = () => document.visibilityState !== 'hidden'

export const useApp = create<AppStore>((set, get) => ({
  auth: 'unknown',
  configured: true,
  username: '',
  role: '',
  viewer: '',
  state: null,
  connection: 'connecting',
  error: null,
  now: Date.now(),
  toasts: [],
  ssidApplications: [],

  init: () => {
    let stopped = false
    let timer: ReturnType<typeof setTimeout> | undefined
    let running = false
    let failures = 0
    const clock = setInterval(() => set({ now: Date.now() }), 1000)
    const loop = async () => {
      if (stopped || running || !pageVisible()) return
      running = true
      try {
        if (get().auth === 'signed-in') await get().refresh()
        failures = get().connection === 'live' ? 0 : Math.min(5, failures + 1)
      } finally {
        running = false
        if (!stopped && pageVisible()) {
          if (timer) clearTimeout(timer)
          timer = setTimeout(
            loop,
            get().auth === 'signed-in' ? (failures ? Math.max(get().state?.pollSeconds ?? 5, Math.min(30, 2 ** failures)) : (get().state?.pollSeconds ?? 5)) * 1000 : 5000,
          )
        }
      }
    }
    const visible = () => {
      if (timer) clearTimeout(timer)
      if (pageVisible()) void loop()
    }
    document.addEventListener('visibilitychange', visible)
    void api
      .session()
      .then((session) => {
        if (stopped) return
        set({ auth: session.authenticated ? 'signed-in' : 'signed-out', configured: session.configured, username: session.username, role: session.role, viewer: session.viewer ?? '' })
        void loop()
      })
      .catch((error: unknown) => {
        if (!stopped) set({ auth: 'signed-out', connection: 'offline', error: String(error) })
      })
    return () => {
      stopped = true
      clearInterval(clock)
      if (timer) clearTimeout(timer)
      document.removeEventListener('visibilitychange', visible)
    }
  },

  refresh: async () => {
    if (stateRequest) return stateRequest
    const generation = sessionGeneration
    const request = (async () => {
      try {
        const state = await api.state()
        if (generation !== sessionGeneration || get().auth !== 'signed-in') return
        if (![state.radios, state.ssids, state.clients, state.neighbors, state.interfaces].every(Array.isArray)) {
          set({ connection: get().state ? 'reconnecting' : 'connecting', error: state.error ?? null })
          return
        }
        const previous = get().state
        const same =
          previous?.generatedAt === state.generatedAt &&
          previous?.error === state.error &&
          previous?.pollSeconds === state.pollSeconds &&
          previous?.hardware?.updatedAt === state.hardware?.updatedAt &&
          previous?.managementError === state.managementError &&
          JSON.stringify(previous?.management) === JSON.stringify(state.management) &&
          JSON.stringify(previous?.radios.map((r) => r.wifi7)) === JSON.stringify(state.radios.map((r) => r.wifi7))
        const applications = get().ssidApplications
        const remaining = applications.filter((application) => application.pending || !appliedSsid(state, application))
        set({
          state: same ? previous : state, connection: state.error ? 'reconnecting' : 'live', error: state.error ?? null,
          ssidApplications: remaining.length === applications.length ? applications : remaining,
        })
      } catch (error) {
        if (generation !== sessionGeneration) return
        if (error instanceof ApiError && error.status === 401) {
          set({ auth: 'signed-out', state: null, ssidApplications: [] })
          return
        }
        set({ connection: get().state ? 'reconnecting' : 'offline', error: error instanceof Error ? error.message : String(error) })
      }
    })()
    stateRequest = request
    try {
      await request
    } finally {
      if (stateRequest === request) stateRequest = null
    }
  },

  signIn: async (username, password) => {
    const { role } = await api.login(username, password)
    sessionGeneration++
    stateRequest = null
    set({ auth: 'signed-in', connection: 'connecting', username, role })
    void api.session().then((session) => set({ viewer: session.viewer ?? '' })).catch(() => undefined)
    await get().refresh()
  },

  signOut: async () => {
    sessionGeneration++
    stateRequest = null
    set({ auth: 'signed-out', state: null, username: '', role: '', viewer: '', ssidApplications: [] })
    await api.logout().catch(() => undefined)
  },

  setViewer: (viewer) => set({ viewer }),

  toast: (text, tone = 'ok') => {
    const id = ++toastId
    set((store) => ({ toasts: [...store.toasts, { id, tone, text }] }))
    setTimeout(() => set((store) => ({ toasts: store.toasts.filter((toast) => toast.id !== id) })), tone === 'danger' ? 9000 : 5000)
  },

  change: async (label, run, ssids = []) => {
    const applications: SsidApplication[] = ssids.map(({ previousName, input }) => ({
      previousName: previousName ?? input.name,
      sampleAt: get().state?.generatedAt,
      pending: true,
      ssid: {
        name: input.name, enabled: input.enabled, hidden: input.hidden, opmode: input.opmode,
        bands: input.bands, vlan: input.vlan, isolation: input.isolation,
        hasPassword: Boolean(input.password) || Boolean(get().state?.ssids.find((ssid) => ssid.name === previousName)?.hasPassword),
        mfp: false, bssids: [], clients: 0, rxBytes: 0, txBytes: 0,
      },
    }))
    const tracked = new Set(applications.map((application) => application.ssid))
    if (applications.length) set((store) => ({ ssidApplications: [...store.ssidApplications, ...applications] }))
    try {
      await run()
      if (applications.length) {
        set((store) => ({ ssidApplications: store.ssidApplications.map((application) => tracked.has(application.ssid) ? { ...application, pending: false } : application) }))
        void get().refresh()
      }
      get().toast(`${label} — applying. Wi-Fi on this AP restarts for a few seconds.`, 'ok')
      setTimeout(() => void get().refresh(), 1500)
      setTimeout(() => void get().refresh(), 6000)
      return true
    } catch (error) {
      set((store) => ({ ssidApplications: store.ssidApplications.filter((application) => !tracked.has(application.ssid)) }))
      if (error instanceof ApiError && error.status === 401) set({ auth: 'signed-out', state: null, ssidApplications: [] })
      get().toast(`${label} failed: ${error instanceof Error ? error.message : String(error)}`, 'danger')
      return false
    }
  },
}))
