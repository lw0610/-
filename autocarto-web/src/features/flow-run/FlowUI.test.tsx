import { beforeEach, describe, expect, it, vi } from 'vitest'
import { act, fireEvent, render, renderHook, screen, waitFor, within } from '@testing-library/react'
import ChatInput from '@/components/chat/ChatInput'
import MessageList from '@/components/chat/MessageList'
import { useFlowComposer } from './useFlowComposer'
import FlowComposer from './FlowComposer'
import FlowOutputs from './FlowOutputs'
import FlowProgressCard from './FlowProgressCard'
import { useChatStore } from '@/stores/chatStore'
import { useConnectionStore } from '@/stores/connectionStore'
import { useSessionStore } from '@/stores/sessionStore'
import { useInstanceStore } from '@/stores/instanceStore'
import { applyFlowEvent } from './model'
import type { FlowRunState, FlowSubmission } from './types'
import type { FlowDetail } from '@/features/flows/types'

const mocks = vi.hoisted(() => ({ inputMemory: vi.fn(), checkInputs: vi.fn(), list: vi.fn(), get: vi.fn(), compile: vi.fn(), compileStatus: vi.fn(), capabilities: vi.fn(), api: vi.fn(), desktop: vi.fn(), file: vi.fn(), directory: vi.fn(), invoke: vi.fn() }))
vi.mock('@/features/flows/api', () => ({ flowApi: mocks }))
vi.mock('@/lib/api', async original => ({ ...await original<typeof import('@/lib/api')>(), default: { get: mocks.api } }))
vi.mock('@/lib/config-form-helpers', () => ({ isDesktopRuntime: mocks.desktop, pickFilePath: mocks.file, pickDirectoryPath: mocks.directory }))
vi.mock('@tauri-apps/api/core', () => ({ invoke: mocks.invoke }))
const detail: FlowDetail = { meta: { id: 'demo', name: '示例流程', version: '1' }, source: 'builtin', flow_md: '', resources: [], diagnostics: [], 
  plan: { api_version: 'flow-plan/v1', flow_id: 'demo', name: '示例流程', scheme: 'default', resources: [], steps: [], inputs: [
    { key: 'source', label: '隐藏原文', type: 'conversation_text', required: true },
    { key: 'paper', label: '纸张', type: 'choice', required: true, default: 'A1', options: ['A0', 'A2'] },
    { key: 'review', label: '质量审查', type: 'boolean', required: false },
    { key: 'count', label: '数量', type: 'number', required: true },
    { key: 'layer', label: '图层', type: 'layer', required: true, options: ['备用图层'] },
    { key: 'file', label: '源文件', type: 'file', required: false, default: null },
  ] } }
function Composer({ send }: { send: (text: string, flow?: FlowSubmission) => Promise<boolean> }) {
  const composer = useFlowComposer(send)
  return <ChatInput onSend={composer.send} sendBlocked={composer.blocked} flowControls={<FlowComposer composer={composer} mode="build"/>}/>
}
const baseProps = { workflowBlocks: [], focusedWorkflowBlockId: null, onOpenWorkflowBlock: vi.fn(), pendingInterrupts: [], interruptSubmitting: '', interruptResolvedStates: {}, onInterruptApprove: vi.fn(), onInterruptReject: vi.fn() }
function run(): FlowRunState {
  return applyFlowEvent(undefined, { event: 'flow.started', session_id: 'sid', flow_run_id: 'r', payload: { flow_id: 'demo', name: '示例流程', steps: [{ id: 's1', index: 1, title: '检查数据', role_label: '数据角色' }] } })!
}
beforeEach(() => {
  vi.clearAllMocks(); window.history.replaceState({}, '', '/')
  mocks.inputMemory.mockResolvedValue({}); mocks.checkInputs.mockResolvedValue([])
  mocks.list.mockResolvedValue([{ id: 'demo', name: '示例流程', enabled: true }, { id: 'disabled', name: '停用流程', enabled: false }])
  mocks.get.mockResolvedValue(detail); mocks.capabilities.mockResolvedValue([])
  mocks.api.mockResolvedValue({ items: [{ id: 'real-layer', name: '真实图层' }] })
  mocks.desktop.mockReturnValue(false); mocks.file.mockResolvedValue('/tmp/source.csv')
  useConnectionStore.setState({ backendReady: true })
  useChatStore.setState({ activeAgentConfig: 'default', executionMode: 'build', sessionRuntimeStates: { sid: { session_id: 'sid', bound_instance_id: 'bound-instance' } } })
  useSessionStore.setState({ activeSessionId: 'sid' }); useInstanceStore.setState({ activeInstanceId: 'other-instance' })
})
describe('flow conversation UI', () => {
  it('labels system acquisition separately while keeping authored step numbers', async () => {
    const steps = [
      { id: 'acquire', index: 0, title: '获取底图', kind: 'acquire', acquisition_requirement: '底图', instructions: '准备底图', on_failure: 'ask' },
      { id: 's1', index: 1, title: '绘制地图', kind: 'agent', instructions: '绘制地图', on_failure: 'ask' },
    ]
    const state = applyFlowEvent(undefined, { event: 'flow.started', session_id: 'sid', flow_run_id: 'r', payload: { flow_id: 'demo', steps, plan: { ...detail.plan!, steps } } })!
    expect(state.steps[0].index).toBe(0)
    const view = render(<FlowProgressCard run={state} messages={[]} renderMessage={() => null}/>)
    expect(screen.getByRole('listitem', { name: '数据准备 获取底图' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '数据准备 · 获取底图' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '1. 绘制地图' })).toBeInTheDocument()
    expect(screen.queryByRole('listitem', { name: /第 0 步/ })).not.toBeInTheDocument()
    await act(async () => {})
    view.unmount()
  })

  it('does not send a stale flow when selection changes during final input validation', async () => {
    mocks.get.mockResolvedValue({ ...detail, plan: { ...detail.plan, inputs: [] } })
    window.history.replaceState({}, '', '/chat?flow=demo')
    const send = vi.fn(async () => true)
    const hook = renderHook(() => useFlowComposer(send))
    await waitFor(() => expect(hook.result.current.blocked).toBe(false))
    let resolve!: (value: []) => void
    mocks.checkInputs.mockReturnValueOnce(new Promise<[]>(done => { resolve = done }))
    let pending!: Promise<boolean | void>
    act(() => { pending = hook.result.current.send('保留消息') })
    act(() => { hook.result.current.select('') })
    await act(async () => { resolve([]); await pending })
    expect(send).not.toHaveBeenCalled()
    hook.unmount()
  })

  it.each(['session', 'active instance', 'bound instance', 'execution mode'] as const)('discards pending validation when the %s changes', async changed => {
    mocks.get.mockResolvedValue({ ...detail, plan: { ...detail.plan!, inputs: [] } })
    window.history.replaceState({}, '', '/chat?flow=demo')
    const send = vi.fn(async () => true)
    const hook = renderHook(() => useFlowComposer(send))
    await waitFor(() => expect(hook.result.current.blocked).toBe(false))
    let resolve!: (value: []) => void
    mocks.checkInputs.mockReturnValueOnce(new Promise<[]>(done => { resolve = done }))
    let pending!: Promise<boolean | void>
    act(() => { pending = hook.result.current.send('保留消息') })
    act(() => {
      if (changed === 'session') useSessionStore.setState({ activeSessionId: 'other-session' })
      if (changed === 'active instance') useInstanceStore.setState({ activeInstanceId: 'new-instance' })
      if (changed === 'bound instance') useChatStore.setState({ sessionRuntimeStates: { sid: { session_id: 'sid', bound_instance_id: 'new-bound-instance' } } })
      if (changed === 'execution mode') useChatStore.setState({ executionMode: 'plan' })
    })
    await act(async () => { resolve([]); expect(await pending).toBe(false) })
    expect(send).not.toHaveBeenCalled()
    hook.unmount()
  })

  it('ignores obsolete compilation events while validating the saved flow', async () => {
    const saved = { ...detail, plan: { ...detail.plan!, inputs: [], input_constraints: [] } }
    mocks.get.mockResolvedValue(saved)
    window.history.replaceState({}, '', '/chat?flow=demo')
    const send = vi.fn(async () => true)
    const hook = renderHook(() => useFlowComposer(send))
    await waitFor(() => expect(hook.result.current.blocked).toBe(false))
    let resolve!: (value: []) => void
    mocks.checkInputs.mockReturnValueOnce(new Promise<[]>(done => { resolve = done }))
    let pending!: Promise<boolean | void>
    act(() => { pending = hook.result.current.send('保留消息') })
    await act(async () => { window.dispatchEvent(new CustomEvent('autocarto:flow_compile', { detail: { flow_id: 'demo', scheme: 'default', state: 'succeeded' } })) })
    expect(mocks.get).toHaveBeenCalledTimes(1)
    expect(hook.result.current.detail).toBe(saved)
    await act(async () => { resolve([]); await pending })
    expect(send).toHaveBeenCalledOnce()
    hook.unmount()
  })

  it('initializes flow query, hides conversation text, loads bound project layers and sends typed inputs', async () => {
    window.history.replaceState({}, '', '/chat?flow=demo')
    const send = vi.fn(async () => true)
    render(<Composer send={send}/>)
    expect(await screen.findByRole('combobox', { name: '纸张（必填）' })).toHaveValue('A1')
    expect(screen.queryByText('隐藏原文')).not.toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: '源文件' })).toHaveValue('')
    expect(screen.queryByRole('option', { name: '停用流程' })).not.toBeInTheDocument()
    expect(await screen.findByRole('option', { name: '真实图层' })).toBeInTheDocument()
    expect(mocks.api).toHaveBeenCalledWith('/api/instances/bound-instance/layers')
    fireEvent.change(screen.getByRole('spinbutton', { name: '数量（必填）' }), { target: { value: '0' } })
    fireEvent.change(screen.getByRole('combobox', { name: '图层（必填）' }), { target: { value: 'real-layer' } })
    fireEvent.change(screen.getByRole('textbox', { name: '消息输入' }), { target: { value: '完整原文' } })
    await waitFor(() => expect(screen.getByRole('button', { name: '发送' })).toBeEnabled())
    fireEvent.click(screen.getByRole('button', { name: '发送' }))
    await waitFor(() => expect(send).toHaveBeenCalledWith('完整原文', { id: 'demo', inputs: { source: { $ref: 'current_user_message' }, paper: 'A1', review: false, count: 0, layer: 'real-layer' } }))
  })
  it('keeps the message when required input is missing and falls back honestly when layers fail', async () => {
    mocks.api.mockRejectedValue(new Error('offline')); mocks.desktop.mockReturnValue(true)
    const send = vi.fn(async () => true)
    render(<Composer send={send}/>)
    await screen.findByRole('option', { name: '示例流程' })
    fireEvent.change(screen.getByRole('combobox', { name: '选择流程' }), { target: { value: 'demo' } })
    expect(await screen.findByText(/图层列表读取失败/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '选择文件' }))
    await waitFor(() => expect(screen.getByRole('textbox', { name: '源文件' })).toHaveValue('/tmp/source.csv'))
    fireEvent.change(screen.getByRole('textbox', { name: '消息输入' }), { target: { value: '保留这条消息' } })
    await waitFor(() => expect(screen.getByRole('button', { name: '发送' })).toBeEnabled())
    fireEvent.click(screen.getByRole('button', { name: '发送' }))
    expect(await screen.findByText('请填写数量')).toBeInTheDocument()
    expect(send).not.toHaveBeenCalled()
    expect(screen.getByRole('textbox', { name: '消息输入' })).toHaveValue('保留这条消息')
    fireEvent.change(screen.getByRole('combobox', { name: '选择流程' }), { target: { value: '' } })
    await waitFor(() => expect(screen.getByRole('button', { name: '发送' })).toBeEnabled())
    fireEvent.click(screen.getByRole('button', { name: '发送' }))
    await waitFor(() => expect(send).toHaveBeenCalledWith('保留这条消息'))
  })
  it('retains the query while the initial scheme hydrates, then resets on a deliberate scheme change', async () => {
    window.history.replaceState({}, '', '/chat?flow=demo')
    useChatStore.setState({ activeAgentConfig: '' })
    render(<Composer send={vi.fn(async () => true)}/>)
    await screen.findByRole('combobox', { name: '纸张（必填）' })
    act(() => useChatStore.setState({ activeAgentConfig: 'hydrated-scheme' }))
    await waitFor(() => expect(mocks.get).toHaveBeenCalledWith('demo', 'hydrated-scheme'))
    expect(screen.getByRole('combobox', { name: '选择流程' })).toHaveValue('demo')
    act(() => useChatStore.setState({ activeAgentConfig: 'user-switched-scheme' }))
    await waitFor(() => expect(screen.getByRole('combobox', { name: '选择流程' })).toHaveValue(''))
  })
  it('loads saved inputs without model requests or historical compilation warnings', async () => {
    window.history.replaceState({}, '', '/chat?flow=demo')
    const partial = { ...detail, plan: { ...detail.plan!, plan_status: 'partial', enrichment: { done: 0, total: 1, failed: [{ step_id: 'one', title: '导出成果', reason: 'invalid_json' }] } } }
    mocks.get.mockResolvedValue(partial)
    render(<Composer send={vi.fn(async () => true)}/>)
    const count = await screen.findByRole('spinbutton', { name: '数量（必填）' })
    fireEvent.change(count, { target: { value: '7' } })
    expect(count).toHaveValue(7)
    expect(screen.getByText('已解析')).toBeInTheDocument()
    expect(screen.queryByText(/未完善|模型返回格式|重新编译/)).not.toBeInTheDocument()
    expect(screen.queryByText('正在读取流程…')).not.toBeInTheDocument()
    expect(mocks.compile).not.toHaveBeenCalled()
    expect(mocks.compileStatus).not.toHaveBeenCalled()
  })
  it('prefills remembered input and prevents submission when a required data file is missing', async () => {
    window.history.replaceState({}, '', '/chat?flow=demo')
    mocks.get.mockResolvedValue({ ...detail, plan: { ...detail.plan!, inputs: [{ key: '底图数据', label: '底图数据', type: 'folder', remember: true, required: true, default: '/default' }] } })
    mocks.inputMemory.mockResolvedValue({ 底图数据: '/last-choice' })
    mocks.checkInputs.mockImplementation(async (_id: string, _scheme: string, values: Record<string, unknown>) => values['底图数据'] === '/missing' ? [{ label: '底图数据', code: 'FLOW_DATA_FILE_MISSING', level: 'error', message: '缺少 base.shp' }] : [])
    const send = vi.fn(async () => true)
    render(<Composer send={send}/>)
    const input = await screen.findByRole('textbox', { name: '底图数据（必填）' })
    await waitFor(() => expect(input).toHaveValue('/last-choice'))
    fireEvent.change(input, { target: { value: '/missing' } })
    expect(await screen.findByText('缺少 base.shp')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '发送' })).toBeDisabled()
    expect(send).not.toHaveBeenCalled()
  })
  it('groups tools in their step, shows authoritative waiting state and reuses HITL choices', async () => {
    const state = run(); state.status = 'waiting_user'; state.steps[0].status = 'waiting_user'
    const approve = vi.fn()
    render(<MessageList {...baseProps} onInterruptApprove={approve} flowRuns={[state]} messages={[{ id: 'tool', type: 'tool', content: '真实工具回执', timestamp: Date.now(), phase: 'tool_result', data: { flow_run_id: 'r', step_id: 's1', tool: 'get_layers_info' } }]} pendingInterrupts={[{ checkpoint_id: 'cp', flow_run_id: 'r', step_id: 's1', interrupt_contexts: [{ is_root_cause: true, info: { type: 'decision', title: '本步骤未完成', select_mode: 'single', required: true, default: 'retry', category: 'flow_confirmation', options: [{ value: 'retry', label: '重试本步' }, { value: 'skip', label: '跳过本步' }, { value: 'stop', label: '终止流程' }] } }] }]}/>)
    const card = screen.getByRole('region', { name: '流程进度 示例流程' })
    expect(within(card).getAllByText(/等待确认/).length).toBeGreaterThan(0)
    expect(within(card).queryByText('自定义回答')).not.toBeInTheDocument()
    expect(within(card).queryByRole('button', { name: '拒绝' })).not.toBeInTheDocument()
    expect(within(card).getByText('步骤过程（1）')).toBeInTheDocument()
    fireEvent.click(within(card).getByText('步骤过程（1）'))
    expect(within(card).getByText('真实工具回执')).toBeVisible()
    fireEvent.click(within(card).getByRole('radio', { name: '跳过本步' }))
    fireEvent.change(within(card).getByPlaceholderText('可选：补充确认信息'), { target: { value: '保留已完成产出' } })
    fireEvent.click(within(card).getByRole('button', { name: '确认并继续' }))
    expect(approve).toHaveBeenCalledWith('cp', { option: 'skip', text: '保留已完成产出' })
  })
  it('reuses knowledge review inside the waiting step and supports restart continuation', async () => {
    const state = run(); state.status = 'interrupted'; state.steps[0].status = 'waiting_user'
    const show = vi.fn(), resume = vi.fn(async () => true)
    render(<MessageList {...baseProps} flowRuns={[state]} onResumeFlow={resume} messages={[]} sessionId="sid" onOpenKnowledgeReview={show} pendingInterrupts={[{ checkpoint_id: 'knowledge-cp', flow_run_id: 'r', step_id: 's1', interrupt_contexts: [{ info: { type: 'knowledge_review', review_id: 'review-1', status: 'awaiting_review' } }] }]}/>)
    fireEvent.click(screen.getByRole('button', { name: '查看并修改' }))
    expect(show).toHaveBeenCalledWith(expect.objectContaining({ review_id: 'review-1', session_id: 'sid' }))
    fireEvent.click(screen.getByRole('button', { name: '继续此流程' }))
    await waitFor(() => expect(resume).toHaveBeenCalledWith(state))
    expect(screen.getByRole('button', { name: '继续请求已发送，等待确认' })).toBeDisabled()
  })
  it('shows Plan preview using the same step renderer and keeps normal messages visible', async () => {
    const plan = { ...detail.plan!, steps: [{ id: 'one', index: 1, title: '预览检查', role_label: '数据角色', kind: 'agent', instructions: '读取数据', expected_outputs: [], on_failure: 'ask' }] }
    render(<MessageList {...baseProps} executionMode="plan" messages={[{ id: 'ordinary', type: 'assistant', content: '普通分析回复', timestamp: 1 }, { id: 'preview', type: 'status', content: '', timestamp: 2, data: { flow_preview: { execution_mode: 'plan', flow_id: 'demo', name: '示例流程', plan, diagnostics: [], input_check: { valid: false, missing: ['数据文件'] } } } }]}/>)
    expect(screen.getByText('普通分析回复')).toBeInTheDocument()
    expect(screen.getByText('1. 预览检查')).toBeInTheDocument()
    expect(screen.getByText('需要补充：数据文件')).toBeInTheDocument()
    await act(async () => {})
  })
  it.each(['build', 'plan'] as const)('does not show internal or unclassified historical flow queries in current %s mode', async mode => {
    const preview = { flow_id: 'demo', plan: detail.plan }
    render(<MessageList {...baseProps} executionMode={mode} messages={[
      { id: 'reply', type: 'assistant', content: '数据已加载', timestamp: 1 },
      { id: 'old-build', type: 'status', content: '流程预览：示例流程', timestamp: 2, data: { mode_phase: 'building', flow_preview: preview } },
      { id: 'unknown', type: 'status', content: '流程预览：示例流程', timestamp: 3, data: { flow_preview: preview } },
      { id: 'build-tool', type: 'tool', phase: 'tool_result', content: '', timestamp: 4, data: { tool: 'flow_preview', result: { ...preview, execution_mode: 'build' } } },
    ]}/> )
    expect(screen.getByText('数据已加载')).toBeVisible()
    expect(screen.queryByText('流程预览：示例流程')).not.toBeInTheDocument()
    expect(screen.queryByRole('region', { name: '流程预览 示例流程' })).not.toBeInTheDocument()
    await act(async () => {})
  })
  it('keeps a recorded Plan preview when switching mode and shows real execution as progress', async () => {
    const messages = [{ id: 'preview', type: 'status' as const, content: '', timestamp: 1, data: { flow_preview: { execution_mode: 'plan', flow_id: 'demo', plan: detail.plan } } }]
    const view = render(<MessageList {...baseProps} executionMode="plan" messages={messages}/>)
    expect(screen.getByRole('region', { name: '流程预览 示例流程' })).toBeVisible()
    view.rerender(<MessageList {...baseProps} executionMode="build" messages={messages} flowRuns={[run()]}/>)
    expect(screen.getByRole('region', { name: '流程预览 示例流程' })).toBeVisible()
    expect(screen.getByRole('region', { name: '流程进度 示例流程' })).toBeVisible()
    expect(screen.getByText('这是流程步骤与输入的预览，不代表流程已经开始执行。')).toBeVisible()
    expect(screen.queryByText(/切换到 Build/)).not.toBeInTheDocument()
    await act(async () => {})
  })
  it('opens desktop output only on click and reports a native failure', async () => {
    mocks.desktop.mockReturnValue(true); mocks.invoke.mockRejectedValue('不支持打开此类文件')
    render(<FlowOutputs outputs={{ 图件: 'outputs/map.pdf' }}/>)
    expect(mocks.invoke).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'outputs/map.pdf' }))
    await waitFor(() => expect(mocks.invoke).toHaveBeenCalledWith('open_local_path', { path: 'outputs/map.pdf' }))
    expect(await screen.findByRole('status')).toHaveTextContent('不支持打开此类文件')
  })
  it('restores a raw persisted Plan tool result as a preview and copies paths in ordinary browsers', async () => {
    const copy = vi.fn(async () => {})
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: copy } })
    render(<><MessageList {...baseProps} messages={[{ id: 'saved-tool', type: 'tool', phase: 'tool_result', timestamp: 1, content: '', data: { tool: 'flow_preview', mode_phase: 'planning', result: JSON.stringify({ flow_id: 'demo', plan: detail.plan }) } }]}/><FlowOutputs outputs={{ 图件: 'outputs/map.pdf' }}/></>)
    expect(screen.getByRole('region', { name: '流程预览 示例流程' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'outputs/map.pdf' }))
    await waitFor(() => expect(copy).toHaveBeenCalledWith('outputs/map.pdf'))
    expect(await screen.findByRole('status')).toHaveTextContent('请在桌面应用中点击打开成果')
    expect(mocks.invoke).not.toHaveBeenCalled()
  })
  it.each(['continue_snapshot', 'restart'] as const)('资源变化卡片把明确选择 %s 交给恢复操作', async choice => {
    const original = run(); original.status = 'waiting_user'
    const state = applyFlowEvent(original, { event: 'flow.resources_changed', session_id: original.session_id, flow_run_id: original.flow_run_id,
      payload: { status: 'waiting_user', resource_change: { fingerprint: 'new-fingerprint', message: '资源在暂停期间发生变化。继续使用本次运行保存的资源引用；已修改的文件内容不会恢复。' } } })!
    const resume = vi.fn(async () => true)
    render(<MessageList {...baseProps} flowRuns={[state]} onResumeFlow={resume} messages={[]}/>)
    expect(screen.getByText(/已修改的文件内容不会恢复/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: choice === 'continue_snapshot' ? '继续使用运行快照' : '使用当前资源重新开始' }))
    await waitFor(() => expect(resume).toHaveBeenCalledWith(state, choice))
    expect(screen.getByRole('button', { name: '继续使用运行快照' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '使用当前资源重新开始' })).toBeDisabled()
    expect(applyFlowEvent(state, { event: 'flow.resources_confirmed', session_id: state.session_id, flow_run_id: state.flow_run_id, payload: {} })?.resource_change).toBeNull()
  })

})
