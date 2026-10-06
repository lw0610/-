import React from 'react'
import { render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import ChatPanel from '../ChatPanel'

type ChatMessagingState = {
  activeInterrupt: null
  activeInterruptCheckpointId: string
  connected: boolean
  handleCancel: ReturnType<typeof vi.fn>
  handleInterruptApprove: ReturnType<typeof vi.fn>
  handleInterruptReject: ReturnType<typeof vi.fn>
  handleSend: ReturnType<typeof vi.fn>
  interruptSubmitting: string
  isRunning: boolean
  isThinking: boolean
  messages: Array<{ id?: string; type: string; content: string }>
  pendingInterrupts: unknown[]
  projectConnected: boolean
  resolvedInterruptStates: Record<string, 'approved' | 'rejected'>
  tasks: Array<{ content: string; status: string }>
  workflowBlocks: Array<{ id: string; summary: string }>
}

const chatMessagingState: ChatMessagingState = {
  activeInterrupt: null,
  activeInterruptCheckpointId: '',
  connected: true,
  handleCancel: vi.fn(),
  handleInterruptApprove: vi.fn(),
  handleInterruptReject: vi.fn(),
  handleSend: vi.fn(),
  interruptSubmitting: '',
  isRunning: false,
  isThinking: false,
  messages: [{ id: 'm-1', type: 'assistant', content: '历史消息' }],
  pendingInterrupts: [],
  projectConnected: true,
  resolvedInterruptStates: {},
  tasks: [{ content: '观察运行中任务', status: 'in_progress' }],
  workflowBlocks: [{ id: 'wf-1', summary: '已恢复工作流' }],
}

const instanceState: {
  activeInstanceId: string | null
  instances: Array<{
    id: string
    project_path: string
    status: string
  }>
  runningCount: number
  stale: boolean
} = {
  activeInstanceId: 'instance-a',
  instances: [
    {
      id: 'instance-a',
      project_path: '/tmp/demo.qgz',
      status: 'running',
    },
  ],
  runningCount: 1,
  stale: false,
}

const sessionState = {
  loadSessionSettings: vi.fn(),
  ensureSession: vi.fn(async () => 'sid-demo'),
  createSnapshotForSession: vi.fn(),
  sessionSettings: null,
  applySessionRecord: vi.fn(),
  sessionRef: { current: null },
  focusedWorkflowBlockId: null,
  handleOpenWorkflowBlock: vi.fn(),
  inspectorOpen: false,
  setFocusedWorkflowBlockId: vi.fn(),
  handleCreateSnapshotQuick: vi.fn(),
  handleRestoreSnapshotQuick: vi.fn(),
  recentSnapshots: [],
  confirmDialog: {
    open: false,
    title: '',
    description: '',
    confirmText: '',
    danger: false,
  },
  confirmingAction: false,
  runConfirmDialogAction: vi.fn(async () => {}),
  closeConfirmDialog: vi.fn(),
}

const contextState = {
  executionMode: 'build' as const,
  contextStats: {
    messageCount: 1,
    estimatedTokens: 42,
    totalChars: 12,
  },
  thinkingEnabled: false,
  handleExecutionModeChange: vi.fn(),
  handleThinkingToggle: vi.fn(),
  handleCompressContext: vi.fn(),
}

const runtimeConfigState = {
  planUnavailableReason: '',
  planEnabled: true,
  planIssueMessage: '',
  activeModelName: 'demo-model',
  activeAgentConfig: 'demo-agent',
  availableModels: [],
  availableAgentConfigs: [],
  handleModelSwitch: vi.fn(),
  handleAgentConfigSwitch: vi.fn(),
  loadRuntimeConfig: vi.fn(),
  modelLoadError: '',
  agentConfigLoadError: '',
}

vi.mock('../ChatInput', () => ({
  __esModule: true,
  default: ({ disabled, running }: { disabled?: boolean; running?: boolean }) => (
    <div data-testid="chat-input" data-disabled={String(Boolean(disabled))} data-running={String(Boolean(running))} />
  ),
}))

vi.mock('../ContextToolbar', () => ({
  __esModule: true,
  default: ({ disabled, running }: { disabled?: boolean; running?: boolean }) => (
    <div data-testid="context-toolbar" data-disabled={String(Boolean(disabled))} data-running={String(Boolean(running))} />
  ),
}))

vi.mock('../InterruptCard', () => ({
  __esModule: true,
  default: () => <div data-testid="interrupt-card" />,
}))

vi.mock('../MessageList', () => ({
  __esModule: true,
  default: ({ messages, tasks, workflowBlocks }: { messages: unknown[]; tasks?: unknown[]; workflowBlocks?: unknown[] }) => (
    <div data-testid="message-list">
      messages:{messages.length}
      tasks:{Array.isArray(tasks) ? tasks.length : 'na'}
      workflow:{Array.isArray(workflowBlocks) ? workflowBlocks.length : 'na'}
    </div>
  ),
}))

vi.mock('../RightInspector', () => ({
  __esModule: true,
  default: () => <div data-testid="right-inspector" />,
}))

vi.mock('../ThinkingIndicator', () => ({
  __esModule: true,
  default: () => <div data-testid="thinking-indicator" />,
}))

vi.mock('@/components/common/ConfirmDialog', () => ({
  __esModule: true,
  default: () => null,
}))

vi.mock('@/hooks/useChatSession', () => ({
  useChatSession: () => sessionState,
}))

vi.mock('@/hooks/useChatContext', () => ({
  useChatContext: () => contextState,
}))

vi.mock('@/hooks/useChatRuntimeConfig', () => ({
  useChatRuntimeConfig: () => runtimeConfigState,
}))

vi.mock('@/hooks/useChatMessaging', () => ({
  useChatMessaging: () => chatMessagingState,
}))

vi.mock('@/stores/instanceStore', () => ({
  useInstanceStore: (selector: (state: typeof instanceState) => unknown) => selector(instanceState),
}))

describe('ChatPanel degraded runtime state', () => {
  beforeEach(() => {
    chatMessagingState.connected = true
    chatMessagingState.projectConnected = true
    chatMessagingState.isRunning = false
    chatMessagingState.isThinking = false
    chatMessagingState.messages = [{ id: 'm-1', type: 'assistant', content: '历史消息' }]
    chatMessagingState.tasks = [{ content: '观察运行中任务', status: 'in_progress' }]
    chatMessagingState.workflowBlocks = [{ id: 'wf-1', summary: '已恢复工作流' }]
    instanceState.activeInstanceId = 'instance-a'
    instanceState.instances = [{ id: 'instance-a', project_path: '/tmp/demo.qgz', status: 'running' }]
    instanceState.runningCount = 1
    instanceState.stale = false
  })

  it('在后端断开时展示 backend 状态 banner，并保留消息区域可观察', () => {
    chatMessagingState.connected = false
    chatMessagingState.projectConnected = false

    render(<ChatPanel />)

    expect(screen.getByTestId('backend-status-banner')).toHaveTextContent('后端连接中断')
    expect(screen.getByTestId('backend-status-banner')).toHaveTextContent('历史会话与运行状态暂时保持只读')
    expect(screen.queryByTestId('project-capability-banner')).not.toBeInTheDocument()
    expect(screen.getByTestId('message-list')).toBeInTheDocument()
    expect(screen.getByTestId('chat-input')).toHaveAttribute('data-disabled', 'true')
  })

  it('在运行时同步中展示 project capability banner，而不是隐藏聊天工作区', () => {
    chatMessagingState.projectConnected = false
    instanceState.stale = true
    instanceState.activeInstanceId = null
    instanceState.instances = []
    instanceState.runningCount = 0

    render(<ChatPanel />)

    expect(screen.getByTestId('project-capability-banner')).toHaveTextContent('运行时同步中')
    expect(screen.getByTestId('project-capability-banner')).toHaveTextContent('历史会话、任务和工作流仍可查看')
    expect(screen.getByTestId('project-capability-banner')).toHaveTextContent('新的地图执行请求暂时不可发送')
    expect(screen.getByTestId('message-list')).toBeInTheDocument()
    expect(screen.getByTestId('context-toolbar')).toHaveAttribute('data-disabled', 'true')
  })

  it('在项目能力缺失但后端在线时给出明确阻塞原因', () => {
    chatMessagingState.projectConnected = false
    instanceState.instances = [{ id: 'instance-a', project_path: '', status: 'running' }]

    render(<ChatPanel />)

    expect(screen.getByTestId('project-capability-banner')).toHaveTextContent('QGIS 项目能力暂不可用')
    expect(screen.getByTestId('project-capability-banner')).toHaveTextContent('可继续查看历史会话')
    expect(screen.getByTestId('project-capability-banner')).toHaveTextContent('请先打开或重新连接 QGIS 项目后再发送新消息')
  })
})
