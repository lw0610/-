import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  HTTP_CONTROL_CONTRACT_VERSION,
  HTTP_CONTROL_CONTRACT_VERSION_HEADER,
} from './api'
import {
  buildBackendRuntime,
  fetchServicePorts,
  normalizeBackendPort,
} from './backendRuntime'

afterEach(() => {
  vi.restoreAllMocks()
})

describe('backendRuntime authority-only behavior', () => {
  it('does not synthesize legacy defaults for invalid ports', () => {
    expect(normalizeBackendPort(undefined)).toBeNull()
    expect(normalizeBackendPort(0)).toBeNull()
    expect(normalizeBackendPort('3000x')).toBeNull()

    expect(buildBackendRuntime(undefined)).toEqual({
      port: 0,
      backendUrl: '',
      wsUrl: '',
    })
  })

  it('builds authoritative urls only from the provided runtime port', () => {
    expect(buildBackendRuntime(50321)).toEqual({
      port: 50321,
      backendUrl: 'http://127.0.0.1:50321',
      wsUrl: 'ws://127.0.0.1:50321/ws',
    })
  })

  it('fetchServicePorts sends the authoritative http contract header', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ success: true, data: {} }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )
    vi.stubGlobal('fetch', fetchMock)

    await fetchServicePorts('http://127.0.0.1:3000')

    const options = fetchMock.mock.calls[0]?.[1] as RequestInit | undefined
    const headers = new Headers(options?.headers)
    expect(headers.get(HTTP_CONTROL_CONTRACT_VERSION_HEADER)).toBe(
      HTTP_CONTROL_CONTRACT_VERSION,
    )
  })
})
