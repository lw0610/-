import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { SessionRecord } from '@/lib/api'
import { useChatSession } from '@/hooks/useChatSession'
import { useChatStore } from '@/stores/chatStore'
import { useInstanceStore } from '@/stores/instanceStore'
import { useSessionStore } from '@/stores/sessionStore'
import { useUIStore } from '@/stores/uiStore'
import type { WorkflowBlock } from '@/types/message'

const {
  getSessionMock,
  clearSessionMock,
  flushAllMock,
  flushSessionMock,
  markSessionDeletedMock,
  markSessionLoadedMock,
} = vi.hoisted(() => ({
  getSessionMock: vi.fn(),
  markSessionLoadedMock: vi.fn(),
  markSessionDeletedMock: vi.fn(),
  clearSessionMock: vi.fn(),
  flushSessionMock: vi.fn(async () => {}),
  flushAllMock: vi.fn(async () => {}),
}))

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    default: {
      ...actual.default,
      getSession: getSessionMock,
    },
  }
})

vi.mock('@/hooks/useGlobalPersistence', () => ({
  getGlobalPersistenceController: () => ({
    markSessionLoaded: markSessionLoadedMock,
    markSessionDeleted: markSessionDeletedMock,
    clearSession: clearSessionMock,
    flushSession: flushSessionMock,
    flushAll: flushAllMock,
  }),
}))

function buildSessionRecord(overrides: Partial<SessionRecord> = {}): SessionRecord {
  return {
    session_id: 'sid-test',
    name: '测试会话',
    created_at: '2026-03-19T10:00:00.000Z',
    updated_at: '2026-03-19T10:10:00.000Z',
    execution_mode: 'build',
    run_intent: 'execute',
    messages: [],
    workflow_blocks: [],
    ...overrides,
  }
}

function resetStores() {
  useChatStore.setState({
    sessionId: null,
    messages: [],
    sessionMessages: {},
    isThinking: false,
    isRunning: false,
    runningSessionIds: [],
    sessionRuntimeStates: {},
    sessionThinking: {},
    sessionExecutionMode: {},
		sessionThinkingEnabled: {},
		sessionActiveModelName: {},
		executionMode: 'build',
    executionModeTracked: false,
    autoPhase: 'idle',
    thinkingEnabled: false,
    activeModelName: '',
    activeAgentConfig: '',
    availableModels: [],
    availableAgentConfigs: [],
    tasks: [],
    sessionTasks: {},
    pendingInterrupts: [],
    sessionPendingInterrupts: {},
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
    sessionsReady: false,
    activeSessionId: null,
    sessionMutationTick: 0,
    lastMutatedSessionId: null,
    sessionAttachState: {},
  })

  useInstanceStore.setState({
    instances: [],
    activeInstanceId: null,
    qgisAvailable: false,
    projectConnected: false,
    instanceCount: 0,
    runningCount: 0,
    stale: false,
    loading: false,
  })

  useUIStore.setState({
    inspectorOpen: false,
    focusedWorkflowBlockId: null,
    sessionPanelCollapsed: false,
    displaySessionId: null,
  })

  window.localStorage.clear()
}

describe('useChatSession.loadSessionMessages', () => {
  beforeEach(() => {
    resetStores()
    getSessionMock.mockReset()
    markSessionLoadedMock.mockClear()
    markSessionDeletedMock.mockClear()
    clearSessionMock.mockClear()
    flushSessionMock.mockClear()
    flushAllMock.mockClear()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('历史会话恢复时不会把非运行态缓存消息再合并回去', async () => {
    const sessionId = 'sid-history'
    const serverWorkflow = {
      id: 'wf-server',
      agent: 'planner',
      agentDisplayName: 'Planner',
      summary: '分析需求',
      status: 'completed',
      startedAt: Date.parse('2026-03-19T10:00:05.000Z'),
      updatedAt: Date.parse('2026-03-19T10:00:08.000Z'),
      entries: [],
    }

    useSessionStore.setState({ activeSessionId: sessionId })

    useChatStore.setState({
      sessionMessages: {
        [sessionId]: [
          {
            id: 'cached-user',
            type: 'user',
            content: '制作湖北省 GDP 条形统计专题图',
            timestamp: Date.parse('2026-03-19T10:00:00.321Z'),
          },
        ],
      },
      sessionWorkflowBlocks: {
        [sessionId]: [
          {
            id: 'wf-cached',
            agent: 'planner',
            agentDisplayName: 'Planner',
            summary: '不应使用的本地卡片',
            status: 'running',
            startedAt: Date.parse('2026-03-19T10:00:04.000Z'),
            updatedAt: Date.parse('2026-03-19T10:00:09.000Z'),
            entries: [],
          },
        ],
      },
    })

    getSessionMock.mockResolvedValueOnce(buildSessionRecord({
      session_id: sessionId,
      messages: [
        {
          role: 'user',
          msg_type: 'user',
          content: '制作湖北省 GDP 条形统计专题图',
          timestamp: '2026-03-19T10:00:00.000Z',
        },
        {
          role: 'assistant',
          msg_type: 'assistant',
          content: '好的，开始分析。',
          timestamp: '2026-03-19T10:00:02.000Z',
        },
      ],
      workflow_blocks: [serverWorkflow],
    }))

    const { result } = renderHook(() => useChatSession())

    await act(async () => {
      await result.current.loadSessionMessages(sessionId)
    })

    const state = useChatStore.getState()
    expect(state.messages.map((item) => item.content)).toEqual([
      '制作湖北省 GDP 条形统计专题图',
      '好的，开始分析。',
    ])
    expect(state.messages.filter((item) => item.type === 'user')).toHaveLength(1)
    expect(state.workflowBlocks.map((item) => item.id)).toEqual(['wf-server'])
    expect(markSessionLoadedMock).toHaveBeenCalledWith(
      sessionId,
      expect.objectContaining({
        messages: expect.arrayContaining([
          expect.objectContaining({ content: '制作湖北省 GDP 条形统计专题图' }),
          expect.objectContaining({ content: '好的，开始分析。' }),
        ]),
        workflowBlocks: expect.arrayContaining([
          expect.objectContaining({ id: 'wf-server' }),
        ]),
      }),
    )
  })

  it('运行态会话恢复时继续合并实时消息并优先使用缓存 workflow block', async () => {
    const sessionId = 'sid-live'
    const cachedWorkflowBlocks: WorkflowBlock[] = [
      {
        id: 'wf-live',
        agent: 'builder',
        agentDisplayName: 'Builder',
        summary: '实时工具卡片',
        status: 'running',
        startedAt: Date.parse('2026-03-19T10:10:01.000Z'),
        updatedAt: Date.parse('2026-03-19T10:10:05.000Z'),
        entries: [],
      },
    ]

    useSessionStore.setState({ activeSessionId: sessionId })

    useChatStore.setState({
      sessionMessages: {
        [sessionId]: [
          {
            id: 'cached-status',
            type: 'status',
            content: '实时流更新',
            timestamp: Date.parse('2026-03-19T10:10:02.000Z'),
          },
        ],
      },
      sessionWorkflowBlocks: {
        [sessionId]: cachedWorkflowBlocks,
      },
      runningSessionIds: [sessionId],
    })

    getSessionMock.mockResolvedValueOnce(buildSessionRecord({
      session_id: sessionId,
      messages: [
        {
          role: 'user',
          msg_type: 'user',
          content: '继续执行任务',
          timestamp: '2026-03-19T10:10:00.000Z',
        },
        {
          role: 'assistant',
          msg_type: 'agent',
          content: '正在调用工具',
          timestamp: '2026-03-19T10:10:01.000Z',
          agent: 'builder',
          agent_display_name: 'Builder',
          workflow_block_id: 'wf-live',
        },
      ],
      workflow_blocks: [
        {
          id: 'wf-server-stale',
          agent: 'builder',
          agentDisplayName: 'Builder',
          summary: '旧快照卡片',
          status: 'completed',
          startedAt: Date.parse('2026-03-19T10:10:01.000Z'),
          updatedAt: Date.parse('2026-03-19T10:10:03.000Z'),
          entries: [],
        },
      ],
    }))

    const { result } = renderHook(() => useChatSession())

    await act(async () => {
      await result.current.loadSessionMessages(sessionId)
    })

    const state = useChatStore.getState()
    expect(state.messages.map((item) => item.content)).toEqual([
      '继续执行任务',
      '正在调用工具',
      '实时流更新',
    ])
    expect(state.workflowBlocks.map((item) => item.id)).toEqual(['wf-live'])
    expect(state.activeWorkflowBlockId).toBe('wf-live')
    expect(useUIStore.getState().focusedWorkflowBlockId).toBe('wf-live')
    expect(markSessionLoadedMock).toHaveBeenCalledWith(
      sessionId,
      expect.objectContaining({
        workflowBlocks: expect.arrayContaining([
          expect.objectContaining({ id: 'wf-live', status: 'running' }),
        ]),
      }),
    )
  })

  it('恢复历史会话时会从会话数据中恢复任务列表', async () => {
    const sessionId = 'sid-tasks'
    useSessionStore.setState({ activeSessionId: sessionId })

    getSessionMock.mockResolvedValueOnce(buildSessionRecord({
      session_id: sessionId,
      tasks: [
        {
          content: '修复任务列表恢复',
          status: 'in_progress',
          priority: 'high',
        },
        {
          content: '验证 write_todos 卡片完成态',
          status: 'completed',
        },
      ],
    }))

    const { result } = renderHook(() => useChatSession())

    await act(async () => {
      await result.current.loadSessionMessages(sessionId)
    })

    const state = useChatStore.getState()
    expect(state.tasks).toEqual([
      expect.objectContaining({ content: '修复任务列表恢复', status: 'in_progress', priority: 'high' }),
      expect.objectContaining({ content: '验证 write_todos 卡片完成态', status: 'completed' }),
    ])
  })
})
