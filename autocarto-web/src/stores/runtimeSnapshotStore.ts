import { create } from 'zustand'
import api, {
  EMPTY_RUNTIME_SNAPSHOT,
  type RuntimeSnapshot,
  normalizeRuntimeSnapshot,
} from '@/lib/api'

interface RuntimeSnapshotState {
  snapshot: RuntimeSnapshot
  loading: boolean
  applySnapshot: (snapshot?: Partial<RuntimeSnapshot> | null) => RuntimeSnapshot
  fetchSnapshot: () => Promise<RuntimeSnapshot>
  clear: () => void
}

export const useRuntimeSnapshotStore = create<RuntimeSnapshotState>((set) => ({
  snapshot: EMPTY_RUNTIME_SNAPSHOT,
  loading: false,

  applySnapshot: (snapshot) => {
    const normalized = normalizeRuntimeSnapshot(snapshot)
    set({ snapshot: normalized, loading: false })
    return normalized
  },

  fetchSnapshot: async () => {
    set({ loading: true })
    try {
      const snapshot = await api.getRuntimeSnapshot()
      set({ snapshot, loading: false })
      return snapshot
    } catch (error) {
      set({ loading: false })
      throw error
    }
  },

  clear: () => {
    set({ snapshot: EMPTY_RUNTIME_SNAPSHOT, loading: false })
  },
}))
