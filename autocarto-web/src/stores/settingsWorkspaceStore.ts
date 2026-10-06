import { create } from 'zustand'

import api from '@/lib/api'
import {
  emptySettingsWorkspaceMetadata,
  normalizeSettingsWorkspaceMetadata,
  type SettingsWorkspaceMetadata,
} from '@/lib/settingsRegistry'

interface SettingsWorkspaceState {
  metadata: SettingsWorkspaceMetadata
  loading: boolean
  loaded: boolean
  error?: string
  ensureLoaded: () => Promise<void>
}

export const useSettingsWorkspaceStore = create<SettingsWorkspaceState>((set, get) => ({
  metadata: emptySettingsWorkspaceMetadata,
  loading: false,
  loaded: false,
  error: undefined,

  ensureLoaded: async () => {
    const state = get()
    const hasWorkspaceContract =
      state.metadata.tasks.length > 0 || state.metadata.descriptors.length > 0
    if (state.loading || (state.loaded && !state.error && hasWorkspaceContract)) {
      return
    }
    set({ loading: true })
    try {
      const payload = await api.getSettingsWorkspace()
      const metadata = normalizeSettingsWorkspaceMetadata(payload)
      set({
        metadata,
        loading: false,
        loaded: true,
        error:
          metadata.tasks.length > 0 || metadata.descriptors.length > 0
            ? undefined
            : 'settings workspace contract unavailable',
      })
    } catch (error) {
      set({
        metadata: emptySettingsWorkspaceMetadata,
        loading: false,
        loaded: true,
        error:
          error instanceof Error && error.message.trim()
            ? error.message.trim()
            : 'settings workspace contract unavailable',
      })
    }
  },
}))
