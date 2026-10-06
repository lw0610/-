import { beforeEach, describe, expect, it, vi } from 'vitest'

import api from '@/lib/api'
import { useConnectionStore } from '@/stores/connectionStore'
import { useRuntimeServicesStore } from '@/stores/runtimeServicesStore'
import { useRuntimeSnapshotStore } from '@/stores/runtimeSnapshotStore'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    default: {
      ...actual.default,
      getRuntimeServices: vi.fn(),
    },
  }
})

describe('runtime services diagnostics', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    useConnectionStore.setState({
      connected: false,
      backendReady: false,
      backendUrl: 'http://127.0.0.1:3000',
      wsUrl: 'ws://127.0.0.1:3000/ws',
      startupError: '',
      servicePorts: {},
    })
    useRuntimeServicesStore.setState({
      snapshot: { services: {}, instances: {}, stale: false },
      loading: false,
    })
  })

  it('reports a diagnostic when both runtime snapshot paths fail', async () => {
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {})
    useRuntimeSnapshotStore.getState().fetchSnapshot = vi.fn(async () => {
      throw new Error('snapshot failed')
    })
    vi.mocked(api.getRuntimeServices).mockRejectedValueOnce(new Error('fallback failed'))

    await useRuntimeServicesStore.getState().fetchSnapshot()

    expect(warnSpy).toHaveBeenCalled()
    expect(useRuntimeServicesStore.getState().loading).toBe(false)
  })

  it('treats configured websocket runtime as ready without HTTP health check', async () => {
    const fetchMock = vi.fn(async () => {
      throw new Error('network down')
    })
    vi.stubGlobal('fetch', fetchMock)

    const ok = await useConnectionStore.getState().checkHealth()

    expect(ok).toBe(true)
    expect(fetchMock).not.toHaveBeenCalled()
    expect(useConnectionStore.getState().startupError).toBe('')
  })
})
