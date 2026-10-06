import { create } from 'zustand'
import api, {
  type RuntimeServicesSnapshot,
  type RuntimeSnapshot,
  toRuntimeServicesSnapshot,
} from '@/lib/api'
import { reportClientIssue } from '@/lib/clientDiagnostics'
import { useRuntimeSnapshotStore } from '@/stores/runtimeSnapshotStore'

const EMPTY_RUNTIME_SERVICES_SNAPSHOT: RuntimeServicesSnapshot = {
  services: {},
  instances: {},
  stale: false,
}

interface RuntimeServicesState {
  snapshot: RuntimeServicesSnapshot
  loading: boolean
  applyRuntimeSnapshot: (snapshot: RuntimeSnapshot) => void
  fetchSnapshot: () => Promise<void>
}

export const useRuntimeServicesStore = create<RuntimeServicesState>((set) => ({
  snapshot: EMPTY_RUNTIME_SERVICES_SNAPSHOT,
  loading: false,

  applyRuntimeSnapshot: (snapshot) => {
    set({ snapshot: toRuntimeServicesSnapshot(snapshot), loading: false })
  },

  fetchSnapshot: async () => {
    set({ loading: true })
    try {
      const snapshot = await useRuntimeSnapshotStore.getState().fetchSnapshot()
      set({ snapshot: toRuntimeServicesSnapshot(snapshot), loading: false })
    } catch (primaryError) {
      reportClientIssue('运行时快照刷新失败，准备回退到服务快照', primaryError, '运行时快照刷新失败')
      try {
        const fallback = await api.getRuntimeServices()
        set({ snapshot: fallback, loading: false })
      } catch (fallbackError) {
        reportClientIssue('运行时服务快照回退失败', fallbackError, '运行时服务快照回退失败')
        set({ loading: false })
      }
    }
  },
}))

useRuntimeSnapshotStore.subscribe((state, prevState) => {
  if (state.snapshot === prevState.snapshot) {
    return
  }
  useRuntimeServicesStore.getState().applyRuntimeSnapshot(state.snapshot)
})
