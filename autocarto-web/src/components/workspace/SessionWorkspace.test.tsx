import React from 'react'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import SessionWorkspace, { workspaceUsesDrawer } from './SessionWorkspace'
import { useUIStore } from '@/stores/uiStore'
import { useFlowRunStore } from '@/features/plan/store'
import type { TodoItem, WorkflowBlock, WSMessage } from '@/types/message'
const { listeners, refresh } = vi.hoisted(() => ({
  listeners: new Set<(message: WSMessage) => void>(),
  refresh: vi.fn(async () => {}),
}))
vi.mock('@/lib/websocket', () => ({
  default: {
    onMessage: (fn: (message: WSMessage) => void) => {
      listeners.add(fn)
      return () => listeners.delete(fn)
    },
  },
}))
vi.mock('@/hooks/useSessionFiles', () => ({
  useSessionFilesStore: { getState: () => ({ items: {} }) },
  useSessionFiles: () => ({
    items: [],
    all: false,
    setAll: vi.fn(),
    loading: false,
    error: '',
    sessionDir: '/tmp/session',
    refresh,
    newCount: 0,
    viewedAt: 0,
  }),
}))
vi.mock('@/components/chat/TaskListPanel', () => ({
  default: ({ tasks }: { tasks: TodoItem[] }) => (
    <div>tasks:{tasks.length}</div>
  ),
}))
vi.mock('@/components/chat/AgentBlockView', () => ({
  default: ({
    block,
    collapsed,
    onToggleCollapse,
  }: {
    block: WorkflowBlock
    collapsed: boolean
    onToggleCollapse: () => void
  }) => (
    <button onClick={onToggleCollapse}>
      {block.agentDisplayName}:{collapsed ? '收起' : '展开'}
    </button>
  ),
}))
const blocks: WorkflowBlock[] = [
  {
    id: 'wf-1',
    agent: 'planner',
    agentDisplayName: '规划角色',
    summary: '处理中',
    status: 'running',
    startedAt: 1,
    updatedAt: 2,
    entries: [],
  },
]
const props = {
  sessionId: 'sid',
  projectPath: '/tmp/map.qgz',
  tasks: [
    { content: 'task', priority: 'high', status: 'pending' },
  ] as TodoItem[],
  pendingInterrupts: [],
  workflowBlocks: blocks,
  focusedWorkflowBlockId: null,
  onFocusWorkflowBlock: vi.fn(),
}
beforeEach(() => {
  listeners.clear()
  refresh.mockClear()
  useUIStore.setState({
    workspace: { open: false, tab: 'files', width: 360, previewWidth: 520 },
    inspectorOpen: false,
    filesViewedAt: {},
    sessionPanelCollapsed: false,
  })
  useFlowRunStore.setState({runs: {}, plans: {}})
})
describe('会话工作区', () => {
  it('图标栏常驻，选择页签有高亮，再次点击收起', () => {
    render(<SessionWorkspace {...props} />)
    expect(screen.queryByRole('complementary')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '工作区任务与过程' }))
    expect(screen.getByRole('tab', { name: '任务与过程' })).toHaveAttribute(
      'aria-selected',
      'true',
    )
    expect(screen.getByText('tasks:1')).toBeVisible()
    expect(screen.getByText('规划角色:展开')).toBeVisible()
    expect(screen.queryByRole('tab', { name: '过程' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '工作区任务与过程' }))
    expect(screen.queryByRole('complementary')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '工作区文件' })).toBeVisible()
  })
  it('任务与过程保留独立滚动区和手工折叠', () => {
    useUIStore.getState().openWorkspace('process')
    render(<SessionWorkspace {...props} />)
    expect(screen.getByTestId('workspace-task-region')).toBeVisible()
    expect(screen.getByTestId('workspace-process-region')).toBeVisible()
    expect(screen.getByTestId('workspace-task-scroll')).toHaveClass('max-h-[40%]', 'overflow-y-auto')
    expect(screen.getByTestId('workspace-process-region')).toHaveClass('h-full', 'overflow-y-auto')
    expect(screen.getByRole('tab', { name: '任务与过程' })).toHaveAttribute('aria-selected', 'true')
    fireEvent.click(screen.getByText('规划角色:展开'))
    expect(screen.getByText('规划角色:收起')).toBeVisible()
    fireEvent.click(screen.getByRole('button', { name: '展开全部' }))
    expect(screen.getByText('规划角色:展开')).toBeVisible()
  })
  it.each(['tasks', 'process'] as const)('旧 %s 入口打开同一个面板，文件页签保持独立', tab => {
    useUIStore.getState().openWorkspace(tab)
    render(<SessionWorkspace {...props} />)
    expect(screen.getByTestId('workspace-task-region')).toBeVisible()
    expect(screen.getByTestId('workspace-process-region')).toBeVisible()
    expect(screen.getByRole('button', { name: '工作区任务与过程' })).toHaveAttribute('aria-pressed', 'true')
    fireEvent.click(screen.getByRole('tab', { name: '文件' }))
    expect(screen.getByTestId('workspace-task-region')).not.toBeVisible()
    expect(screen.getByTestId('workspace-process-region')).not.toBeVisible()
    fireEvent.click(screen.getByRole('tab', { name: '任务与过程' }))
    expect(screen.getByTestId('workspace-task-region')).toBeVisible()
    expect(screen.getByTestId('workspace-process-region')).toBeVisible()
    fireEvent.click(screen.getByRole('button', { name: '工作区任务与过程' }))
    expect(screen.queryByRole('complementary')).not.toBeInTheDocument()
  })
  it('查看详情打开合并面板后仍展开指定的已完成过程', () => {
    useUIStore.getState().openWorkspace('process')
    render(<SessionWorkspace {...props} workflowBlocks={[{ ...blocks[0], status: 'completed' }]} focusedWorkflowBlockId="wf-1" />)
    expect(screen.getByText('tasks:1')).toBeVisible()
    expect(screen.getByText('规划角色:展开')).toBeVisible()
  })
  it('流程任务仍显示原始智能体块，不把步骤回执重新拼成过程', () => {
    useUIStore.getState().openWorkspace('process')
    useFlowRunStore.setState({runs: {sid: [{api_version: 'flow-run/v1', flow_run_id: 'run', session_id: 'sid', flow_id: '', status: 'completed', started_at: '2026-10-29T00:00:00Z',
      steps: [{id: 'step', index: 1, title: '步骤标题', role: 'data_engineer', status: 'completed', outputs: {}, attempts: [{run_id: 'attempt', started_at: '2026-10-29T00:00:00Z', receipt: {status: 'completed', summary: '额外步骤回执', outputs: {}, issues: []}}]}],
      result: {summary: '', deliverables: {}, issues: []}}]}})
    const original = {...blocks[0], status: 'completed' as const, entries: [{id: 'entry', type: 'thinking' as const, content: '原始思考内容', timestamp: Date.parse('2026-10-29T00:00:01Z'), data: {flow_run_id: 'run', step_id: 'step', run_id: 'attempt'}}]}
    render(<SessionWorkspace {...props} workflowBlocks={[original]} focusedWorkflowBlockId={original.id}/>)
    // The original id remains the focus target; a step-derived replacement
    // would lose this selection and collapse the completed block.
    expect(screen.getByText('规划角色:展开')).toBeVisible()
    expect(screen.queryByText('数据工程智能体:收起')).not.toBeInTheDocument()
  })
  it('待确认只读内容仍展示且没有确认操作', () => {
    useUIStore.getState().openWorkspace('tasks')
    render(
      <SessionWorkspace
        {...props}
        pendingInterrupts={[
          {
            checkpoint_id: 'cp',
            interrupt_contexts: [
              {
                is_root_cause: true,
                info: {
                  title: '选择路线',
                  description: '确认路线数据',
                  type: 'decision',
                  options: [{}],
                },
              },
            ],
          },
        ]}
      />,
    )
    expect(screen.getByText('选择路线')).toBeVisible()
    expect(screen.getByText(/请在主聊天区完成选择/)).toBeVisible()
    expect(
      screen.queryByRole('button', { name: '确认并继续' }),
    ).not.toBeInTheDocument()
  })
  it('关闭时流程结束不弹面板，打开时有成果才切文件', async () => {
    render(<SessionWorkspace {...props} />)
    const message = {
      type: 'flow_event',
      content: '',
      session_id: 'sid',
      data: {
        event: 'flow.finished',
        payload: { deliverables: { 地图: '/tmp/map.pdf' } },
      },
    }
    await act(async () => listeners.forEach((listener) => listener(message)))
    expect(useUIStore.getState().workspace.open).toBe(false)
    act(() => useUIStore.getState().openWorkspace('process'))
    await act(async () => listeners.forEach((listener) => listener(message)))
    await waitFor(() =>
      expect(useUIStore.getState().workspace.tab).toBe('files'),
    )
  })
  it('宽度计算保留聊天420像素；900窗口始终使用抽屉', () => {
    expect(workspaceUsesDrawer(1048, 360, 1200)).toBe(false)
    expect(workspaceUsesDrawer(748, 360, 900)).toBe(true)
    expect(workspaceUsesDrawer(700, 520, 1200)).toBe(true)
  })
  it('旧详情接口映射到工作区，保留上次页签', () => {
    useUIStore.getState().openWorkspace('process')
    useUIStore.getState().setInspectorOpen(false)
    expect(useUIStore.getState().workspace).toMatchObject({
      open: false,
      tab: 'process',
    })
    useUIStore.getState().toggleInspectorOpen()
    expect(useUIStore.getState().workspace).toMatchObject({
      open: true,
      tab: 'process',
    })
  })
})
