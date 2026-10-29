import { cleanup, act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { useInstanceStore } from '@/stores/instanceStore'
import StyleLayerPreview from './StyleLayerPreview'
import { loadStyleLayerPreview, resourceApi } from './api'
vi.mock('./api', () => ({loadStyleLayerPreview: vi.fn(), resourceApi: {layers: vi.fn()}}))
const instance = {id: 'qgis-a', pid: 1, qgis_version: '3.44', project_path: '', status: 'running' as const, services: {}, created_at: '', updated_at: '', last_heartbeat: '', source: 'register' as const}
const originals = Object.fromEntries(['createObjectURL', 'revokeObjectURL'].map(name => [name, Object.getOwnPropertyDescriptor(URL, name)]))
beforeEach(() => {
  vi.clearAllMocks()
  useInstanceStore.setState({instances: [instance], activeInstanceId: instance.id})
  vi.mocked(resourceApi.layers).mockResolvedValue({items: [{id: 'river', name: '河流', type: 'vector'}, {id: 'road', name: '道路', type: 'vector'}, {id: 'dem', name: '高程', type: 'raster'}]})
  Object.defineProperty(URL, 'createObjectURL', {configurable: true, value: vi.fn().mockReturnValue('blob:layer')})
  Object.defineProperty(URL, 'revokeObjectURL', {configurable: true, value: vi.fn()})
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  for (const [name, descriptor] of Object.entries(originals)) {
    if (descriptor) Object.defineProperty(URL, name, descriptor)
    else Reflect.deleteProperty(URL, name)
  }
})
async function selectLayer(id = 'river') { fireEvent.change(await screen.findByLabelText('预览图层'), {target: {value: id}}) }
it('previews the selected layer, discards its result on selection change and shows the backend reason for an incompatible layer', async () => {
  vi.mocked(loadStyleLayerPreview).mockResolvedValueOnce(new Blob(['png'], {type: 'image/png'})).mockRejectedValueOnce(new Error('样式需要人口字段，所选图层没有此字段'))
  render(<StyleLayerPreview resourceId="style.river" geometry="line"/>); await selectLayer()
  expect(screen.queryByRole('option', {name: '高程'})).not.toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', {name: '生成图层预览'}))
  expect(await screen.findByRole('img', {name: '河流套用样式后的预览'})).toHaveAttribute('src', 'blob:layer')
  expect(loadStyleLayerPreview).toHaveBeenCalledWith('style.river', 'qgis-a', 'river', expect.any(AbortSignal))
  await selectLayer('road'); expect(screen.queryByRole('img')).not.toBeInTheDocument(); expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:layer')
  fireEvent.click(screen.getByRole('button', {name: '生成图层预览'}))
  expect(await screen.findByRole('alert')).toHaveTextContent('样式需要人口字段')
})
it('cancels an in-flight render and ignores a late response after changing the selected layer or closing the preview', async () => {
  let resolve!: (blob: Blob) => void
  vi.mocked(loadStyleLayerPreview).mockImplementation(() => new Promise(done => { resolve = done }))
  const view = render(<StyleLayerPreview resourceId="style.river" geometry="line"/>); await selectLayer()
  fireEvent.click(screen.getByRole('button', {name: '生成图层预览'}))
  const firstSignal = vi.mocked(loadStyleLayerPreview).mock.calls[0][3]!
  await selectLayer('road'); expect(firstSignal.aborted).toBe(true)
  await act(async () => resolve(new Blob(['late'], {type: 'image/png'})))
  expect(URL.createObjectURL).not.toHaveBeenCalled(); expect(screen.queryByRole('img')).not.toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', {name: '生成图层预览'}))
  const secondSignal = vi.mocked(loadStyleLayerPreview).mock.calls[1][3]!
  fireEvent.click(screen.getByRole('button', {name: '取消生成'})); expect(secondSignal.aborted).toBe(true)
  fireEvent.click(screen.getByRole('button', {name: '生成图层预览'}))
  const thirdSignal = vi.mocked(loadStyleLayerPreview).mock.calls[2][3]!
  view.unmount(); expect(thirdSignal.aborted).toBe(true)
})
it('aborts when the active instance disconnects and never silently uses a different instance', async () => {
  vi.mocked(loadStyleLayerPreview).mockImplementation(() => new Promise(() => {}))
  render(<StyleLayerPreview resourceId="style.river" geometry="line"/>); await selectLayer()
  fireEvent.click(screen.getByRole('button', {name: '生成图层预览'}))
  const signal = vi.mocked(loadStyleLayerPreview).mock.calls[0][3]!
  act(() => useInstanceStore.setState({instances: [{...instance, status: 'dead'}, {...instance, id: 'qgis-b'}]}))
  expect(await screen.findByText(/请先连接并选择一个 QGIS 工程/)).toBeVisible(); expect(signal.aborted).toBe(true)
  expect(resourceApi.layers).toHaveBeenCalledTimes(1)
})
it('gives a clear empty state when there is no compatible layer category', async () => {
  vi.mocked(resourceApi.layers).mockResolvedValue({items: [{id: 'dem', name: '高程', type: 'raster'}]})
  render(<StyleLayerPreview resourceId="style.river" geometry="line"/>)
  expect(await screen.findByText('当前工程没有可供此样式预览的矢量图层。')).toBeVisible()
  expect(screen.queryByRole('button', {name: '生成图层预览'})).not.toBeInTheDocument()
})
