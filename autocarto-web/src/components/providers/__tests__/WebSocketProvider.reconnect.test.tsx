import { render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import WebSocketProvider from '../WebSocketProvider'
import { EMPTY_RUNTIME_SNAPSHOT } from '@/lib/api'
import { useChatStore } from '@/stores/chatStore'
import { useConnectionStore } from '@/stores/connectionStore'
import { useInstanceStore } from '@/stores/instanceStore'
import { useRuntimeSnapshotStore } from '@/stores/runtimeSnapshotStore'
import { useSessionStore } from '@/stores/sessionStore'

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
  setBaseURL: vi.fn(),
}))

const loadSessionsForProjectMock = vi.hoisted(() => vi.fn(async () => []))
const useHydrationMock = vi.hoisted(() => vi.fn())
const initUIStoreBridgeMock = vi.hoisted(() => vi.fn())
const markSessionDeletedMock = vi.hoisted(() => vi.fn())
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
      setBaseURL: apiMocks.setBaseURL,
    },
  }
})

vi.mock('@/hooks/useGlobalPersistence', () => ({
  useGlobalPersistence: vi.fn(),
  getGlobalPersistenceController: () => ({
    markSessionDeleted: markSessionDeletedMock,
  }),
}))

vi.mock('@/hooks/useHydration', () => ({
  useHydration: useHydrationMock,
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

function resetStores(waitForBackendMock: ReturnType<typeof vi.fn>) {
  useConnectionStore.setState({
    connected: false,
    backendReady: false,
    backendUrl: 'http://127.0.0.1:3000',
    wsUrl: 'ws://127.0.0.1:3000/ws',
    startupError: 'backend supervisor still starting',
    servicePorts: {},
    initializeRuntime: vi.fn(async () => {}),
    waitForBackend: waitForBackendMock,
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
    sessionsReady: true,
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

  useRuntimeSnapshotStore.setState({
    snapshot: EMPTY_RUNTIME_SNAPSHOT,
    loading: false,
    fetchSnapshot: vi.fn(async () => EMPTY_RUNTIME_SNAPSHOT),
  })
}

describe('WebSocketProvider reconnect baseline', () => {
  beforeEach(() => {
    websocketHarness.reset()
    apiMocks.setBaseURL.mockReset()
    loadSessionsForProjectMock.mockReset()
    loadSessionsForProjectMock.mockResolvedValue([])
    useHydrationMock.mockClear()
    initUIStoreBridgeMock.mockClear()
    markSessionDeletedMock.mockClear()
    tauriMocks.isDesktopRuntime.mockReset()
    tauriMocks.isDesktopRuntime.mockReturnValue(true)
  })

  it('keeps children visible while an authoritative desktop runtime is still reconnecting', async () => {
    const waitForBackendMock = vi.fn(async () => false)
    resetStores(waitForBackendMock)

    render(
      <WebSocketProvider>
        <div data-testid="reconnect-child">runtime diagnostics child</div>
      </WebSocketProvider>,
    )

    await waitFor(() => {
      expect(waitForBackendMock).toHaveBeenCalledTimes(1)
    })

    await waitFor(() => {
      expect(screen.getByTestId('reconnect-child')).toBeVisible()
    })

    expect(screen.queryByText('无法连接到后端服务')).not.toBeInTheDocument()
  })
})
