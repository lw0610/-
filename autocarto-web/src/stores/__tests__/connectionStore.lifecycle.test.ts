import { beforeEach, describe, expect, it, vi } from 'vitest'

const setBaseURLMock = vi.fn()
vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    default: {
      ...actual.default,
      setBaseURL: setBaseURLMock,
    },
  }
})

describe('connection store lifecycle baseline', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    vi.resetModules()
    setBaseURLMock.mockReset()
  })

  it('does not invent a default websocket runtime without Tauri or env urls', async () => {
    delete process.env.NEXT_PUBLIC_AUTOCARTO_BACKEND_URL
    delete process.env.NEXT_PUBLIC_API_URL
    delete process.env.NEXT_PUBLIC_AUTOCARTO_WS_URL
    delete process.env.NEXT_PUBLIC_WS_URL
    const { useConnectionStore } = await import('@/stores/connectionStore')

    useConnectionStore.setState({
      connected: false,
      backendReady: false,
      backendUrl: '',
      wsUrl: '',
      startupError: '',
      authoritativeStartupError: '',
      servicePorts: {},
    })

    await useConnectionStore.getState().initializeRuntime()

    expect(setBaseURLMock).toHaveBeenCalledWith('')
    expect(useConnectionStore.getState().backendUrl).toBe('')
    expect(useConnectionStore.getState().wsUrl).toBe('')
    expect(useConnectionStore.getState().backendReady).toBe(true)
    expect(useConnectionStore.getState().startupError).toBe('')
  })

  it('waits without descriptor retries when no web runtime url is configured', async () => {
    delete process.env.NEXT_PUBLIC_AUTOCARTO_BACKEND_URL
    delete process.env.NEXT_PUBLIC_API_URL
    delete process.env.NEXT_PUBLIC_AUTOCARTO_WS_URL
    delete process.env.NEXT_PUBLIC_WS_URL
    const { useConnectionStore } = await import('@/stores/connectionStore')

    useConnectionStore.setState({
      connected: false,
      backendReady: false,
      backendUrl: '',
      wsUrl: '',
      startupError: '',
      authoritativeStartupError: '',
      servicePorts: {},
    })

    const ok = await useConnectionStore.getState().waitForBackend()

    expect(ok).toBe(true)
    expect(useConnectionStore.getState().backendUrl).toBe('')
    expect(useConnectionStore.getState().wsUrl).toBe('')
    expect(useConnectionStore.getState().servicePorts).toEqual({})
  })

  it('accepts explicit web runtime override via NEXT_PUBLIC_AUTOCARTO_BACKEND_URL', async () => {
    const previousBackend = process.env.NEXT_PUBLIC_AUTOCARTO_BACKEND_URL
    const previousWs = process.env.NEXT_PUBLIC_AUTOCARTO_WS_URL
    process.env.NEXT_PUBLIC_AUTOCARTO_BACKEND_URL = 'http://127.0.0.1:50321/v1'
    delete process.env.NEXT_PUBLIC_AUTOCARTO_WS_URL

    try {
      const { useConnectionStore } = await import('@/stores/connectionStore')

      useConnectionStore.setState({
        connected: false,
        backendReady: false,
        backendUrl: '',
        wsUrl: '',
        startupError: '',
        authoritativeStartupError: '',
        servicePorts: {},
      })

      await useConnectionStore.getState().initializeRuntime()

      expect(setBaseURLMock).toHaveBeenCalledWith('http://127.0.0.1:50321')
      expect(useConnectionStore.getState().backendUrl).toBe(
        'http://127.0.0.1:50321',
      )
      expect(useConnectionStore.getState().wsUrl).toBe('ws://127.0.0.1:50321/ws')
    } finally {
      vi.unstubAllGlobals()
      if (typeof previousBackend === 'string') {
        process.env.NEXT_PUBLIC_AUTOCARTO_BACKEND_URL = previousBackend
      } else {
        delete process.env.NEXT_PUBLIC_AUTOCARTO_BACKEND_URL
      }
      if (typeof previousWs === 'string') {
        process.env.NEXT_PUBLIC_AUTOCARTO_WS_URL = previousWs
      } else {
        delete process.env.NEXT_PUBLIC_AUTOCARTO_WS_URL
      }
    }
  })

  it('accepts legacy web runtime override via NEXT_PUBLIC_API_URL', async () => {
    const previousBackend = process.env.NEXT_PUBLIC_AUTOCARTO_BACKEND_URL
    const previousApiUrl = process.env.NEXT_PUBLIC_API_URL
    const previousWs = process.env.NEXT_PUBLIC_AUTOCARTO_WS_URL
    const previousLegacyWs = process.env.NEXT_PUBLIC_WS_URL
    delete process.env.NEXT_PUBLIC_AUTOCARTO_BACKEND_URL
    process.env.NEXT_PUBLIC_API_URL = 'http://127.0.0.1:50331/api'
    delete process.env.NEXT_PUBLIC_AUTOCARTO_WS_URL
    delete process.env.NEXT_PUBLIC_WS_URL

    try {
      const { useConnectionStore } = await import('@/stores/connectionStore')

      useConnectionStore.setState({
        connected: false,
        backendReady: false,
        backendUrl: '',
        wsUrl: '',
        startupError: '',
        authoritativeStartupError: '',
        servicePorts: {},
      })

      await useConnectionStore.getState().initializeRuntime()

      expect(setBaseURLMock).toHaveBeenCalledWith('http://127.0.0.1:50331')
      expect(useConnectionStore.getState().backendUrl).toBe(
        'http://127.0.0.1:50331',
      )
      expect(useConnectionStore.getState().wsUrl).toBe('ws://127.0.0.1:50331/ws')
    } finally {
      vi.unstubAllGlobals()
      if (typeof previousBackend === 'string') {
        process.env.NEXT_PUBLIC_AUTOCARTO_BACKEND_URL = previousBackend
      } else {
        delete process.env.NEXT_PUBLIC_AUTOCARTO_BACKEND_URL
      }
      if (typeof previousApiUrl === 'string') {
        process.env.NEXT_PUBLIC_API_URL = previousApiUrl
      } else {
        delete process.env.NEXT_PUBLIC_API_URL
      }
      if (typeof previousWs === 'string') {
        process.env.NEXT_PUBLIC_AUTOCARTO_WS_URL = previousWs
      } else {
        delete process.env.NEXT_PUBLIC_AUTOCARTO_WS_URL
      }
      if (typeof previousLegacyWs === 'string') {
        process.env.NEXT_PUBLIC_WS_URL = previousLegacyWs
      } else {
        delete process.env.NEXT_PUBLIC_WS_URL
      }
    }
  })

  it('does not use hardcoded fallback ports or HTTP probing', async () => {
    const previousBackend = process.env.NEXT_PUBLIC_AUTOCARTO_BACKEND_URL
    const previousApiUrl = process.env.NEXT_PUBLIC_API_URL
    delete process.env.NEXT_PUBLIC_AUTOCARTO_BACKEND_URL
    delete process.env.NEXT_PUBLIC_API_URL

    try {
      const fetchMock = vi.fn()
      vi.stubGlobal('fetch', fetchMock)
      const { useConnectionStore } = await import('@/stores/connectionStore')

      useConnectionStore.setState({
        connected: false,
        backendReady: false,
        backendUrl: '',
        wsUrl: '',
        startupError: '',
        authoritativeStartupError: '',
        servicePorts: {},
      })

      await useConnectionStore.getState().initializeRuntime()

      expect(fetchMock).not.toHaveBeenCalled()
      expect(useConnectionStore.getState().backendUrl).toBe('')
      expect(useConnectionStore.getState().wsUrl).toBe('')
      expect(setBaseURLMock).toHaveBeenCalledWith('')
    } finally {
      vi.unstubAllGlobals()
      if (typeof previousBackend === 'string') {
        process.env.NEXT_PUBLIC_AUTOCARTO_BACKEND_URL = previousBackend
      } else {
        delete process.env.NEXT_PUBLIC_AUTOCARTO_BACKEND_URL
      }
      if (typeof previousApiUrl === 'string') {
        process.env.NEXT_PUBLIC_API_URL = previousApiUrl
      } else {
        delete process.env.NEXT_PUBLIC_API_URL
      }
    }
  })
})
