import { act, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import WebSocketProvider from '../WebSocketProvider'
import api, { EMPTY_RUNTIME_SNAPSHOT } from '@/lib/api'
import { useChatStore } from '@/stores/chatStore'
import { useConnectionStore } from '@/stores/connectionStore'
import { useInstanceStore } from '@/stores/instanceStore'
import { useRuntimeSnapshotStore } from '@/stores/runtimeSnapshotStore'
import { useSessionStore } from '@/stores/sessionStore'
import { useSessionViewStore } from '@/stores/sessionViewStore'
import { useUIStore } from '@/stores/uiStore'

const websocketHarness = vi.hoisted(() => {
  const messageHandlers = new Set<(msg: Record<string, unknown>) => void>()
  const connectionHandlers = new Set<(connected: boolean) => void>()
  const state = { connected: false }

  const connectMock = vi.fn()
  const disconnectMock = vi.fn(() => {
    state.connected = false
  })
  const setUrlMock = vi.fn()
  const sendSessionAttachMock = vi.fn(() => true)
  const sendSessionDetachMock = vi.fn(() => true)

  return {
    connectMock,
    disconnectMock,
    setUrlMock,
    sendSessionAttachMock,
    sendSessionDetachMock,
    reset() {
      state.connected = false
      messageHandlers.clear()
      connectionHandlers.clear()
      connectMock.mockReset()
      disconnectMock.mockClear()
      setUrlMock.mockReset()
      sendSessionAttachMock.mockReset()
      sendSessionDetachMock.mockReset()
    },
    emitConnection(connected: boolean) {
      state.connected = connected
      connectionHandlers.forEach((handler) => handler(connected))
    },
    client: {
      connect: connectMock,
      disconnect: disconnectMock,
      setUrl: setUrlMock,
      sendSessionAttach: sendSessionAttachMock,
      sendSessionDetach: sendSessionDetachMock,
      onConnection(handler: (connected: boolean) => void) {
        connectionHandlers.add(handler)
        return () => connectionHandlers.delete(handler)
      },
      onMessage(handler: (msg: Record<string, unknown>) => void) {
        messageHandlers.add(handler)
        return () => messageHandlers.delete(handler)
      },
      get connected() {
        return state.connected
      },
    },
  }
})

const apiMocks = vi.hoisted(() => ({
  getRuntimeSnapshot: vi.fn(),
  setBaseURL: vi.fn(),
}))

const loadSessionsForProjectMock = vi.hoisted(() => vi.fn(async () => []))
const initUIStoreBridgeMock = vi.hoisted(() => vi.fn())
const tauriMocks = vi.hoisted(() => ({
  isDesktopRuntime: vi.fn(() => true),
}))

vi.mock('@/lib/websocket', () => ({
  __esModule: true,
  default: websocketHarness.client,
  wsClient: websocketHarness.client,
}))

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    default: {
      ...actual.default,
      getRuntimeSnapshot: apiMocks.getRuntimeSnapshot,
      setBaseURL: apiMocks.setBaseURL,
    },
  }
})

vi.mock('@/hooks/useGlobalPersistence', () => ({
  useGlobalPersistence: vi.fn(),
  getGlobalPersistenceController: () => ({
    markSessionDeleted: vi.fn(),
  }),
}))

vi.mock('@/lib/uiStoreBridge', () => ({
  initUIStoreBridge: initUIStoreBridgeMock,
}))

vi.mock('@/lib/session-utils', () => ({
  loadSessionsForProject: loadSessionsForProjectMock,
}))

vi.mock('@/lib/tauri', () => ({
  isDesktopRuntime: tauriMocks.isDesktopRuntime,
}))

function resetStores() {
  useConnectionStore.setState({
    connected: false,
    backendReady: true,
    backendUrl: 'http://127.0.0.1:3000',
    wsUrl: 'ws://127.0.0.1:3000/ws',
    startupError: '',
    servicePorts: {},
    initializeRuntime: vi.fn(async () => {}),
    waitForBackend: vi.fn(async () => true),
    checkHealth: vi.fn(async () => false),
  })

  useChatStore.setState({
    sessionId: '',
    runningSessionIds: [],
    sessionRuntimeStates: {},
    sessionThinking: {},
    sessionExecutionMode: {},
    sessionThinkingEnabled: {},
    sessionActiveModelName: {},
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
    messages: [],
    sessionMessages: {},
    isThinking: false,
    isRunning: false,
    executionMode: 'build',
    executionModeTracked: false,
    autoPhase: 'idle',
    thinkingEnabled: false,
    activeModelName: '',
    activeAgentConfig: '',
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

  useSessionViewStore.setState({
    sessions: [],
    sessionsReady: false,
  })

  useUIStore.setState({
    displaySessionId: null,
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

  useRuntimeSnapshotStore.setState({
    snapshot: EMPTY_RUNTIME_SNAPSHOT,
    loading: false,
    applySnapshot: vi.fn((snapshot) => snapshot ?? EMPTY_RUNTIME_SNAPSHOT),
    fetchSnapshot: vi.fn(async () => EMPTY_RUNTIME_SNAPSHOT),
  })
}

describe('WebSocketProvider reconnect hydration integration', () => {
  beforeEach(() => {
    websocketHarness.reset()
    apiMocks.getRuntimeSnapshot.mockReset()
    apiMocks.setBaseURL.mockReset()
    loadSessionsForProjectMock.mockReset()
    loadSessionsForProjectMock.mockResolvedValue([])
    initUIStoreBridgeMock.mockReset()
    tauriMocks.isDesktopRuntime.mockReset()
    tauriMocks.isDesktopRuntime.mockReturnValue(true)
    resetStores()
  })

  it('rehydrates again when backendReady drops during reconnect and later recovers', async () => {
    apiMocks.getRuntimeSnapshot
      .mockResolvedValueOnce(EMPTY_RUNTIME_SNAPSHOT)
      .mockResolvedValueOnce(EMPTY_RUNTIME_SNAPSHOT)

    render(
      <WebSocketProvider>
        <div data-testid="provider-child">provider child</div>
      </WebSocketProvider>,
    )

    await waitFor(() => {
      expect(screen.getByTestId('provider-child')).toBeVisible()
    })

    await waitFor(() => {
      expect(api.getRuntimeSnapshot).toHaveBeenCalledTimes(1)
    })

    act(() => {
      websocketHarness.emitConnection(true)
    })

    act(() => {
      websocketHarness.emitConnection(false)
      useConnectionStore.setState({ backendReady: false })
    })

    act(() => {
      websocketHarness.emitConnection(true)
      useConnectionStore.setState({ backendReady: true })
    })

    await waitFor(() => {
      expect(api.getRuntimeSnapshot).toHaveBeenCalledTimes(2)
    })
  })
})
