import { renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import api from '@/lib/api'
import { useHydration } from '@/hooks/useHydration'
import { useConnectionStore } from '@/stores/connectionStore'
import { useInstanceStore } from '@/stores/instanceStore'
import { useRuntimeSnapshotStore } from '@/stores/runtimeSnapshotStore'
import { useSessionStore } from '@/stores/sessionStore'
import { useSessionViewStore } from '@/stores/sessionViewStore'
import { useUIStore } from '@/stores/uiStore'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    default: {
      ...actual.default,
      getRuntimeSnapshot: vi.fn(),
    },
  }
})

describe('useHydration', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    useConnectionStore.setState({
      connected: false,
      backendReady: true,
      backendUrl: 'http://127.0.0.1:3000',
      wsUrl: 'ws://127.0.0.1:3000/ws',
      startupError: '',
      servicePorts: {},
    })
    useRuntimeSnapshotStore.setState({
      snapshot: { services: {}, instances: { items: [], instance_count: 0, running_count: 0 }, stale: false },
      loading: false,
    })
    useSessionStore.setState({
      sessions: [],
      sessionsReady: false,
      activeSessionId: null,
    })
    useUIStore.setState({ displaySessionId: null })
    useInstanceStore.setState({
      instances: [],
      activeInstanceId: null,
      qgisAvailable: false,
      projectConnected: false,
      instanceCount: 0,
      runningCount: 0,
      stale: false,
      loading: false,
    })
    useSessionViewStore.setState({ sessions: [] })
  })

  it('stores a diagnostic startup error when runtime snapshot hydration fails', async () => {
    vi.mocked(api.getRuntimeSnapshot).mockRejectedValueOnce(new Error('runtime snapshot failed'))

    renderHook(() => useHydration(true))

    await waitFor(() => {
      expect(useConnectionStore.getState().startupError).toContain('runtime snapshot failed')
    })
  })

  it('preserves sticky unsupported-contract startup errors after successful hydration', async () => {
    vi.mocked(api.getRuntimeSnapshot).mockResolvedValueOnce({
      services: {},
      instances: { items: [], active_id: undefined, instance_count: 0, running_count: 0 },
      stale: false,
    })
    useConnectionStore.setState({
      startupError: '协议版本不兼容，请刷新页面。',
      backendReady: true,
    })

    renderHook(() => useHydration(true))

    await waitFor(() => {
      expect(api.getRuntimeSnapshot).toHaveBeenCalledTimes(1)
    })

    expect(useConnectionStore.getState().startupError).toBe('协议版本不兼容，请刷新页面。')
  })

  it('rehydrates again after backend recovers from a disconnect', async () => {
    vi.mocked(api.getRuntimeSnapshot)
      .mockResolvedValueOnce({
        services: {},
        instances: { items: [], active_id: undefined, instance_count: 0, running_count: 0 },
        stale: false,
      })
      .mockResolvedValueOnce({
        services: {},
        instances: { items: [], active_id: undefined, instance_count: 0, running_count: 0 },
        stale: false,
      })

    const { rerender } = renderHook(({ backendReady }) => useHydration(backendReady), {
      initialProps: { backendReady: false },
    })

    rerender({ backendReady: true })

    await waitFor(() => {
      expect(api.getRuntimeSnapshot).toHaveBeenCalledTimes(1)
    })

    rerender({ backendReady: false })
    rerender({ backendReady: true })

    await waitFor(() => {
      expect(api.getRuntimeSnapshot).toHaveBeenCalledTimes(2)
    })
  })
})
