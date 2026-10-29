import { beforeEach, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { useInstanceStore } from '@/stores/instanceStore'
import ResourceLibrary from './ResourceLibrary'
import type { LibraryItem, ResolvedResource } from './types'
const mocks = vi.hoisted(() => ({capture: vi.fn(), layouts: vi.fn(), layers: vi.fn(), list: vi.fn(), types: vi.fn(), get: vi.fn(), import: vi.fn(), copy: vi.fn(), replace: vi.fn(), references: vi.fn(), remove: vi.fn(), update: vi.fn(), regeneratePreview: vi.fn(), export: vi.fn()}))
vi.mock('./api', () => ({resourceApi: mocks}))
vi.mock('./ResourceMedia', () => ({default: () => <span>大图</span>, ResourceThumbnail: () => <span aria-hidden>缩略图</span>, geometryLabel: (value: string) => ({line: '线', point: '点', polygon: '面', raster: '栅格'}[value] || '')}))
vi.mock('@/lib/config-form-helpers', () => ({isDesktopRuntime: () => false, pickDirectoryPath: vi.fn(), pickFilePath: vi.fn()}))
const river: LibraryItem = {id: 'style.river', name: '河流线', type: 'style', geometry: 'line', label_field: 'NAME', tags: ['水系'], description: '蓝色水系', owner: 'library_builtin', origin: 'library_builtin', path: '/builtin/river.qml', size: 100}
const city: LibraryItem = {...river, id: 'style.city', name: '城市点', geometry: 'point', description: '', tags: [], owner: 'library_user', origin: 'library_user'}
const layout: LibraryItem = {...river, id: 'layout.map', name: '四角版面', type: 'layout', paper: 'A4', owner: 'library_user', origin: 'library_user'}
const resolve = (item: LibraryItem): ResolvedResource => ({...item, key: item.id!, label: item.name, location: item.path, entries: []})
const refs = [{flow_id: 'demo', flow_name: '行军路线图', source: 'builtin', via: 'lock'}]
beforeEach(() => {
  vi.clearAllMocks(); useInstanceStore.setState({instances: [], activeInstanceId: null})
  mocks.types.mockResolvedValue(['style', 'layout'].map(type => ({type, public: true})))
  mocks.list.mockResolvedValue([river, city, layout]); mocks.get.mockImplementation(async id => resolve([river, city, layout].find(item => item.id === id)!)); mocks.references.mockResolvedValue(refs)
  mocks.copy.mockResolvedValue(resolve({...city, name: '副本'})); mocks.remove.mockResolvedValue({deleted: true})
})
const openCity = async () => { fireEvent.click(await screen.findByRole('button', {name: /城市点/})); return screen.findByRole('region', {name: '资源详情'}) }
it('shows two colored tabs and filters styles by geometry, source, tags and description', async () => {
  render(<ResourceLibrary/>); await screen.findByRole('button', {name: /河流线/})
  expect(screen.getAllByRole('tab')).toHaveLength(2); expect(screen.getByRole('tab', {name: '样式'})).toHaveAttribute('aria-selected', 'true')
  fireEvent.change(screen.getByLabelText('样式几何类型'), {target: {value: 'point'}}); expect(screen.queryByRole('button', {name: /河流线/})).not.toBeInTheDocument()
  fireEvent.change(screen.getByLabelText('样式几何类型'), {target: {value: ''}}); fireEvent.change(screen.getByLabelText('搜索样式与版面'), {target: {value: '水系'}})
  expect(screen.getByRole('button', {name: /河流线/})).toBeInTheDocument(); expect(screen.queryByRole('button', {name: /城市点/})).not.toBeInTheDocument()
  fireEvent.change(screen.getByLabelText('搜索样式与版面'), {target: {value: ''}}); fireEvent.click(screen.getByRole('tab', {name: '版面'}))
  expect(screen.getByRole('tab', {name: '版面'})).toHaveAttribute('aria-selected', 'true'); expect(screen.getByRole('button', {name: /四角版面/})).toBeInTheDocument(); expect(screen.queryByText('样式集')).not.toBeInTheDocument()
})
it('copies builtin resources under a unique new name without an override option', async () => {
  render(<ResourceLibrary/>); fireEvent.click(await screen.findByRole('button', {name: /河流线/})); fireEvent.click(await screen.findByRole('button', {name: '复制为我的资源'}))
  fireEvent.change(screen.getByLabelText('资源名称'), {target: {value: '我的河流'}}); fireEvent.click(screen.getByRole('button', {name: '确定'}))
  await waitFor(() => expect(mocks.copy).toHaveBeenCalledWith('style.river', false, '我的河流')); expect(screen.queryByText('复制并覆盖内置')).not.toBeInTheDocument()
})
it('lists affected flows before deletion and only forces after confirmation', async () => {
  render(<ResourceLibrary/>); await openCity(); fireEvent.click(screen.getByRole('button', {name: '删除'}))
  const dialog = await screen.findByRole('dialog'); expect(within(dialog).getByText('行军路线图')).toBeInTheDocument(); expect(mocks.remove).not.toHaveBeenCalled()
  fireEvent.click(within(dialog).getByRole('button', {name: '确认删除'})); await waitFor(() => expect(mocks.remove).toHaveBeenCalledWith('style.city', true))
})
it('keeps existing content when replacement validation fails', async () => {
  mocks.replace.mockRejectedValue(new Error('QML 文件无效')); render(<ResourceLibrary/>); await openCity(); fireEvent.click(screen.getByRole('button', {name: '替换文件'}))
  const dialog = await screen.findByRole('dialog'); fireEvent.change(within(dialog).getByLabelText('替换为文件'), {target: {value: '/tmp/broken.qml'}}); fireEvent.click(within(dialog).getByRole('button', {name: '确定'}))
  expect(await within(dialog).findByRole('alert')).toHaveTextContent('QML 文件无效'); expect(mocks.remove).not.toHaveBeenCalled()
})
it('shows batch import names and renamed items from the backend', async () => {
  mocks.import.mockResolvedValue({items: [resolve(city), resolve({...city, id: 'style.other', name: '城市点（2）'})], warnings: ['同名资源已重命名']}); render(<ResourceLibrary/>); fireEvent.click(screen.getByRole('button', {name: '导入模板或样式'}))
  fireEvent.change(screen.getByLabelText('资源文件或文件夹路径'), {target: {value: '/tmp/styles'}}); fireEvent.click(screen.getByRole('button', {name: '确定'}))
  expect(await screen.findByRole('status')).toHaveTextContent('城市点（2）'); expect(screen.getByRole('status')).toHaveTextContent('同名资源已重命名')
})
it('edits name, description and tags while preserving identity', async () => {
  mocks.update.mockResolvedValue(resolve({...city, name: '驻地点'})); render(<ResourceLibrary/>); await openCity(); fireEvent.click(screen.getByRole('button', {name: '重命名与编辑说明'}))
  fireEvent.change(screen.getByLabelText('名称'), {target: {value: '驻地点'}}); fireEvent.change(screen.getByLabelText('说明'), {target: {value: '驻地样式'}}); fireEvent.change(screen.getByLabelText('标签（用顿号分隔）'), {target: {value: '驻地、点'}}); fireEvent.click(screen.getByRole('button', {name: '保存说明'}))
  await waitFor(() => expect(mocks.update).toHaveBeenCalledWith('style.city', {name: '驻地点', description: '驻地样式', tags: ['驻地', '点']}))
})
it('keeps project capture disabled without a connection', () => {
  render(<ResourceLibrary/>); expect(screen.getByRole('button', {name: '从当前工程保存'})).toBeDisabled()
})
it('captures multiple visible layers as separate styles with default layer names', async () => {
  useInstanceStore.setState({activeInstanceId: 'qgis', instances: [{id: 'qgis', status: 'running'}] as never})
  mocks.layouts.mockResolvedValue({layouts: []}); mocks.layers.mockResolvedValue({items: [{id: 'river', name: '河流', type: 'vector', visible: true}, {id: 'city', name: '城市', type: 'vector', visible: true}, {id: 'hidden', name: '隐藏', type: 'vector', visible: false}]}); mocks.capture.mockResolvedValue({items: [resolve(river), resolve(city)]})
  render(<ResourceLibrary/>); fireEvent.click(screen.getByRole('button', {name: '从当前工程保存'})); fireEvent.change(screen.getByLabelText('保存种类'), {target: {value: 'style'}})
  await screen.findByLabelText('河流'); expect(screen.getByLabelText('河流')).toBeChecked(); expect(screen.getByLabelText('隐藏（隐藏）')).not.toBeChecked(); expect(screen.queryByText('图层样式 → 样式集')).not.toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', {name: '保存到样式与版面库'})); await waitFor(() => expect(mocks.capture).toHaveBeenCalledWith({instance_id: 'qgis', kind: 'style', name: '', layers: ['river', 'city']}))
})
it('reports successful writes separately from a failed refresh', async () => {
  mocks.import.mockResolvedValue(resolve(city)); render(<ResourceLibrary/>); await screen.findByRole('button', {name: /城市点/}); mocks.list.mockRejectedValue(new Error('离线'))
  fireEvent.click(screen.getByRole('button', {name: '导入模板或样式'})); fireEvent.change(screen.getByLabelText('资源文件或文件夹路径'), {target: {value: '/tmp/a.qml'}}); fireEvent.click(screen.getByRole('button', {name: '确定'}))
  expect(await screen.findByRole('status')).toHaveTextContent('已导入'); expect(screen.getByRole('alert')).toHaveTextContent('操作已完成，但列表刷新失败')
})
