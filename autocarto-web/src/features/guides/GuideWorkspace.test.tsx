import { beforeEach, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import GuideWorkspace from './GuideWorkspace'
import { useConnectionStore } from '@/stores/connectionStore'
import type { GuideDetail } from './types'
const mocks = vi.hoisted(() => ({list: vi.fn(), get: vi.fn(), save: vi.fn(), create: vi.fn(), import: vi.fn(), copy: vi.fn(), export: vi.fn(), setEnabled: vi.fn()}))
vi.mock('./api', () => ({guideApi: mocks}))
vi.mock('./ResourceLibrary', () => ({default: () => <p>样式与版面库</p>}))
vi.mock('@/hooks/useUnsavedChangesGuard', () => ({useUnsavedChangesGuard: vi.fn(), confirmDiscardChanges: () => true}))
vi.mock('@/lib/config-form-helpers', () => ({isDesktopRuntime: () => false, pickDirectoryPath: vi.fn(), pickFilePath: vi.fn()}))
const markdown = '---\n名称: 河流图\n说明: 制作真实河流图\n版本: 2.0.0\n---\n# 河流图\n\n任意组织正文，读取真实河流数据，使用样式「蓝色河流」。'
const detail = (): GuideDetail => ({meta: {id: 'river', name: '河流图', version: '2.0.0'}, source: 'user', enabled: true, updated_at: '', guide_md: markdown, resources: [], diagnostics: [], guide: {body: markdown, resources: [], diagnostics: []}, resolved_resources: [{id: 'style.river', key: 'river', name: '蓝色河流', label: '蓝色河流', type: 'style', location: '', geometry: 'line', label_field: 'name', entries: []}]})
beforeEach(() => { vi.clearAllMocks(); useConnectionStore.setState({backendReady: true}); mocks.list.mockResolvedValue([{...detail().meta, source: 'user', enabled: true, description: '制作真实河流图'}]); mocks.get.mockResolvedValue(detail()); mocks.save.mockImplementation(async (_id, text) => ({...detail(), guide_md: text, diagnostics: [{level: 'warning', code: 'FLOW_RESOURCE_NOT_FOUND', message: '样式不存在'}]})); window.history.replaceState({}, '', '/') })
it('renders free prose without a resource sidebar or semantic tabs', async () => {
  render(<GuideWorkspace/>); fireEvent.click(await screen.findByRole('button', {name: /河流图 制作真实河流图/}))
  const body = await screen.findByRole('article', {name: '流程正文'})
  expect(within(body).getByText(/任意组织正文/)).toBeVisible(); expect(within(body).queryByText(/版本:/)).not.toBeInTheDocument()
  expect(screen.queryByRole('complementary', {name: '引用的样式与版面'})).not.toBeInTheDocument()
  expect(screen.queryByRole('tablist')).not.toBeInTheDocument(); expect(screen.queryByText('角色对应')).not.toBeInTheDocument()
})
it('previews editing immediately, saves once, and updates diagnostics without calling a model', async () => {
  render(<GuideWorkspace/>); fireEvent.click(await screen.findByRole('button', {name: /河流图 制作真实河流图/})); fireEvent.click(await screen.findByRole('button', {name: '编辑流程'}))
  fireEvent.change(screen.getByLabelText('FLOW.md 内容'), {target: {value: '# 更新流程\n\n新正文'}})
  expect(within(screen.getByRole('article', {name: '流程正文'})).getByText('新正文')).toBeVisible()
  expect(screen.getByRole('link', {name: '在对话中使用'})).toHaveAttribute('aria-disabled', 'true')
  fireEvent.click(screen.getByRole('button', {name: '保存'})); await waitFor(() => expect(mocks.save).toHaveBeenCalledWith('river', '# 更新流程\n\n新正文'))
  expect(await screen.findByText('已保存，流程预览已更新。')).toBeInTheDocument(); expect(screen.getByText('样式不存在')).toBeInTheDocument()
})
it('keeps builtin prose read only and does not enable it merely by selecting it', async () => {
  mocks.get.mockResolvedValue({...detail(), source: 'builtin', enabled: false}); mocks.setEnabled.mockResolvedValue({...detail(), source: 'builtin', enabled: true})
  render(<GuideWorkspace/>); fireEvent.click(await screen.findByRole('button', {name: /河流图 制作真实河流图/})); await screen.findByRole('button', {name: '启用'})
  expect(mocks.setEnabled).not.toHaveBeenCalled(); expect(screen.queryByRole('button', {name: '编辑流程'})).not.toBeInTheDocument(); expect(screen.queryByRole('button', {name: '保存'})).not.toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', {name: '启用'})); await waitFor(() => expect(mocks.setEnabled).toHaveBeenCalledWith('river', true)); expect(screen.getByRole('button', {name: '复制为我的流程'})).toBeInTheDocument()
})
it('exports the document with dependencies and offers no data packaging option', async () => {
  mocks.export.mockResolvedValue({zip_path: '/tmp/river.zip'})
  render(<GuideWorkspace/>); fireEvent.click(await screen.findByRole('button', {name: /河流图 制作真实河流图/})); fireEvent.click(await screen.findByRole('button', {name: '导出流程'}))
  expect(screen.queryByText('包含实际数据文件')).not.toBeInTheDocument()
  fireEvent.change(screen.getByLabelText('导出 ZIP 文件完整路径'), {target: {value: '/tmp/river.zip'}}); fireEvent.click(screen.getByRole('button', {name: '确定'}))
  await waitFor(() => expect(mocks.export).toHaveBeenCalledWith('river', '/tmp/river.zip', false, true))
})
