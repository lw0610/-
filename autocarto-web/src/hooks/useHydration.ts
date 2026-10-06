import { useEffect, useRef } from 'react'
import api from '@/lib/api'
import { formatClientIssue } from '@/lib/clientDiagnostics'
import { useConnectionStore } from '@/stores/connectionStore'
import { useSessionViewStore } from '@/stores/sessionViewStore'
import { useSessionStore } from '@/stores/sessionStore'
import { useInstanceStore } from '@/stores/instanceStore'
import { useRuntimeSnapshotStore } from '@/stores/runtimeSnapshotStore'
import { useUIStore } from '@/stores/uiStore'

export type HydrationStatus = 'idle' | 'loading' | 'ready' | 'error'

function isStickyUnsupportedContract(detail: string) {
  const normalized = detail.trim()
  if (!normalized) {
    return false
  }
  return normalized.includes('协议版本') || normalized.includes('升级客户端')
}

export function useHydration(backendReady: boolean) {
  const statusRef = useRef<HydrationStatus>('idle')
  const attemptedRef = useRef(false)

  useEffect(() => {
    if (!backendReady) {
      attemptedRef.current = false
      statusRef.current = 'idle'
      return
    }
    if (attemptedRef.current) return
    attemptedRef.current = true
    statusRef.current = 'loading'

    const run = async () => {
      try {
        const runtimeSnapshot = await api.getRuntimeSnapshot()

        useRuntimeSnapshotStore.getState().applySnapshot(runtimeSnapshot)
        useInstanceStore.getState().syncFromRuntimeSnapshot(runtimeSnapshot)

        useSessionStore.getState().setSessions([])
        useSessionViewStore.getState().hydrateSessions([])

        const displaySessionId = useUIStore.getState().displaySessionId
        const activeSessionId = useSessionStore.getState().activeSessionId
        if (activeSessionId && !displaySessionId) {
          useUIStore.getState().setDisplaySessionId(activeSessionId)
        }

        const stickyStartupError = useConnectionStore.getState().startupError
        if (!isStickyUnsupportedContract(stickyStartupError)) {
          useConnectionStore.setState({ startupError: '' })
        }
        statusRef.current = 'ready'
      } catch (error) {
        useConnectionStore.setState({
          startupError: `初始化运行时快照失败: ${formatClientIssue(error, 'unknown error')}`,
        })
        statusRef.current = 'error'
      }
    }

    void run()
  }, [backendReady])

  return statusRef
}
