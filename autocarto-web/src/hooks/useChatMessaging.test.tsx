import { act, renderHook } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useChatMessaging } from '@/hooks/useChatMessaging'
import { useChatStore } from '@/stores/chatStore'
import { useConnectionStore } from '@/stores/connectionStore'
import { useInstanceStore } from '@/stores/instanceStore'
import { useSessionStore } from '@/stores/sessionStore'
import type { QGISInstance } from '@/types/instance'

const {
  websocketState,
  sendInterruptResponseMock,
  sendCancelMock,
  sendChatMock,
  waitUntilConnectedMock,
} = vi.hoisted(() => ({
  websocketState: { connected: true },
  sendInterruptResponseMock: vi.fn(),
  sendCancelMock: vi.fn(),
  sendChatMock: vi.fn(),
  waitUntilConnectedMock: vi.fn(),
}))

vi.mock('@/lib/websocket', () => ({
  wsClient: {
    get connected() {
      return websocketState.connected
    },
    sendInterruptResponse: sendInterruptResponseMock,
    sendCancel: sendCancelMock,
    sendChat: sendChatMock,
    waitUntilConnected: waitUntilConnectedMock,
  },
}))

const runningInstance: QGISInstance = {
  id: 'inst-hitl',
  pid: 1234,
  qgis_version: '3.34.0',
  project_path: '/tmp/hitl-test.qgz',
  status: 'running',
  services: {},
  created_at: '2026-03-22T00:00:00Z',
  updated_at: '2026-03-22T00:00:00Z',
  last_heartbeat: '2026-03-22T00:00:00Z',
  source: 'register',
}

function resetStores() {
  const pending = [{ checkpoint_id: 'cp-1', interrupt_contexts: [] }]

  useChatStore.setState({
    sessionId: 'sid-hitl',
    messages: [],
    sessionMessages: { 'sid-hitl': [] },
    isThinking: false,
    isRunning: true,
    runningSessionIds: ['sid-hitl'],
    sessionRuntimeStates: {},
    sessionThinking: {},
    sessionExecutionMode: {},
    sessionThinkingEnabled: {},
    sessionActiveModelName: {},
    executionMode: 'build',
    executionModeTracked: true,
    autoPhase: 'idle',
    thinkingEnabled: false,
    activeModelName: '',
    activeAgentConfig: '',
    availableModels: [],
    availableAgentConfigs: [],
    tasks: [],
    sessionTasks: { 'sid-hitl': [] },
    pendingInterrupts: pending,
    sessionPendingInterrupts: { 'sid-hitl': pending },
    workflowBlocks: [],
    sessionWorkflowBlocks: {},
    activeWorkflowBlockId: null,
    sessionActiveWorkflowBlockId: {},
    focusedWorkflowBlockId: null,
    sessionFocusedWorkflowBlockId: {},
    inspectorOpen: false,
    contextStats: {
      messageCount: 0,
      totalChars: 0,
      estimatedTokens: 0,
      updatedAt: 0,
    },
    lastContextResponse: null,
  })

  useSessionStore.setState({
    sessions: [],
    sessionsReady: true,
    activeSessionId: 'sid-hitl',
    sessionPanelCollapsed: false,
    sessionMutationTick: 0,
    lastMutatedSessionId: null,
    sessionAttachState: {},
  })

  useConnectionStore.setState({
    connected: true,
    backendReady: true,
    backendUrl: 'http://127.0.0.1:3000',
    wsUrl: 'ws://127.0.0.1:3000/ws',
    startupError: '',
    servicePorts: {},
  })

  useInstanceStore.setState({
    instances: [runningInstance],
    activeInstanceId: runningInstance.id,
    qgisAvailable: true,
    projectConnected: true,
    instanceCount: 1,
    runningCount: 1,
    stale: false,
    loading: false,
  })
}

describe('useChatMessaging HITL 交互流', () => {
  beforeEach(() => {
    resetStores()
    websocketState.connected = true
    sendInterruptResponseMock.mockReset()
    sendCancelMock.mockReset()
    sendChatMock.mockReset()
    waitUntilConnectedMock.mockReset()
  })

  it('确认中断会发送 websocket 响应并更新本地状态', () => {
    sendInterruptResponseMock.mockReturnValue(true)
    const ensureSession = vi.fn(async () => 'sid-hitl')
    const { result } = renderHook(() => useChatMessaging({ ensureSession }))

    act(() => {
      result.current.handleInterruptApprove('cp-1', { option: '继续执行' })
    })

    expect(sendInterruptResponseMock).toHaveBeenCalledWith({
      checkpointId: 'cp-1',
      approved: true,
      userData: { option: '继续执行' },
      sessionId: 'sid-hitl',
    })

    expect(useChatStore.getState().pendingInterrupts).toEqual([])
    expect(result.current.resolvedInterruptStates['cp-1']).toBe('approved')

    const decisionMessage = useChatStore.getState().messages.at(-1)
    expect(decisionMessage?.type).toBe('decision')
    expect(decisionMessage?.content).toContain('决策结果（cp-1）')
  })

  it('checkpoint 为空时会回退到当前 pending interrupt', () => {
    sendInterruptResponseMock.mockReturnValue(true)
    const ensureSession = vi.fn(async () => 'sid-hitl')
    const { result } = renderHook(() => useChatMessaging({ ensureSession }))

    act(() => {
      result.current.handleInterruptReject('')
    })

    expect(sendInterruptResponseMock).toHaveBeenCalledWith({
      checkpointId: 'cp-1',
      approved: false,
      sessionId: 'sid-hitl',
    })
    expect(result.current.resolvedInterruptStates['cp-1']).toBe('rejected')
  })

  it('发送失败时保留 pending interrupt 并写入错误消息', () => {
    sendInterruptResponseMock.mockReturnValue(false)
    const ensureSession = vi.fn(async () => 'sid-hitl')
    const { result } = renderHook(() => useChatMessaging({ ensureSession }))

    act(() => {
      result.current.handleInterruptApprove('cp-1', { option: '继续执行' })
    })

    expect(useChatStore.getState().pendingInterrupts).toHaveLength(1)
    const errorMessage = useChatStore.getState().messages.at(-1)
    expect(errorMessage?.type).toBe('error')
    expect(errorMessage?.content).toContain('中断确认发送失败')
  })

  it('同一 checkpoint 再次进入 pending 时保留 resolved 状态，避免卡片复活', () => {
    sendInterruptResponseMock.mockReturnValue(true)
    const ensureSession = vi.fn(async () => 'sid-hitl')
    const { result } = renderHook(() => useChatMessaging({ ensureSession }))

    act(() => {
      result.current.handleInterruptApprove('cp-1', { option: '继续执行' })
    })

    expect(result.current.resolvedInterruptStates['cp-1']).toBe('approved')

    act(() => {
      useChatStore.setState((state) => ({
        pendingInterrupts: [{ checkpoint_id: 'cp-1', interrupt_contexts: [] }],
        sessionPendingInterrupts: {
          ...state.sessionPendingInterrupts,
          'sid-hitl': [{ checkpoint_id: 'cp-1', interrupt_contexts: [] }],
        },
      }))
    })

    expect(result.current.resolvedInterruptStates['cp-1']).toBe('approved')
  })

  it('WebSocket 尚未 ready 时会等待连接恢复后再发送消息', async () => {
    websocketState.connected = false
    waitUntilConnectedMock.mockImplementation(async () => {
      websocketState.connected = true
      return true
    })
    sendChatMock.mockReturnValue(true)
    const ensureSession = vi.fn(async () => 'sid-hitl')
    const { result } = renderHook(() => useChatMessaging({ ensureSession }))

    await act(async () => {
      await result.current.handleSend('请检查项目路径')
    })

    expect(waitUntilConnectedMock).toHaveBeenCalled()
    expect(ensureSession).toHaveBeenCalled()
    expect(sendChatMock).toHaveBeenCalledWith(
      '请检查项目路径',
      'sid-hitl',
      expect.objectContaining({
        project_path: '/tmp/hitl-test.qgz',
        instance_id: 'inst-hitl',
      }),
    )
    expect(useChatStore.getState().messages.at(-1)?.type).toBe('user')
  })

  it('等待连接超时后写入恢复中错误，不创建新会话', async () => {
    websocketState.connected = false
    waitUntilConnectedMock.mockResolvedValue(false)
    const ensureSession = vi.fn(async () => 'sid-hitl')
    const { result } = renderHook(() => useChatMessaging({ ensureSession }))

    await act(async () => {
      await result.current.handleSend('请检查项目路径')
    })

    expect(ensureSession).not.toHaveBeenCalled()
    expect(sendChatMock).not.toHaveBeenCalled()
    const errorMessage = useChatStore.getState().messages.at(-1)
    expect(errorMessage?.type).toBe('error')
    expect(errorMessage?.content).toContain('当前实时连接正在恢复')
  })
  it('取消运行会使用当前会话发送 websocket cancel 请求', () => {
    sendCancelMock.mockReturnValue(true)
    const ensureSession = vi.fn(async () => 'sid-hitl')
    const { result } = renderHook(() => useChatMessaging({ ensureSession }))

    act(() => {
      result.current.handleCancel()
    })

    expect(sendCancelMock).toHaveBeenCalledWith('sid-hitl')
    expect(useChatStore.getState().isThinking).toBe(false)
  })
})
