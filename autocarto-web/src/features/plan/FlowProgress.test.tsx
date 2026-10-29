import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import MessageList from '@/components/chat/MessageList'
import FlowOutputs from './FlowOutputs'
import { applyFlowEvent } from './model'
import type { FlowRunState } from './types'
const mocks = vi.hoisted(() => ({ capabilities: vi.fn(), desktop: vi.fn(), invoke: vi.fn() }))
vi.mock('@/features/guides/api', () => ({ guideApi: mocks }))
vi.mock('@/lib/config-form-helpers', () => ({ isDesktopRuntime: mocks.desktop, pickFilePath: vi.fn() }))
vi.mock('@tauri-apps/api/core', () => ({ invoke: mocks.invoke }))
const baseProps = { workflowBlocks: [], focusedWorkflowBlockId: null, onOpenWorkflowBlock: vi.fn(), pendingInterrupts: [], interruptSubmitting: '', interruptResolvedStates: {}, onInterruptApprove: vi.fn(), onInterruptReject: vi.fn() }
function run(): FlowRunState {
  return applyFlowEvent(undefined, { event: 'flow.started', session_id: 'sid', flow_run_id: 'r', payload: { flow_id: 'demo', name: '示例流程', steps: [{ id: 's1', index: 1, title: '检查数据', role_label: '数据角色' }] } })!
}
beforeEach(() => { vi.clearAllMocks(); mocks.capabilities.mockResolvedValue([]); mocks.desktop.mockReturnValue(false) })
describe('execution progress and outputs', () => {
  it('keeps a detail entry in chat and retains authoritative waiting state and HITL choices', async () => {
    const state = run(); state.carto_plan={api_version:'carto-plan/v1',plan_id:'p',version:1,goal:'示例流程',facts:[],assumptions:[],data:[],steps:[],deliverables:[],checks:[]}; state.status = 'waiting_user'; state.steps[0].status = 'waiting_user'
    const approve = vi.fn()
    render(<MessageList {...baseProps} onInterruptApprove={approve} flowRuns={[state]} messages={[{ id: 'tool', type: 'tool', content: '真实工具回执', timestamp: Date.now(), phase: 'tool_result', data: { flow_run_id: 'r', step_id: 's1', tool: 'get_layers_info' } }]} pendingInterrupts={[{ checkpoint_id: 'cp', flow_run_id: 'r', step_id: 's1', interrupt_contexts: [{ is_root_cause: true, info: { type: 'decision', title: '本步骤未完成', select_mode: 'single', required: true, default: 'retry', category: 'flow_confirmation', options: [{ value: 'retry', label: '重试本步' }, { value: 'skip', label: '跳过本步' }, { value: 'stop', label: '终止流程' }] } }] }]}/>)
    const card = screen.getByRole('region', { name: '流程进度 示例流程' })
    expect(within(card).getAllByText(/等待确认/).length).toBeGreaterThan(0)
    expect(within(card).queryByText('自定义回答')).not.toBeInTheDocument()
    expect(within(card).queryByRole('button', { name: '拒绝' })).not.toBeInTheDocument()
    expect(within(card).queryByText('真实工具回执')).not.toBeInTheDocument()
    fireEvent.click(within(card).getByRole('button', { name: '查看详情' }))
    expect(baseProps.onOpenWorkflowBlock).toHaveBeenCalledWith('execution-r-s1-0')
    fireEvent.click(within(card).getByRole('radio', { name: '跳过本步' }))
    fireEvent.change(within(card).getByPlaceholderText('可选：补充确认信息'), { target: { value: '保留已完成产出' } })
    fireEvent.click(within(card).getByRole('button', { name: '确认并继续' }))
    expect(approve).toHaveBeenCalledWith('cp', { option: 'skip', text: '保留已完成产出' })
  })
  it('keeps legacy runs read only', () => {
    const state = run(); state.status = 'interrupted'
    render(<MessageList {...baseProps} flowRuns={[state]} onResumeFlow={vi.fn()} messages={[]} />)
    expect(screen.getByText('旧版运行记录（只读）')).toBeInTheDocument()
    expect(screen.queryByText('继续此流程')).not.toBeInTheDocument()
  })
  it('opens desktop output only on click and reports a native failure', async () => {
    mocks.desktop.mockReturnValue(true); mocks.invoke.mockRejectedValue('不支持打开此类文件')
    render(<FlowOutputs outputs={{ 图件: 'outputs/map.pdf' }}/>)
    expect(mocks.invoke).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'outputs/map.pdf' }))
    await waitFor(() => expect(mocks.invoke).toHaveBeenCalledWith('open_local_path', { path: 'outputs/map.pdf' }))
    expect(await screen.findByRole('status')).toHaveTextContent('不支持打开此类文件')
  })

})
