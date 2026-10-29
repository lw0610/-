import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { loadResourcePreview, loadStyleLayerPreview } from './api'
import { useConnectionStore } from '@/stores/connectionStore'

beforeEach(() => useConnectionStore.setState({backendUrl: 'http://127.0.0.1:50999'}))
afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals(); vi.restoreAllMocks() })

it('requests an isolated layer preview with the control contract and reads the PNG response', async () => {
  const fetcher = vi.fn().mockResolvedValue(new Response(new Uint8Array([137, 80, 78, 71]), {headers: {'Content-Type': 'image/png'}}))
  vi.stubGlobal('fetch', fetcher)
  const blob = await loadStyleLayerPreview('style.河流', 'qgis-one', 'layer-one')
  expect(blob.type).toBe('image/png')
  const [url, options] = fetcher.mock.calls[0]
  expect(url).toBe('http://127.0.0.1:50999/api/resources/by-id/style.%E6%B2%B3%E6%B5%81/preview-layer')
  expect(options.method).toBe('POST')
  expect(JSON.parse(options.body)).toEqual({instance_id: 'qgis-one', layer_id: 'layer-one'})
  expect(options.headers.get('X-AutoCarto-Contract-Version')).toBe('v1')
})
it('preserves a concrete backend error and rejects non-image successes', async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce(new Response(JSON.stringify({success: false, error: {message: '图层缺少分类字段 TYPE'}}), {status: 422}))
    .mockResolvedValueOnce(new Response('{}', {headers: {'Content-Type': 'application/json'}})))
  await expect(loadStyleLayerPreview('style.a', 'qgis-one', 'layer-one')).rejects.toThrow('图层缺少分类字段 TYPE')
  await expect(loadResourcePreview('style.a')).rejects.toThrow('服务未返回有效预览图片')
})
it('propagates caller cancellation while the image body is downloading', async () => {
  const fetcher = vi.fn(async (_url: string, options: RequestInit) => ({ok: true, blob: () => new Promise((_resolve, reject) => {
    options.signal?.addEventListener('abort', () => reject(new DOMException('Fetch is aborted', 'AbortError')), {once: true})
  })}))
  vi.stubGlobal('fetch', fetcher)
  const caller = new AbortController()
  const result = loadStyleLayerPreview('style.a', 'qgis-one', 'layer-one', caller.signal).catch(reason => reason)
  await vi.waitFor(() => expect(fetcher).toHaveBeenCalledOnce())
  caller.abort()
  expect(await result).toMatchObject({name: 'AbortError'})
  expect(fetcher.mock.calls[0][1].signal?.aborted).toBe(true)
})
it('explains a timed-out preview instead of exposing Fetch is aborted', async () => {
  vi.useFakeTimers()
  const fetcher = vi.fn((_url: string, options: RequestInit) => new Promise((_resolve, reject) => {
    options.signal?.addEventListener('abort', () => reject(new DOMException('Fetch is aborted', 'AbortError')), {once: true})
  }))
  vi.stubGlobal('fetch', fetcher)
  const result = loadStyleLayerPreview('style.a', 'qgis-one', 'layer-one').catch(reason => reason)
  await vi.waitFor(() => expect(fetcher).toHaveBeenCalledOnce())
  await vi.advanceTimersByTimeAsync(60000)
  expect(await result).toMatchObject({message: '预览请求超时，请稍后重试。'})
})
