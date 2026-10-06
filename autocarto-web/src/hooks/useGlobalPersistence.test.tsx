import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { type SessionRecord } from '@/lib/api'
import {
  getGlobalPersistenceController,
  useGlobalPersistence,
} from '@/hooks/useGlobalPersistence'
import { useChatStore } from '@/stores/chatStore'
import type { Message } from '@/types/message'

const { getSessionMock, saveSessionMock } = vi.hoisted(() => ({
  getSessionMock: vi.fn(),
  saveSessionMock: vi.fn(),
}))

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    default: {
      ...actual.default,
      getSession: getSessionMock,
      saveSession: saveSessionMock,
    },
  }
})

function buildSessionRecord(overrides: Partial<SessionRecord> = {}): SessionRecord {
  return {
    session_id: 'sid-test',
    name: '测试会话',
    created_at: '2026-04-11T00:00:00.000Z',
    updated_at: '2026-04-11T00:00:00.000Z',
    project_path: '/tmp/demo.qgz',
    execution_mode: 'build',
    run_intent: 'execute',
    messages: [],
    workflow_blocks: [],
    ...overrides,
  }
}

function resetChatStore() {
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
}

describe('useGlobalPersistence', () => {
  beforeEach(() => {
    resetChatStore()
    getSessionMock.mockReset()
    saveSessionMock.mockReset()
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.clearAllMocks()
  })

  it('keeps local pending messages when session rehydrate only confirms server-backed history', async () => {
    const sessionId = 'sid-live'
    const projectPath = '/tmp/demo.qgz'
    const serverSession = buildSessionRecord({
      session_id: sessionId,
      project_path: projectPath,
      messages: [],
    })
    const userMessage: Message = {
      id: 'msg-user-local',
      type: 'user',
      content: '制作湖北省GDP条形统计专题图',
      timestamp: Date.parse('2026-04-11T00:00:01.000Z'),
    }
    const errorMessage: Message = {
      id: 'msg-error-local',
      type: 'error',
      content: 'Build 模式不可用：智能体未初始化。',
      timestamp: Date.parse('2026-04-11T00:00:02.000Z'),
    }

    getSessionMock.mockResolvedValue(serverSession)
    saveSessionMock.mockResolvedValue(undefined)

    const { unmount } = renderHook(() => useGlobalPersistence(projectPath))

    act(() => {
      useChatStore.setState({
        sessionMessages: {
          [sessionId]: [userMessage],
        },
      })
    })

    act(() => {
      getGlobalPersistenceController().markSessionLoaded(sessionId, {
        messages: [userMessage],
        persistedMessages: [],
        workflowBlocks: [],
        sessionRecord: serverSession,
      })
    })

    act(() => {
      useChatStore.setState({
        sessionMessages: {
          [sessionId]: [userMessage, errorMessage],
        },
      })
    })

    await act(async () => {
      vi.advanceTimersByTime(500)
      await Promise.resolve()
      await Promise.resolve()
    })

    expect(getSessionMock).toHaveBeenCalledWith(sessionId, projectPath)
    expect(saveSessionMock).toHaveBeenCalledTimes(1)
    expect(saveSessionMock).toHaveBeenCalledWith(
      sessionId,
      expect.objectContaining({
        session_id: sessionId,
        messages: [
          expect.objectContaining({
            role: 'user',
            msg_type: 'user',
            content: userMessage.content,
          }),
          expect.objectContaining({
            role: 'system',
            msg_type: 'error',
            content: errorMessage.content,
          }),
        ],
      }),
      projectPath,
    )

    unmount()
  })
})
