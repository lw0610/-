import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import FlowWorkspace from './FlowWorkspace'
import { useConnectionStore } from '@/stores/connectionStore'
import type { FlowDetail, FlowSummary, ResolvedResource, Diagnostic } from './types'
const mocks = vi.hoisted(() => ({
  inputMemory: vi.fn(),
  setInputMemory: vi.fn(),
  clearInputMemory: vi.fn(),
  list: vi.fn(),
  get: vi.fn(),
  capabilities: vi.fn(),
  create: vi.fn(),
  save: vi.fn(),
  compile: vi.fn(),
  compileStatus: vi.fn(),
  importResource: vi.fn(),
  removeResource: vi.fn(),
  import: vi.fn(),
  copy: vi.fn(),
  export: vi.fn(),
  remove: vi.fn(),
  bind: vi.fn(),
  repairResource: vi.fn(),
  bindingStatus: vi.fn(),
  getAgentConfigs: vi.fn(),
}))
const resourceMocks = vi.hoisted(() => ({ list: vi.fn(), types: vi.fn(), upgrade: vi.fn() }))
vi.mock('./api', () => ({
  flowApi: mocks,
  resourceApi: resourceMocks,
}))
vi.mock('@/lib/api', async original => ({
  ...await original<typeof import('@/lib/api')>(),
  default: {
    getAgentConfigs: mocks.getAgentConfigs,
  },
}))
const detail: FlowDetail = {
  meta: { id: 'demo', name: '示例流程', version: '1.0.0' },
  flow_md: '## 步骤\n### A\n- 负责：甲',
  source: 'user',
  resources: [],
  diagnostics: [],
  plan: {
    api_version: 'flow-plan/v1',
    flow_id: 'demo',
    name: '示例流程',
    scheme: 'default',
    inputs: [],
    resources: [],
    steps: [
      {
        id: 's1',
        index: 1,
        title: '读取数据',
        kind: 'agent',
        role_label: '数据专家',
        instructions: '读取用户提供的文件。',
        on_failure: 'stop',
        expected_outputs: [{ label: '图层', type: 'layers', required: true }],
      },
    ],
  },
}
const summary: FlowSummary = {
  id: 'demo',
  name: '示例流程',
  description: '处理用户数据',
  version: '1.0.0',
  source: 'user',
  updated_at: '',
  enabled: true,
  diagnostics_summary: { errors: 0, warnings: 0, infos: 0 },
}
beforeEach(() => {
  vi.clearAllMocks()
  window.history.replaceState({}, '', '/')
  useConnectionStore.setState({ backendReady: true })
  mocks.inputMemory.mockResolvedValue({})
  mocks.setInputMemory.mockResolvedValue({})
  mocks.clearInputMemory.mockResolvedValue({})
  mocks.list.mockResolvedValue([summary])
  mocks.bindingStatus.mockResolvedValue({ orphaned_count: 0 })
  resourceMocks.list.mockResolvedValue([])
  resourceMocks.types.mockResolvedValue([
    { type: 'data_list', label: '数据清单', words: ['数据清单'], group: 'data', folder: false, entry_extensions: ['.csv'] },
    { type: 'style', public: true, label: '样式', words: ['样式'], group: 'styles', folder: false, entry_extensions: ['.qml'] },
    { type: 'style_set', public: true, label: '样式集', words: ['样式集'], group: 'styles', folder: true, entry_extensions: [] },
  ])
  mocks.repairResource.mockResolvedValue(detail)
  mocks.importResource.mockResolvedValue({})
  mocks.removeResource.mockResolvedValue({})
  mocks.bind.mockResolvedValue({ saved: true })
  mocks.export.mockResolvedValue({ zip_path: '/tmp/flow.zip' })
  mocks.get.mockResolvedValue(detail)
  mocks.capabilities.mockResolvedValue([])
  mocks.getAgentConfigs.mockResolvedValue([
    { config_name: 'default', is_active: true },
  ])
  mocks.save.mockImplementation((_id: string, text: string) =>
    Promise.resolve({ ...detail, flow_md: text }),
  )
  mocks.create.mockResolvedValue(detail)
})
describe('flow center', () => {
  it('shows the saved preview without compiling and keeps unsaved edits local', async () => {
    mocks.get.mockResolvedValue({
      ...detail,
      plan: { ...detail.plan!, plan_status: 'partial', enrichment: { done: 1, total: 2, failed: [{ step_id: 's1', title: '读取数据', reason: 'invalid_json' }] } },
    })
    render(<FlowWorkspace />)
    fireEvent.click(await screen.findByRole('button', { name: /示例流程.*处理用户数据/ }))
    expect(await screen.findByText('1. 读取数据')).toBeInTheDocument()
    const editor = screen.getByRole('textbox', { name: '流程内容' })
    fireEvent.change(editor, { target: { value: 'unsaved draft' } })
    expect(screen.queryByRole('button', { name: /体检|重新编译/ })).not.toBeInTheDocument()
    expect(screen.queryByText(/正在完善|未完善|模型返回格式不正确/)).not.toBeInTheDocument()
    expect(mocks.save).not.toHaveBeenCalled()
    expect(mocks.compile).not.toHaveBeenCalled()
    expect(mocks.compileStatus).not.toHaveBeenCalled()
    expect(editor).toHaveValue('unsaved draft')
    expect(screen.getByText('1. 读取数据')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '试运行' })).toHaveAttribute('aria-disabled', 'true')
  })
  it('updates the step preview from the save response and keeps save and trial run together', async () => {
    const source = '## 步骤\n### 新的读取步骤\n- 负责：甲'
    mocks.save.mockResolvedValue({
      ...detail, flow_md: source,
      plan: { ...detail.plan!, steps: [{ ...detail.plan!.steps[0], title: '新的读取步骤', instructions: '读取修改后的数据。' }] },
    })
    render(<FlowWorkspace />)
    fireEvent.click(await screen.findByRole('button', { name: /示例流程.*处理用户数据/ }))
    fireEvent.change(await screen.findByRole('textbox', { name: '流程内容' }), { target: { value: source } })
    fireEvent.click(screen.getByRole('button', { name: '输入' }))
    const save = screen.getByRole('button', { name: '保存' })
    const run = screen.getByRole('link', { name: '试运行' })
    expect(run.parentElement).toBe(save.parentElement)
    fireEvent.click(save)
    expect(await screen.findByText('1. 新的读取步骤')).toBeInTheDocument()
    expect(screen.getByText('读取修改后的数据。', { selector: 'summary' })).toBeInTheDocument()
    expect(screen.queryByText('1. 读取数据')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '步骤预览' })).toHaveAttribute('aria-pressed', 'true')
    await waitFor(() => expect(run).toHaveAttribute('aria-disabled', 'false'))
    expect(mocks.save).toHaveBeenCalledWith('demo', source, 'default')
    expect(mocks.get).toHaveBeenCalledTimes(1)
    expect(mocks.compile).not.toHaveBeenCalled()
    expect(mocks.compileStatus).not.toHaveBeenCalled()
  })
  it('preserves the draft and old preview when saving fails', async () => {
    mocks.save.mockRejectedValueOnce(new Error('保存失败'))
    render(<FlowWorkspace />)
    fireEvent.click(await screen.findByRole('button', { name: /示例流程.*处理用户数据/ }))
    const editor = await screen.findByRole('textbox', { name: '流程内容' })
    fireEvent.change(editor, { target: { value: 'unsaved draft' } })
    fireEvent.click(screen.getByRole('button', { name: '保存' }))
    expect(await screen.findByText('保存失败')).toBeInTheDocument()
    expect(editor).toHaveValue('unsaved draft')
    expect(screen.getByText('1. 读取数据')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '试运行' })).toHaveAttribute('aria-disabled', 'true')
    expect(mocks.compile).not.toHaveBeenCalled()
  })
  it('opens a scheme link after its list loads and keeps that scheme', async () => {
    window.history.replaceState({}, '', '/flows?flow=demo&scheme=review')
    let resolveList!: (value: FlowSummary[]) => void
    mocks.list.mockImplementation(() => new Promise<FlowSummary[]>((resolve) => {
      resolveList = resolve
    }))
    mocks.getAgentConfigs.mockResolvedValue([
      { config_name: 'default', is_active: true },
      { config_name: 'review', is_active: false },
    ])
    render(<FlowWorkspace />)
    await waitFor(() => expect(mocks.list).toHaveBeenCalledWith('review'))
    expect(mocks.get).not.toHaveBeenCalled()
    resolveList([summary])
    expect(await screen.findByText('1. 读取数据')).toBeInTheDocument()
    expect(mocks.get).toHaveBeenCalledWith('demo', 'review')
    expect(screen.getByRole('combobox', { name: '当前方案' })).toHaveValue('review')
    expect(mocks.list).not.toHaveBeenCalledWith('default')
  })
  it('does not let a delayed running-scheme list replace the linked scheme', async () => {
    window.history.replaceState({}, '', '/flows?flow=demo&scheme=review')
    let resolveSchemes!: (value: { config_name: string; is_active: boolean }[]) => void
    mocks.getAgentConfigs.mockImplementation(() => new Promise((resolve) => {
      resolveSchemes = resolve
    }))
    render(<FlowWorkspace />)
    expect(await screen.findByText('1. 读取数据')).toBeInTheDocument()
    resolveSchemes([
      { config_name: 'default', is_active: true },
      { config_name: 'review', is_active: false },
    ])
    await waitFor(() => expect(screen.getByRole('combobox', { name: '当前方案' })).toHaveValue('review'))
    expect(mocks.list).not.toHaveBeenCalledWith('default')
    expect(mocks.get).toHaveBeenCalledTimes(1)
  })
  it('shows contract-shaped steps and saves edited Markdown', async () => {
    render(<FlowWorkspace />)
    fireEvent.click(
      await screen.findByRole('button', { name: /示例流程.*处理用户数据/ }),
    )
    expect(await screen.findByText('1. 读取数据')).toBeInTheDocument()
    fireEvent.change(screen.getByRole('textbox', { name: '流程内容' }), {
      target: { value: 'changed content' },
    })
    fireEvent.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() =>
      expect(mocks.save).toHaveBeenCalledWith(
        'demo',
        'changed content',
        'default',
      ),
    )
  })
  it('keeps builtin editing and destructive actions disabled', async () => {
    mocks.get.mockResolvedValue({ ...detail, source: 'builtin' })
    render(<FlowWorkspace />)
    fireEvent.click(
      await screen.findByRole('button', { name: /示例流程.*处理用户数据/ }),
    )
    expect(
      await screen.findByRole('textbox', { name: '流程内容' }),
    ).toHaveAttribute('readonly')
    expect(screen.getByRole('button', { name: '删除流程' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '复制为我的流程' })).toBeEnabled()
    expect(screen.getByRole('button', { name: '保存' })).toBeDisabled()
  })
  it('imports with explicit overwrite and opens the step preview', async () => {
    mocks.import.mockResolvedValue({ flow: detail })
    render(<FlowWorkspace />)
    fireEvent.click(screen.getByRole('button', { name: '导入' }))
    fireEvent.change(
      screen.getByRole('textbox', { name: '文件夹、ZIP 或 FLOW.md 路径' }),
      { target: { value: '/tmp/flow.zip' } },
    )
    fireEvent.click(
      screen.getByRole('checkbox', {
        name: '覆盖同编号流程（旧版本自动备份）',
      }),
    )
    fireEvent.click(screen.getByRole('button', { name: '确定' }))
    await waitFor(() =>
      expect(mocks.import).toHaveBeenCalledWith(
        '/tmp/flow.zip',
        true,
        undefined,
      ),
    )
    expect(
      await screen.findByRole('button', { name: '步骤预览', pressed: true }),
    ).toBeInTheDocument()
  })
  it('creates a template and blocks trial runs when diagnostics contain errors', async () => {
    mocks.get.mockResolvedValue({
      ...detail,
      diagnostics: [
        {
          level: 'error',
          code: 'FLOW_RESOURCE_NOT_FOUND',
          message: '资源缺失',
        },
      ],
    })
    render(<FlowWorkspace />)
    fireEvent.click(screen.getByRole('button', { name: '新建流程' }))
    fireEvent.change(
      screen.getByRole('textbox', {
        name: '流程编号（小写字母、数字、连字符）',
      }),
      { target: { value: 'new-flow' } },
    )
    fireEvent.click(screen.getByRole('button', { name: '确定' }))
    await waitFor(() =>
      expect(mocks.create).toHaveBeenCalledWith(
        expect.stringContaining('## 步骤'),
        'new-flow',
      ),
    )
    fireEvent.click(
      await screen.findByRole('button', { name: /示例流程.*处理用户数据/ }),
    )
    await waitFor(() =>
      expect(screen.getByRole('link', { name: '试运行' })).toHaveAttribute(
        'aria-disabled',
        'true',
      ),
    )
    expect(screen.getByRole('alert')).toHaveTextContent('资源缺失')
    expect(mocks.remove).not.toHaveBeenCalled()
  })
  it('keeps a saved flow editable when no preview is available', async () => {
    let finishCreate!: (value: FlowDetail) => void
    mocks.create.mockImplementation(() => new Promise<FlowDetail>((resolve) => { finishCreate = resolve }))
    render(<FlowWorkspace />)
    await screen.findByRole('button', { name: /示例流程.*处理用户数据/ })
    fireEvent.click(screen.getByRole('button', { name: '新建流程' }))
    fireEvent.change(screen.getByRole('textbox', { name: '流程编号（小写字母、数字、连字符）' }), { target: { value: 'new-flow' } })
    fireEvent.click(screen.getByRole('button', { name: '确定' }))
    expect(await screen.findByText('正在保存流程…')).toBeInTheDocument()
    finishCreate({ ...detail, plan: null })
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(screen.getByRole('textbox', { name: '流程内容' })).toHaveValue(detail.flow_md)
    expect(screen.getByText('流程已创建。')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '试运行' })).toHaveAttribute('aria-disabled', 'true')
    expect(mocks.create).toHaveBeenCalledTimes(1)
  })
  it('closes the successful create dialog even if refreshing the list fails', async () => {
    render(<FlowWorkspace />)
    await screen.findByRole('button', { name: /示例流程.*处理用户数据/ })
    mocks.list.mockRejectedValue(new Error('请求超时'))
    fireEvent.click(screen.getByRole('button', { name: '新建流程' }))
    fireEvent.change(screen.getByRole('textbox', { name: '流程编号（小写字母、数字、连字符）' }), { target: { value: 'new-flow' } })
    fireEvent.click(screen.getByRole('button', { name: '确定' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(screen.getByRole('textbox', { name: '流程内容' })).toHaveValue(detail.flow_md)
    expect(screen.getByText('操作已完成，但流程列表刷新失败：请求超时')).toBeInTheDocument()
    expect(mocks.create).toHaveBeenCalledTimes(1)
  })
})

const dataResource: ResolvedResource = {
  key: 'path:data/base.csv', label: '底图', type: 'data_list', owner: 'flow',
  location: 'resources/data/base.csv', entry: '/flow/resources/data/base.csv', root: '/flow/resources/data/base.csv',
  base_dir: '/old-data', binding_key: 'flow:demo/path:data/base.csv', version: '1.0.0', entries: [],
}
function withResource(resource: ResolvedResource = dataResource, diagnostic?: Diagnostic): FlowDetail {
  return {
    ...detail,
    flow_md: '## 使用的资源\n- 样式集「底图」：resources/data/base.csv\n## 步骤\n### 1. 检查\n- 做法：使用「底图」\n',
    resources: [{ path: 'resources/data/base.csv', type: 'file', size: 200, exists: true }],
    plan: { ...detail.plan!, resources: [resource] },
    diagnostics: diagnostic ? [diagnostic] : [],
  }
}
async function openResourceFlow() {
  render(<FlowWorkspace />)
  fireEvent.click(await screen.findByRole('button', { name: /示例流程.*处理用户数据/ }))
  await screen.findByRole('textbox', { name: '流程内容' })
}

it('edits a remembered folder through inputs without binding or model compilation', async () => {
  const current = withResource({ ...dataResource, base_dir_from_input: '底图数据' })
  current.plan!.inputs = [{ key: '底图数据', label: '底图数据', type: 'folder', required: true, remember: true }]
  mocks.get.mockResolvedValue(current)
  mocks.inputMemory.mockResolvedValue({ 底图数据: '/old-data' })
  await openResourceFlow()
  fireEvent.click(screen.getByRole('button', { name: '资源' }))
  fireEvent.click(screen.getByRole('button', { name: '选择输入文件夹' }))
  fireEvent.click(await screen.findByRole('button', { name: '选择底图数据文件夹' }))
  const dialog = await screen.findByRole('dialog')
  fireEvent.change(within(dialog).getByRole('textbox', { name: '路径' }), { target: { value: '/new-data' } })
  fireEvent.click(within(dialog).getByRole('button', { name: '确定' }))
  await waitFor(() => expect(mocks.setInputMemory).toHaveBeenCalledWith('demo', '底图数据', '/new-data'))
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  expect(mocks.bind).not.toHaveBeenCalled()
  expect(mocks.compile).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name: '清除底图数据记忆' }))
  await waitFor(() => expect(mocks.clearInputMemory).toHaveBeenCalledWith('demo', '底图数据'))
})

it('offers dependency bundling by default while leaving data-file inclusion optional', async () => {
  await openResourceFlow()
  fireEvent.click(screen.getByRole('button', { name: '导出' }))
  expect(screen.getByRole('checkbox', { name: '附带依赖的模板与样式' })).toBeChecked()
  expect(screen.getByRole('checkbox', { name: '包含实际数据文件' })).not.toBeChecked()
  fireEvent.change(screen.getByRole('textbox', { name: '导出 ZIP 的完整路径' }), { target: { value: '/tmp/flow.zip' } })
  fireEvent.click(screen.getByRole('button', { name: '确定' }))
  await waitFor(() => expect(mocks.export).toHaveBeenCalledWith('demo', '/tmp/flow.zip', false, true))
})

it('offers conversion for a legacy data binding instead of editing a binding', async () => {
  mocks.get.mockResolvedValue(withResource(dataResource, { level: 'warning', code: 'FLOW_DATA_BINDING_LEGACY', message: '旧数据目录需要转换', fix: { action: 'convert_to_input', resource_key: dataResource.key } }))
  await openResourceFlow()
  fireEvent.click(screen.getByRole('button', { name: '资源' }))
  fireEvent.click(screen.getByRole('button', { name: '转换为文件夹输入' }))
  await waitFor(() => expect(mocks.repairResource).toHaveBeenCalledWith('demo', 'convert_to_input', dataResource.key, undefined, 'default'))
  expect(screen.queryByText('绑定数据基准目录')).not.toBeInTheDocument()
})

it('resolves ambiguity by displaying candidate identities and sending only the selected one', async () => {
  mocks.get.mockResolvedValue(withResource(dataResource, {
    level: 'error', code: 'FLOW_RESOURCE_AMBIGUOUS', message: '存在同名资源',
    fix: { action: 'choose_resource', resource_key: dataResource.key, candidates: [
      { id: 'data_list.one', type: 'data_list', name: '底图', owner: 'library_builtin', version: '1.0.0' },
      { id: 'data_list.two', type: 'data_list', name: '底图', owner: 'library_user', version: '2.0.0' },
    ] },
  }))
  await openResourceFlow()
  fireEvent.click(screen.getByRole('button', { name: '资源' }))
  fireEvent.click(screen.getByRole('button', { name: '选择正确的资源' }))
  const choices = await screen.findByRole('combobox', { name: '资源候选' })
  expect(within(choices).getByRole('option', { name: '底图 · 用户 · data_list.two' })).toBeInTheDocument()
  expect(mocks.repairResource).not.toHaveBeenCalled()
  fireEvent.change(choices, { target: { value: 'data_list.two' } })
  fireEvent.click(screen.getByRole('button', { name: '确定' }))
  await waitFor(() => expect(mocks.repairResource).toHaveBeenCalledWith('demo', 'choose_resource', dataResource.key, 'data_list.two', 'default'))
})

it.each([
  ['update_lock', '更新资源锁定'],
  ['install_resource', '安装到模板与样式库'],
  ['use_library', '改用本机模板与样式'],
  ['upgrade_resource', '升级资源清单'],
])('runs %s through the flow resource repair contract', async (action, label) => {
  mocks.get.mockResolvedValue(withResource(dataResource, {
    level: 'warning', code: 'RESOURCE_REPAIR', message: '资源需要处理', fix: { action, resource_key: dataResource.key },
  }))
  await openResourceFlow()
  fireEvent.click(screen.getByRole('button', { name: '资源' }))
  fireEvent.click(screen.getByRole('button', { name: label }))
  await waitFor(() => expect(mocks.repairResource).toHaveBeenCalledWith('demo', action, dataResource.key, undefined, 'default'))
  expect(mocks.compile).not.toHaveBeenCalled()
})

it('upgrades a user public resource through its own management API', async () => {
  const resource = { ...dataResource, id: 'legacy.data_list.a', owner: 'library_user' }
  mocks.get.mockResolvedValue(withResource(resource, {
    level: 'info', code: 'FLOW_RESOURCE_MANIFEST_MISSING', message: '旧式资源', fix: { action: 'upgrade_resource', resource_key: resource.key },
  }))
  await openResourceFlow()
  fireEvent.click(screen.getByRole('button', { name: '资源' }))
  fireEvent.click(screen.getByRole('button', { name: '升级资源清单' }))
  await waitFor(() => expect(resourceMocks.upgrade).toHaveBeenCalledWith('legacy.data_list.a'))
  expect(mocks.repairResource).not.toHaveBeenCalled()
})

it('offers both bundled-resource actions and sends the one explicitly selected', async () => {
  mocks.get.mockResolvedValue(withResource(dataResource, {
    level: 'info', code: 'FLOW_RESOURCE_BUNDLED', message: '流程包含附带资源，本机已有同编号资源',
    fix: { action: 'install_resource', actions: ['install_resource', 'use_library'], resource_key: dataResource.key },
  }))
  await openResourceFlow()
  fireEvent.click(screen.getByRole('button', { name: '资源' }))
  expect(screen.getByRole('button', { name: '安装到模板与样式库' })).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '改用本机模板与样式' }))
  await waitFor(() => expect(mocks.repairResource).toHaveBeenCalledWith('demo', 'use_library', dataResource.key, undefined, 'default'))
})

it('rewrites the type in the draft without silently saving or losing other edits', async () => {
  const resource = { ...dataResource, type: 'style' }
  const original = withResource(resource, {
    level: 'warning', code: 'FLOW_RESOURCE_TYPE_ADJUSTED', message: '声明类型需要修正', fix: { action: 'rewrite_type', resource_key: resource.key, actual_type: 'style' },
  })
  mocks.get.mockResolvedValue(original)
  await openResourceFlow()
  fireEvent.change(screen.getByRole('textbox', { name: '流程内容' }), { target: { value: `${original.flow_md}\n补充业务说明` } })
  fireEvent.click(screen.getByRole('button', { name: '资源' }))
  fireEvent.click(screen.getByRole('button', { name: '修正资源类型' }))
  await waitFor(() => expect(screen.getByRole('textbox', { name: '流程内容' })).toHaveValue(original.flow_md.replace('样式集「底图」', '样式「底图」') + '\n补充业务说明'))
  expect(mocks.save).not.toHaveBeenCalled()
  expect(mocks.repairResource).not.toHaveBeenCalled()
})

it('asks for a copy before attempting to mutate a builtin flow lock', async () => {
  mocks.get.mockResolvedValue({ ...withResource(dataResource, {
    level: 'info', code: 'FLOW_RESOURCE_CHANGED', message: '资源已变化', fix: { action: 'update_lock', resource_key: dataResource.key },
  }), source: 'builtin' })
  await openResourceFlow()
  fireEvent.click(screen.getByRole('button', { name: '资源' }))
  fireEvent.click(screen.getByRole('button', { name: '更新资源锁定' }))
  expect(await screen.findByRole('textbox', { name: '新流程编号' })).toBeInTheDocument()
  expect(mocks.repairResource).not.toHaveBeenCalled()
})


it('keeps unsaved content when a resource lock operation needs a saved flow', async () => {
  const current = withResource(dataResource, {
    level: 'warning', code: 'FLOW_RESOURCE_CHANGED', message: '资源已变化',
    fix: { action: 'update_lock', resource_key: dataResource.key },
  })
  mocks.get.mockResolvedValue(current)
  await openResourceFlow()
  const editor = screen.getByRole('textbox', { name: '流程内容' })
  fireEvent.change(editor, { target: { value: `${current.flow_md}未保存的说明` } })
  fireEvent.click(screen.getByRole('button', { name: '资源' }))
  fireEvent.click(screen.getByRole('button', { name: '更新资源锁定' }))
  expect(await screen.findByText('请先保存流程内容，再修改资源锁定。')).toBeInTheDocument()
  expect(editor).toHaveValue(`${current.flow_md}未保存的说明`)
  expect(mocks.repairResource).not.toHaveBeenCalled()
  expect(mocks.save).not.toHaveBeenCalled()
})

it('refreshes saved resource details after an upload without a draft check', async () => {
  mocks.get.mockResolvedValue(withResource())
  await openResourceFlow()
  fireEvent.click(screen.getByRole('button', { name: '资源' }))
  fireEvent.click(screen.getByRole('button', { name: '上传 / 替换资源' }))
  fireEvent.change(screen.getByRole('textbox', { name: '资源文件或文件夹路径' }), { target: { value: '/tmp/base.csv' } })
  fireEvent.click(screen.getByRole('button', { name: '确定' }))
  await waitFor(() => expect(mocks.importResource).toHaveBeenCalledWith('demo', '/tmp/base.csv', 'data'))
  await waitFor(() => expect(mocks.get).toHaveBeenCalledTimes(2))
  expect(screen.getByRole('textbox', { name: '流程内容' })).toHaveValue(withResource().flow_md)
  expect(mocks.save).not.toHaveBeenCalled()
})
