import { render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import DiagnosticsPage from './page'
import WebSocketProvider from '@/components/providers/WebSocketProvider'
import { useConnectionStore } from '@/stores/connectionStore'
import { useRuntimeSnapshotStore } from '@/stores/runtimeSnapshotStore'

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
    emitMessage(msg: Record<string, unknown>) {
      messageHandlers.forEach((handler) => handler(msg))
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
  getCapabilities: vi.fn(),
  getToolCatalog: vi.fn(),
}))

const initUIStoreBridgeMock = vi.hoisted(() => vi.fn())
const markSessionDeletedMock = vi.hoisted(() => vi.fn())
const loadSessionsForProjectMock = vi.hoisted(() => vi.fn(async () => []))
const tauriMocks = vi.hoisted(() => ({
  isDesktopRuntime: vi.fn(() => true),
}))

const settingsState = vi.hoisted(() => ({
  config: {
    data_dir: 'data',
    log: { dir: 'data/logs' },
    backend: { ws_url: 'ws://127.0.0.1:3000/ws' },
    ports: { agent_system: { default: 3000 } },
  },
  llmProviders: { providers: [] },
  agentConfigs: [],
  skills: [],
  refreshConfig: vi.fn(async () => undefined),
  refreshLLMProviders: vi.fn(async () => undefined),
  refreshAgentConfigs: vi.fn(async () => undefined),
  refreshTools: vi.fn(async () => undefined),
  refreshSkills: vi.fn(async () => undefined),
}))

const runtimeState = vi.hoisted(() => ({
  snapshot: {
    services: {
      agent: { status: 'running', host: '127.0.0.1', port: 3000 },
      qgis: { status: 'running', port: 50101 },
    },
    instances: { items: [], running_count: 0, instance_count: 0 },
    tools: [],
    diagnostics: [],
    runtime_revision: 'rev-1',
    tool_schema_revision: 'schema-1',
    tool_availability_revision: 'availability-1',
    mode_issues: {},
    mode_readiness: {},
    active_config: '',
    runtime_active_agent_config: '',
    active_instance_id: '',
    stale: false,
    version: 1,
  },
  fetchSnapshot: vi.fn(async () => runtimeState.snapshot),
  loading: false,
}))

const runtimeSnapshotStoreMock = vi.hoisted(() => {
  const subscribe = vi.fn(() => () => {})
  return Object.assign(
    (selector: (state: typeof runtimeState) => unknown) => selector(runtimeState),
    {
      getState: () => runtimeState,
      setState: (next: Partial<typeof runtimeState>) => {
        Object.assign(runtimeState, next)
      },
      subscribe,
    },
  )
})

const chatState = vi.hoisted(() => ({
  sessionId: '',
  runningSessionIds: [],
  sessionRuntimeStates: {},
  sessionMessages: {},
  sessionWorkflowBlocks: {},
  sessionActiveWorkflowBlockId: {},
  sessionFocusedWorkflowBlockId: {},
  addMessage: vi.fn(),
  addSessionMessage: vi.fn(),
  setSessionThinking: vi.fn(),
  setSessionRunning: vi.fn(),
  setSessionRuntimeState: vi.fn(),
  setSessionTasks: vi.fn(),
  addSessionInterrupt: vi.fn(),
  setContextStats: vi.fn(),
  setLastContextResponse: vi.fn(),
  setExecutionMode: vi.fn(),
  setAutoPhase: vi.fn(),
  setActiveModelName: vi.fn(),
  setActiveAgentConfig: vi.fn(),
  setAvailableModels: vi.fn(),
  setAvailableAgentConfigs: vi.fn(),
  setSessionWorkflowBlocks: vi.fn(),
  setSessionActiveWorkflowBlockId: vi.fn(),
  setSessionFocusedWorkflowBlockId: vi.fn(),
  purgeSession: vi.fn(),
}))

const chatStoreMock = vi.hoisted(() =>
  Object.assign(
    (selector: (state: typeof chatState) => unknown) => selector(chatState),
    {
      getState: () => chatState,
      setState: (next: Partial<typeof chatState>) => {
        Object.assign(chatState, next)
      },
      subscribe: vi.fn(() => () => {}),
    },
  ),
)

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
      getCapabilities: apiMocks.getCapabilities,
      getToolCatalog: apiMocks.getToolCatalog,
    },
  }
})

vi.mock('@/hooks/useHydration', async () => {
  const actual = await vi.importActual<typeof import('@/hooks/useHydration')>('@/hooks/useHydration')
  return {
    useHydration: actual.useHydration,
  }
})

vi.mock('@/lib/uiStoreBridge', () => ({
  initUIStoreBridge: initUIStoreBridgeMock,
}))

vi.mock('@/hooks/useGlobalPersistence', () => ({
  useGlobalPersistence: vi.fn(),
  getGlobalPersistenceController: () => ({
    markSessionDeleted: markSessionDeletedMock,
  }),
}))

vi.mock('@/lib/session-utils', () => ({
  loadSessionsForProject: loadSessionsForProjectMock,
}))

vi.mock('@/lib/tauri', () => ({
  isDesktopRuntime: tauriMocks.isDesktopRuntime,
}))

vi.mock('@/stores/settingsStore', () => ({
  useSettingsStore: (selector: (state: typeof settingsState) => unknown) => selector(settingsState),
}))

vi.mock('@/stores/runtimeSnapshotStore', () => ({
  useRuntimeSnapshotStore: runtimeSnapshotStoreMock,
}))

vi.mock('@/stores/chatStore', () => ({
  useChatStore: chatStoreMock,
}))

vi.mock('@/components/layout/AppShell', () => ({
  __esModule: true,
  default: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
}))

describe('DiagnosticsPage runtime reconnect T19', () => {
  beforeEach(() => {
    websocketHarness.reset()
    apiMocks.setBaseURL.mockReset()
    apiMocks.getCapabilities.mockReset()
    apiMocks.getToolCatalog.mockReset()
    apiMocks.getCapabilities.mockResolvedValue({ qgis_available: true, instance_count: 0 })
    apiMocks.getToolCatalog.mockResolvedValue({ tool_infos: [] })
    initUIStoreBridgeMock.mockClear()
    markSessionDeletedMock.mockClear()
    loadSessionsForProjectMock.mockReset()
    loadSessionsForProjectMock.mockResolvedValue([])
    tauriMocks.isDesktopRuntime.mockReset()
    tauriMocks.isDesktopRuntime.mockReturnValue(true)
    chatStoreMock.setState({
      sessionId: '',
      runningSessionIds: [],
      sessionRuntimeStates: {},
      sessionMessages: {},
      sessionWorkflowBlocks: {},
      sessionActiveWorkflowBlockId: {},
      sessionFocusedWorkflowBlockId: {},
      addMessage: vi.fn(),
      addSessionMessage: vi.fn(),
      setSessionThinking: vi.fn(),
      setSessionRunning: vi.fn(),
      setSessionRuntimeState: vi.fn(),
      setSessionTasks: vi.fn(),
      addSessionInterrupt: vi.fn(),
      setContextStats: vi.fn(),
      setLastContextResponse: vi.fn(),
      setExecutionMode: vi.fn(),
      setAutoPhase: vi.fn(),
      setActiveModelName: vi.fn(),
      setActiveAgentConfig: vi.fn(),
      setAvailableModels: vi.fn(),
      setAvailableAgentConfigs: vi.fn(),
      setSessionWorkflowBlocks: vi.fn(),
      setSessionActiveWorkflowBlockId: vi.fn(),
      setSessionFocusedWorkflowBlockId: vi.fn(),
      purgeSession: vi.fn(),
    })

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
      refreshServicePorts: vi.fn(async () => {}),
      setConnected: vi.fn(),
      setBackendUrl: vi.fn(),
      setWsUrl: vi.fn(),
    })

    useRuntimeSnapshotStore.setState({
      snapshot: runtimeState.snapshot,
      loading: false,
      fetchSnapshot: vi.fn(async () => runtimeState.snapshot),
    })
  })

  it('shows protocol mismatch detail on diagnostics route during initial connect', async () => {
    const detail = '协议版本不兼容，请刷新页面。'
    websocketHarness.connectMock.mockImplementationOnce(() => {
      websocketHarness.emitMessage({
        type: 'error',
        content: '',
        data: {
          code: 'UNSUPPORTED_PROTOCOL_VERSION',
          user_action: detail,
        },
      })
    })

    render(
      <WebSocketProvider>
        <DiagnosticsPage />
      </WebSocketProvider>,
    )

    await waitFor(() => {
      expect(websocketHarness.connectMock).toHaveBeenCalledTimes(1)
    })

    await waitFor(() => {
      expect(screen.getByText('无法连接到后端服务')).toBeInTheDocument()
      expect(screen.getByText(detail)).toBeInTheDocument()
    })

    expect(screen.queryByText('运行时诊断')).not.toBeInTheDocument()
  })

  it('keeps protocol mismatch detail visible when unsupported protocol arrives shortly after initial connect', async () => {
    const detail = '协议版本不兼容，请刷新页面。'
    websocketHarness.connectMock.mockImplementationOnce(() => {
      setTimeout(() => {
        websocketHarness.emitMessage({
          type: 'error',
          content: '',
          data: {
            code: 'UNSUPPORTED_PROTOCOL_VERSION',
            user_action: detail,
          },
        })
      }, 50)
    })

    render(
      <WebSocketProvider>
        <DiagnosticsPage />
      </WebSocketProvider>,
    )

    await waitFor(() => {
      expect(websocketHarness.connectMock).toHaveBeenCalledTimes(1)
    })

    await waitFor(() => {
      expect(screen.getByText('无法连接到后端服务')).toBeInTheDocument()
    })

    expect(screen.getByText(detail)).toBeInTheDocument()
    expect(screen.queryByText('请检查 carto-agent 是否正常运行')).not.toBeInTheDocument()
  })
})
