import { beforeEach, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { useInstanceStore } from '@/stores/instanceStore'
import ResourceLibrary from './ResourceLibrary'
import type { LibraryItem, ResolvedResource } from './types'

const mocks = vi.hoisted(() => ({
  retired: vi.fn(), capture: vi.fn(), layouts: vi.fn(), layers: vi.fn(), updateAliases: vi.fn(), list: vi.fn(), types: vi.fn(), get: vi.fn(), import: vi.fn(), copy: vi.fn(), replace: vi.fn(), references: vi.fn(), remove: vi.fn(), upgrade: vi.fn(), bind: vi.fn(),
}))
vi.mock('./api', () => ({ resourceApi: mocks }))
vi.mock('./ResourceMedia', () => ({ default: () => null }))
vi.mock('@/lib/config-form-helpers', () => ({ isDesktopRuntime: () => false, pickDirectoryPath: vi.fn(), pickFilePath: vi.fn() }))

const original: LibraryItem = {
  id: 'layout.example', name: '四角版式', type: 'layout', owner: 'library_builtin', origin: 'library_builtin',
  version: '1.0.0', path: '/builtin/layouts/example.layout.yaml', size: 100,
}
const customized: LibraryItem = {
  ...original, id: 'layout.u-a', name: '我的版式', owner: 'library_user', origin: 'library_user',
  overrides: original.id, version: '1.2.3', path: '/user/layouts/example.layout.yaml',
}
const resolve = (item: LibraryItem): ResolvedResource => ({
  ...item, key: item.id!, label: item.name, location: item.path,
  root: '/root', entry: item.path, entries: [], digest: 'sha256:123456789abcdef',
})
beforeEach(() => {
  vi.resetAllMocks()
  useInstanceStore.setState({ instances: [], activeInstanceId: null })
  mocks.retired.mockResolvedValue([])
  mocks.types.mockResolvedValue([{ type: 'layout', public: true, label: '自定义版式标签', words: ['版式'], group: 'layouts', folder: false, entry_extensions: ['.qpt', '.layout.yaml'] }])
  mocks.list.mockResolvedValue([{ ...original, overridden_by: customized.id }, customized])
  mocks.get.mockImplementation(async (id: string) => resolve(id === original.id ? original : customized))
  mocks.references.mockResolvedValue([{ flow_id: 'demo', flow_name: '数据流程', source: 'builtin', via: 'lock' }])
  mocks.copy.mockResolvedValue(resolve(customized))
  mocks.replace.mockResolvedValue(resolve({ ...customized, version: '1.2.4' }))
  mocks.upgrade.mockResolvedValue(resolve({ ...customized, legacy: false }))
  mocks.remove.mockResolvedValue({ deleted: true })
})

it('keeps both resources visible with explicit relationships and reads the clicked identity', async () => {
  render(<ResourceLibrary />)
  expect(await screen.findByRole('button', { name: '自定义版式标签' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '全部' })).toHaveAttribute('aria-pressed', 'true')
  fireEvent.click(screen.getByRole('button', { name: '自定义版式标签' }))
  expect(screen.getByRole('button', { name: '自定义版式标签' })).toHaveAttribute('aria-pressed', 'true')
  expect(screen.getByRole('button', { name: '全部' })).toHaveAttribute('aria-pressed', 'false')
  expect(screen.getByText('覆盖内置「四角版式」')).toBeInTheDocument()
  expect(screen.getByText('已被用户资源覆盖：我的版式')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '四角版式' }))
  await waitFor(() => expect(mocks.get).toHaveBeenCalledWith('layout.example'))
  expect(await screen.findByRole('heading', { name: '四角版式 · 自定义版式标签' })).toBeInTheDocument()
  expect(screen.getByText('摘要：12345678')).toBeInTheDocument()
  expect(screen.queryByText(/版本|1\.0\.0|1\.2\.3/)).not.toBeInTheDocument()
})

it('passes the loaded type catalog with a selected reference instead of relying on another request', async () => {
  const onSelect = vi.fn()
  render(<ResourceLibrary onSelect={onSelect} />)
  fireEvent.click(await screen.findByRole('button', { name: '四角版式' }))
  fireEvent.click(await screen.findByRole('button', { name: '引用到当前流程' }))
  expect(onSelect).toHaveBeenCalledWith(expect.objectContaining({ id: 'layout.example' }), [
    expect.objectContaining({ type: 'layout', words: ['版式'] }),
  ])
})

it('copies explicitly as an override only when that action is chosen', async () => {
  mocks.list.mockResolvedValue([original])
  render(<ResourceLibrary />)
  fireEvent.click(await screen.findByRole('button', { name: '复制并覆盖内置' }))
  fireEvent.change(screen.getByRole('textbox', { name: '资源名称' }), { target: { value: '自定义四角' } })
  fireEvent.click(screen.getByRole('button', { name: '确定' }))
  await waitFor(() => expect(mocks.copy).toHaveBeenCalledWith('layout.example', true, '自定义四角'))
  expect(await screen.findByRole('status')).toHaveTextContent('已复制并覆盖内置资源')
})

it('shows affected flows before deletion and sends force only after explicit confirmation', async () => {
  render(<ResourceLibrary />)
  fireEvent.click(await screen.findByRole('button', { name: '删除' }))
  const dialog = await screen.findByRole('dialog')
  expect(within(dialog).getByText(/数据流程（demo）/)).toBeInTheDocument()
  expect(mocks.remove).not.toHaveBeenCalled()
  fireEvent.click(within(dialog).getByRole('button', { name: '确认删除' }))
  await waitFor(() => expect(mocks.remove).toHaveBeenCalledWith('layout.u-a', true))
})

it('keeps existing resource content when replacement validation fails', async () => {
  mocks.replace.mockRejectedValue(new Error('资源缺少主文件'))
  render(<ResourceLibrary />)
  fireEvent.click(await screen.findByRole('button', { name: '我的版式' }))
  await screen.findByRole('heading', { name: '我的版式 · 自定义版式标签' })
  fireEvent.click(screen.getByRole('button', { name: '替换内容' }))
  const dialog = await screen.findByRole('dialog')
  expect(within(dialog).getByText(/数据流程（demo）/)).toBeInTheDocument()
  fireEvent.change(within(dialog).getByRole('textbox', { name: '替换为文件或文件夹' }), { target: { value: '/tmp/broken' } })
  fireEvent.click(within(dialog).getByRole('button', { name: '确定' }))
  expect(await within(dialog).findByRole('alert')).toHaveTextContent('资源缺少主文件')
  expect(mocks.replace).toHaveBeenCalledWith('layout.u-a', '/tmp/broken')
  expect(mocks.remove).not.toHaveBeenCalled()
})

it('offers replace or save as a new name after an import conflict without deleting anything', async () => {
  mocks.import.mockRejectedValueOnce(Object.assign(new Error('同名资源已存在'), { status: 409 })).mockResolvedValue(resolve(customized))
  render(<ResourceLibrary />)
  const importButton = await screen.findByRole('button', { name: '导入模板或样式' })
  await waitFor(() => expect(importButton).toBeEnabled())
  fireEvent.click(importButton)
  fireEvent.change(screen.getByRole('textbox', { name: '资源文件或文件夹路径' }), { target: { value: '/tmp/my.qpt' } })
  fireEvent.click(screen.getByRole('button', { name: '确定' }))
  expect(await screen.findByRole('button', { name: '替换现有资源' })).toBeDisabled()
  fireEvent.click(screen.getByRole('button', { name: '另存为新名称' }))
  fireEvent.change(screen.getByRole('textbox', { name: '新的资源名称' }), { target: { value: '另一个版式' } })
  fireEvent.click(screen.getByRole('button', { name: '确定' }))
  await waitFor(() => expect(mocks.import).toHaveBeenLastCalledWith('/tmp/my.qpt', '另一个版式'))
  expect(mocks.remove).not.toHaveBeenCalled()
})

it('upgrades a legacy resource by its exact ID', async () => {
  mocks.list.mockResolvedValue([{ ...customized, id: 'legacy.layout.abc', legacy: true }])
  render(<ResourceLibrary />)
  fireEvent.click(await screen.findByRole('button', { name: '升级' }))
  await waitFor(() => expect(mocks.upgrade).toHaveBeenCalledWith('legacy.layout.abc'))
  expect(await screen.findByRole('status')).toHaveTextContent('已生成资源清单')
})

it('reports a saved mutation separately from a subsequent list refresh failure', async () => {
  mocks.list.mockResolvedValueOnce([original]).mockRejectedValue(new Error('网络错误'))
  render(<ResourceLibrary />)
  fireEvent.click(await screen.findByRole('button', { name: '复制为我的资源' }))
  fireEvent.click(screen.getByRole('button', { name: '确定' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('操作已完成，但资源列表刷新失败')
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  expect(mocks.copy).toHaveBeenCalledTimes(1)
})

it('disables project capture without a connected instance and does not ask for an import type', async () => {
  render(<ResourceLibrary />)
  await screen.findByRole('button', { name: '四角版式' })
  expect(screen.getByRole('button', { name: '从当前工程保存' })).toBeDisabled()
  expect(screen.queryByRole('combobox', { name: '资源类型' })).not.toBeInTheDocument()
})

it('captures a layout using the selected map UUID and shows the imported identity', async () => {
  useInstanceStore.setState({ activeInstanceId: 'qgis-1', instances: [{ id: 'qgis-1', status: 'running' } as never] })
  mocks.layouts.mockResolvedValue({ layouts: [{ name: '工程版面', map_items: [{ id: '', uuid: 'map-1', name: '地图一' }, { id: '', uuid: 'map-2', name: '地图二' }] }] })
  mocks.capture.mockResolvedValue(resolve(customized))
  render(<ResourceLibrary />)
  await screen.findByRole('button', { name: '四角版式' })
  fireEvent.click(screen.getByRole('button', { name: '从当前工程保存' }))
  const dialog = await screen.findByRole('dialog')
  fireEvent.change(await within(dialog).findByRole('combobox', { name: '主地图' }), { target: { value: 'map-2' } })
  fireEvent.change(within(dialog).getByRole('textbox', { name: '保存名称' }), { target: { value: '保存版面' } })
  fireEvent.click(within(dialog).getByRole('button', { name: '保存到模板与样式库' }))
  await waitFor(() => expect(mocks.capture).toHaveBeenCalledWith({ instance_id: 'qgis-1', kind: 'layout', name: '保存版面', layout: '工程版面', main_map_item: 'map-2' }))
  expect(await screen.findByRole('heading', { name: '我的版式 · 自定义版式标签' })).toBeInTheDocument()
})

it('defaults a style set to visible vectors and permits a different explicit selection', async () => {
  useInstanceStore.setState({ activeInstanceId: 'qgis-1', instances: [{ id: 'qgis-1', status: 'running' } as never] })
  mocks.layouts.mockResolvedValue({ layouts: [] })
  mocks.layers.mockResolvedValue({ items: [{ id: 'visible', name: '道路', type: 'vector', visible: true }, { id: 'hidden', name: '河流', type: 'vector', visible: false }, { id: 'raster', name: '影像', type: 'raster', visible: true }] })
  mocks.capture.mockResolvedValue(resolve(customized))
  render(<ResourceLibrary />)
  await screen.findByRole('button', { name: '四角版式' })
  fireEvent.click(screen.getByRole('button', { name: '从当前工程保存' }))
  fireEvent.change(screen.getByRole('combobox', { name: '保存种类' }), { target: { value: 'style_set' } })
  expect(await screen.findByRole('checkbox', { name: '道路' })).toBeChecked()
  expect(screen.getByRole('checkbox', { name: '河流（隐藏）' })).not.toBeChecked()
  expect(screen.queryByText('影像')).not.toBeInTheDocument()
  fireEvent.change(screen.getByRole('textbox', { name: '保存名称' }), { target: { value: '道路样式集' } })
  fireEvent.click(screen.getByRole('button', { name: '保存到模板与样式库' }))
  await waitFor(() => expect(mocks.capture).toHaveBeenCalledWith({ instance_id: 'qgis-1', kind: 'style_set', name: '道路样式集', layers: ['visible'] }))
})
