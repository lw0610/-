import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import api, { ApiClient } from '@/lib/api'
import { flowApi } from './api'

beforeEach(() => {
  vi.useFakeTimers()
  // Use the same abort semantics as WebKit, with a controllable test clock.
  vi.spyOn(AbortSignal, 'timeout').mockImplementation((milliseconds) => {
    const controller = new AbortController()
    setTimeout(() => controller.abort(new DOMException('Timed out', 'TimeoutError')), milliseconds)
    return controller.signal
  })
  api.setBaseURL('http://localhost:50000')
})

afterEach(() => {
  vi.clearAllTimers()
  vi.useRealTimers()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  api.setBaseURL('')
})

function delayedResponse(milliseconds: number) {
  const fetchMock = vi.fn((_url: string, options?: RequestInit) => new Promise<Response>((resolve, reject) => {
    const timer = setTimeout(() => {
      options?.signal?.removeEventListener('abort', aborted)
      resolve(new Response(JSON.stringify({ success: true, data: { saved: true } })))
    }, milliseconds)
    function aborted() {
      clearTimeout(timer)
      // WebKit can report this instead of the TimeoutError in signal.reason.
      reject(new DOMException('Fetch is aborted', 'AbortError'))
    }
    options?.signal?.addEventListener('abort', aborted, { once: true })
  }))
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

describe('nonblocking flow requests', () => {
  it.each([
    ['create', () => flowApi.create('flow source', 'new-flow')],
    ['save', () => flowApi.save('demo', 'flow source', 'default')],
    ['import', () => flowApi.import('/tmp/flow.zip', false)],
    ['copy', () => flowApi.copy('demo', 'copy', 'Copy')],
  ] as const)('%s uses the ordinary timeout without waiting for model analysis', async (_name, operation) => {
    const fetchMock = delayedResponse(20_000)
    const result = operation().then(value => ({ value }), error => ({ error }))
    await vi.advanceTimersByTimeAsync(20_000)
    expect(await result).toMatchObject({ error: { code: 'REQUEST_TIMEOUT' } })
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('reports a readable timeout without retrying a possibly saved create', async () => {
    const fetchMock = delayedResponse(20_000)
    const result = flowApi.create('flow source', 'new-flow').catch(error => error)
    await vi.advanceTimersByTimeAsync(15_000)
    expect(await result).toMatchObject({ code: 'REQUEST_TIMEOUT', message: expect.stringContaining('15 秒') })
    expect((await result).message).toContain('可能已保存')
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('keeps the shorter timeout for ordinary reads', async () => {
    delayedResponse(20_000)
    const result = new ApiClient('http://localhost:50000').get('/api/flows').catch(error => error)
    await vi.advanceTimersByTimeAsync(15_000)
    expect(await result).toMatchObject({ code: 'REQUEST_TIMEOUT', message: expect.stringContaining('15 秒') })
    expect(await result).not.toMatchObject({ message: expect.stringContaining('可能已保存') })
  })

  it('uses the normal timeout for deterministic binding updates', async () => {
    delayedResponse(20_000)
    const result = flowApi.bind('demo', 'path:data/base.csv', 'data').catch(error => error)
    await vi.advanceTimersByTimeAsync(15_000)
    expect(await result).toMatchObject({ code: 'REQUEST_TIMEOUT', message: expect.stringContaining('15 秒') })
  })

  it('also explains a timeout while reading the response body', async () => {
    vi.stubGlobal('fetch', vi.fn(async (_url: string, options?: RequestInit) => ({
      ok: true,
      json: () => new Promise((_resolve, reject) => {
        options?.signal?.addEventListener('abort', () => reject(new DOMException('Fetch is aborted', 'AbortError')), { once: true })
      }),
    })))
    const result = new ApiClient('http://localhost:50000').post('/api/flows', {}).catch(error => error)
    await vi.advanceTimersByTimeAsync(15_000)
    expect(await result).toMatchObject({ code: 'REQUEST_TIMEOUT', message: expect.stringContaining('可能已保存') })
  })
})
