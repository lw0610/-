import { useUIStore } from '@/stores/uiStore'
import { useSessionStore } from '@/stores/sessionStore'
import { useChatStore } from '@/stores/chatStore'

let initialized = false

export function initUIStoreBridge() {
  if (initialized) return
  initialized = true

  useUIStore.subscribe((state, prev) => {
    if (state.displaySessionId !== prev.displaySessionId) {
      const current = useSessionStore.getState().activeSessionId
      if (current !== state.displaySessionId) {
        useSessionStore.getState().setActiveSession(state.displaySessionId)
      }
    }
    if (state.inspectorOpen !== prev.inspectorOpen) {
      const current = useChatStore.getState().inspectorOpen
      if (current !== state.inspectorOpen) {
        useChatStore.getState().setInspectorOpen(state.inspectorOpen)
      }
    }
  })

  useSessionStore.subscribe((state, prev) => {
    if (state.activeSessionId !== prev.activeSessionId) {
      const current = useUIStore.getState().displaySessionId
      if (current !== state.activeSessionId) {
        useUIStore.getState().setDisplaySessionId(state.activeSessionId)
      }
    }
  })

  useChatStore.subscribe((state, prev) => {
    if (state.inspectorOpen !== prev.inspectorOpen) {
      const current = useUIStore.getState().inspectorOpen
      if (current !== state.inspectorOpen) {
        useUIStore.getState().setInspectorOpen(state.inspectorOpen)
      }
    }
  })
}
