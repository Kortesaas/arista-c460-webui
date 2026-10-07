import { create } from 'zustand'
import { api } from '@/api'
import type { StagedChange } from '@/types'
import { useApp } from '@/stores/app'

/**
 * Staged changes: SSID and radio edits collected in the browser and applied
 * together, so Wi-Fi restarts once. Kept in memory only, because staged SSID
 * edits can contain passwords.
 */
interface StagingStore {
  changes: StagedChange[]
  busy: boolean
  stage: (change: StagedChange) => void
  remove: (index: number) => void
  discard: () => void
  applyAll: () => Promise<boolean>
}

/** The SSID name or radio a change is about; one change per target. */
export const changeTarget = (c: StagedChange) => (c.kind === 'radio' ? `radio:${c.id}` : `ssid:${c.kind === 'ssid-create' ? c.ssid.name : c.name}`)

export const changeLabel = (c: StagedChange, bandOf: (id: number) => string) => {
  switch (c.kind) {
    case 'ssid-create':
      return `Create network “${c.ssid.name}”`
    case 'ssid-update':
      return c.ssid.name !== c.name ? `Rename “${c.name}” to “${c.ssid.name}”` : `Change network “${c.name}”`
    case 'ssid-delete':
      return `Delete network “${c.name}”`
    case 'radio':
      return `Change ${bandOf(c.id)} GHz radio (channel ${c.radio.channel}, ${c.radio.width} MHz, ${c.radio.enabled ? 'on' : 'off'})`
  }
}

export const useStaging = create<StagingStore>((set, get) => ({
  changes: [],
  busy: false,

  stage: (change) =>
    set((s) => {
      const target = changeTarget(change)
      const existing = s.changes.find((c) => changeTarget(c) === target)
      let next: StagedChange = change
      // Editing a network that is still only staged for creation keeps it a creation.
      if (existing?.kind === 'ssid-create' && change.kind === 'ssid-update') next = { kind: 'ssid-create', ssid: change.ssid }
      // Deleting a staged creation simply drops it.
      if (existing?.kind === 'ssid-create' && change.kind === 'ssid-delete') return { changes: s.changes.filter((c) => c !== existing) }
      return { changes: [...s.changes.filter((c) => c !== existing), next] }
    }),

  remove: (index) => set((s) => ({ changes: s.changes.filter((_, i) => i !== index) })),

  discard: () => set({ changes: [] }),

  applyAll: async () => {
    const { changes, busy } = get()
    if (busy) return false
    if (!changes.length) return true
    set({ busy: true })
    const ok = await useApp.getState().change(`${changes.length} ${changes.length === 1 ? 'change' : 'changes'} applied together`, () => api.applyBatch(changes))
    set({ busy: false, ...(ok ? { changes: [] } : {}) })
    return ok
  },
}))
