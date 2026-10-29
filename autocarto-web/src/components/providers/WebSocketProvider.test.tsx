import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import WebSocketProvider from './WebSocketProvider'
import { EMPTY_RUNTIME_SNAPSHOT } from '@/lib/api'
import type { SessionRecord } from '@/lib/api'
import { useChatStore } from '@/stores/chatStore'
import { useConnectionStore } from '@/stores/connectionStore'
import { useInstanceStore } from '@/stores/instanceStore'
import { useRuntimeSnapshotStore } from '@/stores/runtimeSnapshotStore'
import { useSessionStore } from '@/stores/sessionStore'
import type { WorkflowBlock, WSMessage } from '@/types/message'
import { useFlowRunStore } from '@/features/plan/store'

function createSessionRecord(
  sessionId: string,
  projectPath = '/tmp/reconnect-test.qgz',
): SessionRecord {
  return {
    session_id: sessionId,
    name: sessionId,
    created_at: '2026-04-01T00:00:00Z',
    updated_at: '2026-04-01T00:00:00Z',
    project_path: projectPath,
    messages: [],
  }
}

const websocketHarness = vi.hoisted(() => {
  const messageHandlers = new Set<(msg: WSMessage) => void>()
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
    emitMessage(msg: WSMessage) {
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
      onMessage(handler: (msg: WSMessage) => void) {
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

const loadSessionsForProjectMock = vi.hoisted(
  () => vi.fn<() => Promise<SessionRecord[]>>(async () => []),
)
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

function resetStores() {
  useFlowRunStore.setState({ plans: {}, runs: {}, blocked: {}, snapshotSeq: {}, eventSeq: {} })
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
    sessionId: 'sid-reconnect',
    runningSessionIds: ['sid-reconnect'],
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
    sessions: [createSessionRecord('sid-reconnect')],
    sessionsReady: true,
    activeSessionId: 'sid-reconnect',
    sessionMutationTick: 0,
    lastMutatedSessionId: null,
    sessionAttachState: {},
  })

  useInstanceStore.setState({
    instances: [
      {
        id: 'instance-a',
        pid: 4242,
        qgis_version: '3.34.0',
        project_path: '/tmp/reconnect-test.qgz',
        status: 'running',
        services: {},
        created_at: '2026-04-01T00:00:00Z',
        updated_at: '2026-04-01T00:00:00Z',
        last_heartbeat: '2026-04-01T00:00:00Z',
        source: 'register',
      },
    ],
    activeInstanceId: 'instance-a',
    qgisAvailable: true,
    projectConnected: true,
    instanceCount: 1,
    runningCount: 1,
    stale: false,
    loading: false,
  })

  useRuntimeSnapshotStore.setState({
    snapshot: EMPTY_RUNTIME_SNAPSHOT,
    loading: false,
    fetchSnapshot: vi.fn(async () => EMPTY_RUNTIME_SNAPSHOT),
  })
}

function AttachStateProbe() {
  const attachState = useSessionStore((state) => state.sessionAttachState)
  return <pre data-testid="attach-state">{JSON.stringify(attachState)}</pre>
}

function shouldShowStartupError(startupError: string, backendReady: boolean) {
  if (!startupError.trim()) {
    return false
  }
  if (!backendReady) {
    return true
  }
  return startupError.includes('协议版本') || startupError.includes('升级客户端')
}

function DiagnosticsLikeChild() {
  const backendReady = useConnectionStore((state) => state.backendReady)
  const startupError = useConnectionStore((state) => state.startupError)

  return (
    <div>
      {shouldShowStartupError(startupError, backendReady) ? (
        <div>
          <div>协议版本不兼容</div>
          <div>{startupError}</div>
        </div>
      ) : null}
      <div>运行时诊断</div>
    </div>
  )
}

describe('WebSocketProvider session attach recovery', () => {
  beforeEach(() => {
    websocketHarness.reset()
    apiMocks.setBaseURL.mockReset()
    loadSessionsForProjectMock.mockReset()
    loadSessionsForProjectMock.mockImplementation(
      async (): Promise<SessionRecord[]> => [createSessionRecord('sid-reconnect')],
    )
    useHydrationMock.mockClear()
    initUIStoreBridgeMock.mockClear()
    markSessionDeletedMock.mockClear()
    tauriMocks.isDesktopRuntime.mockReset()
    tauriMocks.isDesktopRuntime.mockReturnValue(true)
    resetStores()
  })

  it.each([new Error('runtime port file 不可用'), 'runtime port file 不可用'])(
    'ends the startup spinner and exposes a rejected runtime lookup: %s',
    async (failure) => {
      useConnectionStore.setState({ initializeRuntime: vi.fn().mockRejectedValue(failure) })
      render(<WebSocketProvider><div>connected shell</div></WebSocketProvider>)

      expect(await screen.findByText('无法连接到后端服务')).toBeInTheDocument()
      expect(screen.getByText('runtime port file 不可用')).toBeInTheDocument()
      expect(screen.queryByText('正在连接后端服务...')).not.toBeInTheDocument()
      expect(screen.getByRole('button', { name: '重试' })).toBeEnabled()
      expect(websocketHarness.connectMock).not.toHaveBeenCalled()
      expect(useConnectionStore.getState().backendReady).toBe(false)
    },
  )

  it('can retry a failed runtime lookup and render the normal shell after recovery', async () => {
    const initializeRuntime = vi.fn()
      .mockRejectedValueOnce(new Error('runtime 暂时不可用'))
      .mockImplementationOnce(async () => {
        useConnectionStore.setState({ startupError: '', backendReady: true })
      })
    useConnectionStore.setState({ initializeRuntime })
    render(<WebSocketProvider><div>recovered shell</div></WebSocketProvider>)

    await screen.findByText('无法连接到后端服务')
    fireEvent.click(screen.getByRole('button', { name: '重试' }))
    expect(await screen.findByText('recovered shell')).toBeInTheDocument()
    expect(initializeRuntime).toHaveBeenCalledTimes(2)
    expect(websocketHarness.connectMock).toHaveBeenCalledTimes(1)
    expect(screen.queryByText('无法连接到后端服务')).not.toBeInTheDocument()
  })

  it('keeps an existing protocol incompatibility visible when runtime lookup also fails', async () => {
    useConnectionStore.setState({
      startupError: '协议版本不兼容，请升级客户端。',
      initializeRuntime: vi.fn().mockRejectedValue(new Error('temporary runtime failure')),
    })
    render(<WebSocketProvider><div>incompatible shell</div></WebSocketProvider>)

    expect(await screen.findByText('无法连接到后端服务')).toBeInTheDocument()
    expect(screen.getByText('协议版本不兼容，请升级客户端。')).toBeInTheDocument()
    expect(screen.queryByText('temporary runtime failure')).not.toBeInTheDocument()
    expect(screen.queryByText('incompatible shell')).not.toBeInTheDocument()
  })

  it('ends the startup spinner when backend readiness checking rejects', async () => {
    useConnectionStore.setState({ waitForBackend: vi.fn().mockRejectedValue(new Error('backend readiness failed')) })
    render(<WebSocketProvider><div>connected shell</div></WebSocketProvider>)

    expect(await screen.findByText('backend readiness failed')).toBeInTheDocument()
    expect(screen.queryByText('正在连接后端服务...')).not.toBeInTheDocument()
    expect(websocketHarness.connectMock).not.toHaveBeenCalled()
  })

  it('ignores a late runtime rejection after this provider has unmounted', async () => {
    let rejectLookup!: (error: Error) => void
    useConnectionStore.setState({ initializeRuntime: vi.fn(() => new Promise<void>((_, reject) => { rejectLookup = reject })) })
    const view = render(<WebSocketProvider><div>connected shell</div></WebSocketProvider>)
    view.unmount()
    useConnectionStore.setState({ startupError: 'new runtime state', backendReady: true })
    await act(async () => { rejectLookup(new Error('old runtime failure')) })
    expect(useConnectionStore.getState().startupError).toBe('new runtime state')
    expect(useConnectionStore.getState().backendReady).toBe(true)
  })

  it.each(['session_runtime_state', 'cancelled', 'session_state'] as const)(
    '%s cancellation clears pending review and stops the session workflow',
    async (type) => {
      const sessionId = 'sid-reconnect'
      const workflow: WorkflowBlock = {
        id: 'wf-review', agent: 'knowledge_engineer', agentDisplayName: '知识工程师',
        summary: '等待核对', status: 'running', startedAt: 1, updatedAt: 1, entries: [],
      }
      const store = useChatStore.getState()
      store.setSessionRunning(sessionId, true)
      store.setSessionThinking(sessionId, true)
      store.addSessionInterrupt(sessionId, { checkpoint_id: 'cp-review', interrupt_contexts: [] })
      store.setSessionWorkflowBlocks(sessionId, [workflow])
      store.setSessionActiveWorkflowBlockId(sessionId, workflow.id)
      store.setSessionRunning('sid-other', true)
      store.addSessionInterrupt('sid-other', { checkpoint_id: 'cp-other', interrupt_contexts: [] })

      await act(async () => { render(<WebSocketProvider><div>cancel review</div></WebSocketProvider>) })
      expect(useChatStore.getState().pendingInterrupts).toHaveLength(1)
      expect(useChatStore.getState().isRunning).toBe(true)

      await act(async () => {
        websocketHarness.emitMessage({
          type, content: '', session_id: sessionId,
          data: {
            status: 'cancelled',
            ...(type === 'session_state' ? { workflow_blocks: [{ ...workflow, id: 'wf-restored' }] } : {}),
          },
        })
      })

      const state = useChatStore.getState()
      expect(state.pendingInterrupts).toEqual([])
      expect(state.sessionPendingInterrupts[sessionId]).toEqual([])
      expect(state.isRunning).toBe(false)
      expect(state.isThinking).toBe(false)
      expect(state.sessionThinking[sessionId]).toBe(false)
      expect(state.runningSessionIds).not.toContain(sessionId)
      expect(state.activeWorkflowBlockId).toBeNull()
      expect(state.sessionActiveWorkflowBlockId[sessionId]).toBeNull()
      expect(state.workflowBlocks).toHaveLength(1)
      expect(state.workflowBlocks[0]).toMatchObject({
        id: type === 'session_state' ? 'wf-restored' : 'wf-review', status: 'completed',
      })
      expect(state.sessionWorkflowBlocks[sessionId]).toEqual(state.workflowBlocks)
      expect(state.runningSessionIds).toContain('sid-other')
      expect(state.sessionPendingInterrupts['sid-other']).toHaveLength(1)
    },
  )

  it('updates the same knowledge card and process through waiting, failure and recovery without another approval', async () => {
    const sid = 'sid-reconnect'
    await act(async () => { render(<WebSocketProvider><div>review status</div></WebSocketProvider>) })
    const send = (status: string, type = 'knowledge_review_update') => websocketHarness.emitMessage({ type, session_id: sid, content: '', data: {
      checkpoint_id: 'cp-review', interrupt_contexts: [{ id: 'interrupt-1', is_root_cause: true, info: { type: 'knowledge_review', session_id: sid, review_id: 'review-1', revision: 2, status, description: 'route_style is required', errors: status === 'compile_failed' ? [{ message: 'route_style is required' }] : [] } }],
    } })
    act(() => { send('awaiting_review', 'interrupt_request'); send('awaiting_review', 'interrupt_request') })
    const original = useChatStore.getState().messages.find(message => message.type === 'interrupt')!
    expect(useChatStore.getState().messages.filter(message => message.type === 'interrupt')).toHaveLength(1)
    expect(useChatStore.getState().workflowBlocks.at(-1)?.status).toBe('waiting_user')
    expect(useChatStore.getState().pendingInterrupts).toHaveLength(1)
    for (const [status, expected] of [['compiling', 'running'], ['compile_failed', 'error'], ['compiling', 'running'], ['compiled', 'completed']]) {
      act(() => { send(status) })
      const state = useChatStore.getState()
      const cards = state.messages.filter(message => message.type === 'interrupt')
      expect(cards).toHaveLength(1)
      expect(cards[0].id).toBe(original.id)
      expect(cards[0].timestamp).toBe(original.timestamp)
      expect(state.pendingInterrupts).toHaveLength(0)
      expect(state.workflowBlocks.at(-1)?.status).toBe(expected)
      if (status === 'compile_failed') expect(cards[0].content).toContain('route_style is required')
    }
  })

  it('keeps concurrent same-name tool results paired with their call IDs', async () => {
    await act(async () => { render(<WebSocketProvider><div>tool identity</div></WebSocketProvider>) })
    await act(async () => {
      websocketHarness.emitConnection(true)
      const emit = (phase: string, call_id: string, data: Record<string, unknown>) => websocketHarness.emitMessage({type: 'agent_workflow', session_id: 'sid-reconnect', agent: 'orchestrator', phase, content: '资源查询', data: {tool: 'resource_get', call_id, ...data}})
      emit('tool_call', 'a', {tool_args: {name: '统计专题图'}})
      emit('tool_call', 'b', {tool_args: {name: '统计专题图指南'}})
      emit('tool_result', 'a', {status: 'failed', result: '统计专题图未找到'})
      emit('tool_result', 'b', {status: 'failed', result: '统计专题图指南未找到'})
      emit('tool_result', 'a', {status: 'failed', result: '统计专题图未找到'})
    })
    const cards = useChatStore.getState().sessionWorkflowBlocks['sid-reconnect'].flatMap(block => block.entries).map(entry => entry.toolCard).filter(Boolean)
    expect(cards).toHaveLength(2)
    expect(cards[0]).toMatchObject({callId: 'a', args: {name: '统计专题图'}, result: '统计专题图未找到'})
    expect(cards[1]).toMatchObject({callId: 'b', args: {name: '统计专题图指南'}, result: '统计专题图指南未找到'})
  })

  it('shows explicit request status after tool activity and replaces its heartbeats', async () => {
    await act(async () => { render(<WebSocketProvider><div>progress</div></WebSocketProvider>) })
    await act(async () => {
      websocketHarness.emitConnection(true)
      websocketHarness.emitMessage({
        type: 'agent_workflow', session_id: 'sid-reconnect', agent: 'knowledge_engineer',
        phase: 'thinking', content: '处理知识抽取',
      })
      for (let i = 0; i < 100; i++) {
        websocketHarness.emitMessage({
          type: 'agent_workflow', session_id: 'sid-reconnect', agent: 'knowledge_engineer',
          phase: 'progress', content: `边界核对 第1次请求 已等待${i * 20}秒`,
          data: { stage: 'boundary', request_key: 'boundary/c1-c2/1/start', status: 'running', elapsed_ms: i * 20000 },
        })
      }
    })
    const state = useChatStore.getState()
    const blocks = state.sessionWorkflowBlocks['sid-reconnect']
    const progress = blocks.flatMap((block) => block.entries).filter((entry) => entry.phase === 'progress')
    expect(progress).toHaveLength(1)
    expect(progress[0].type).toBe('status')
    expect(progress[0].content).toContain('1980秒')
    expect(blocks.at(-1)?.summary).toContain('1980秒')
    expect(state.sessionThinking['sid-reconnect']).toBe(false)
    expect(state.runningSessionIds).toContain('sid-reconnect')
    expect(state.sessionMessages['sid-reconnect'] ?? []).toHaveLength(0)
    await act(async () => websocketHarness.emitMessage({
      type: 'agent_workflow', session_id: 'sid-reconnect', agent: 'knowledge_engineer',
      phase: 'progress', content: '模型已返回；正在校验',
      data: { stage: 'boundary', request_key: 'boundary/c1-c2/1/start', status: 'response_received' },
    }))
    const updated = useChatStore.getState().sessionWorkflowBlocks['sid-reconnect'].flatMap((block) => block.entries).filter((entry) => entry.phase === 'progress')
    expect(updated).toHaveLength(1)
    expect(updated[0].data?.status).toBe('response_received')
  })

  it('keeps parallel OSM downloads separate across delegation and ignores late progress after completion or cancellation', async () => {
    await act(async () => { render(<WebSocketProvider><div>OSM progress</div></WebSocketProvider>) })
    const emit = (key: string, phase: string, content: string) => websocketHarness.emitMessage({
      type: 'agent_workflow', session_id: 'sid-reconnect', agent: 'data_engineer',
      phase: 'progress', content,
      data: { source: 'osm', request_key: key, download_phase: phase, completed_bytes: 1024, total_bytes: 0 },
    })
    await act(async () => {
      websocketHarness.emitConnection(true)
      emit('osm:rivers', 'waiting', '正在请求 rivers.geojson')
      emit('osm:railways', 'downloading', '正在下载 railways.geojson：已接收 0.01 MB')
      websocketHarness.emitMessage({ type: 'agent_workflow', session_id: 'sid-reconnect', agent: 'orchestrator', phase: 'decision', content: '其他处理' })
      emit('osm:rivers', 'completed', '已下载 rivers.geojson：3 个要素')
      emit('osm:rivers', 'downloading', '过时心跳')
      emit('osm:railways', 'cancelled', '下载已取消：railways.geojson')
      emit('osm:railways', 'waiting', '过时等待')
    })
    const state = useChatStore.getState()
    const blocks = state.sessionWorkflowBlocks['sid-reconnect']
    const entries = blocks.flatMap(block => block.entries).filter(entry => entry.data?.source === 'osm')
    expect(entries).toHaveLength(2)
    expect(entries.map(entry => entry.data?.download_phase)).toEqual(['completed', 'cancelled'])
    expect(entries[0].content).toContain('3 个要素')
    expect(entries[1].content).toContain('已取消')
    expect(entries.every(entry => entry.type === 'status' && entry.data?.percent === undefined)).toBe(true)
    expect(blocks).toHaveLength(2)
    expect(state.sessionMessages['sid-reconnect'] ?? []).toHaveLength(0)
  })

  it('renders children without connecting when web runtime lacks an authoritative descriptor', async () => {
    tauriMocks.isDesktopRuntime.mockReturnValue(false)
    useConnectionStore.setState({
      backendReady: false,
      backendUrl: '',
      wsUrl: '',
      startupError: 'discovery descriptor unavailable',
      initializeRuntime: vi.fn(async () => {}),
      waitForBackend: vi.fn(async () => false),
      checkHealth: vi.fn(async () => false),
    })

    render(
      <WebSocketProvider>
        <div data-testid="shell-child">shell mounted</div>
      </WebSocketProvider>,
    )

    await waitFor(() => {
      expect(screen.getByTestId('shell-child')).toBeVisible()
    })

    expect(websocketHarness.connectMock).not.toHaveBeenCalled()
    expect(screen.queryByText('无法连接到后端服务')).not.toBeInTheDocument()
  })

  it('updates session subscriptions and finishes a flow without rebuilding the connection', async () => {
    const sessions = ['sid-reconnect', 'sid-next', 'sid-background'].map((id) => createSessionRecord(id))
    loadSessionsForProjectMock.mockResolvedValue(sessions)
    const { unmount } = render(<WebSocketProvider><div>persistent connection</div></WebSocketProvider>)
    await waitFor(() => expect(websocketHarness.connectMock).toHaveBeenCalledTimes(1))
    await act(async () => websocketHarness.emitConnection(true))
    expect(websocketHarness.sendSessionAttachMock).toHaveBeenCalledWith('sid-reconnect', undefined)

    act(() => useSessionStore.getState().setActiveSession('sid-next'))
    expect(websocketHarness.sendSessionAttachMock).toHaveBeenCalledWith('sid-next', undefined)
    act(() => useChatStore.getState().setSessionRunning('sid-background', true))
    expect(websocketHarness.sendSessionAttachMock).toHaveBeenCalledWith('sid-background', undefined)
    act(() => useChatStore.getState().setSessionId('sid-next'))

    for (const [seq, event] of [[1, 'flow.started'], [2, 'flow.finished']] as const) {
      await act(async () => websocketHarness.emitMessage({
        type: 'run_event', content: '', session_id: 'sid-reconnect',
        data: {
          seq, source_type: 'flow_event',
          payload: {
            event, session_id: 'sid-reconnect', flow_run_id: 'flow-connection',
            payload: { flow_id: 'demo', status: event === 'flow.finished' ? 'completed' : 'running', steps: [] },
          },
        },
      }))
    }
    expect(useFlowRunStore.getState().runs['sid-reconnect'][0].status).toBe('completed')
    expect(websocketHarness.sendSessionDetachMock).toHaveBeenCalledWith('sid-reconnect')
    act(() => useChatStore.getState().setSessionRunning('sid-background', false))
    expect(websocketHarness.sendSessionDetachMock).toHaveBeenCalledWith('sid-background')
    expect(useConnectionStore.getState().connected).toBe(true)
    expect(websocketHarness.connectMock).toHaveBeenCalledTimes(1)
    expect(websocketHarness.disconnectMock).not.toHaveBeenCalled()
    expect(useConnectionStore.getState().initializeRuntime).toHaveBeenCalledTimes(1)
    expect(useConnectionStore.getState().waitForBackend).toHaveBeenCalledTimes(1)

    unmount()
    expect(websocketHarness.disconnectMock).toHaveBeenCalledTimes(1)
  })

  it('uses the latest session targets when the same connection subscription reconnects', async () => {
    loadSessionsForProjectMock.mockResolvedValue([
      createSessionRecord('sid-reconnect'), createSessionRecord('sid-latest'),
    ])
    render(<WebSocketProvider><AttachStateProbe /></WebSocketProvider>)
    await waitFor(() => expect(websocketHarness.connectMock).toHaveBeenCalledTimes(1))
    await act(async () => websocketHarness.emitConnection(true))
    act(() => websocketHarness.emitConnection(false))
    act(() => {
      useSessionStore.getState().setActiveSession('sid-latest')
      useChatStore.getState().setSessionId('sid-latest')
      useChatStore.getState().setSessionRunning('sid-reconnect', false)
    })
    websocketHarness.sendSessionAttachMock.mockClear()
    await act(async () => websocketHarness.emitConnection(true))

    expect(websocketHarness.sendSessionAttachMock.mock.calls).toEqual([['sid-latest', undefined]])
    expect(useConnectionStore.getState().connected).toBe(true)
    expect(websocketHarness.connectMock).toHaveBeenCalledTimes(1)
    expect(websocketHarness.disconnectMock).not.toHaveBeenCalled()
  })

  it('reconnects desired sessions with last seen seq and promotes them back to live', async () => {
    render(
      <WebSocketProvider>
        <AttachStateProbe />
      </WebSocketProvider>,
    )

    await waitFor(() => {
      expect(websocketHarness.connectMock).toHaveBeenCalledTimes(1)
    })

    act(() => {
      websocketHarness.emitConnection(true)
    })

    await waitFor(() => {
      expect(websocketHarness.sendSessionAttachMock).toHaveBeenCalledWith('sid-reconnect', undefined)
    })

    act(() => {
      websocketHarness.emitMessage({
        type: 'run_event',
        content: '',
        session_id: 'sid-reconnect',
        data: {
          seq: 11,
          payload: { session_id: 'sid-reconnect' },
        },
      } as WSMessage)
    })

    act(() => {
      websocketHarness.emitConnection(false)
    })

    await waitFor(() => {
      expect(screen.getByTestId('attach-state')).toHaveTextContent(
        JSON.stringify({ 'sid-reconnect': 'reattaching' }),
      )
    })

    act(() => {
      websocketHarness.emitConnection(true)
    })

    await waitFor(() => {
      expect(websocketHarness.sendSessionAttachMock).toHaveBeenLastCalledWith('sid-reconnect', 11)
    })

    act(() => {
      websocketHarness.emitMessage({
        type: 'session_state',
        content: '',
        data: {
          session_id: 'sid-reconnect',
        },
      } as WSMessage)
    })

    await waitFor(() => {
      expect(screen.getByTestId('attach-state')).toHaveTextContent(
        JSON.stringify({ 'sid-reconnect': 'live' }),
      )
    })
  })

  it('marks blocked attaches as replay_required when backend requests replay', async () => {
    useChatStore.setState({
      sessionId: 'sid-blocked',
      runningSessionIds: ['sid-blocked'],
    })
    useSessionStore.setState({
      sessions: [createSessionRecord('sid-blocked')],
      activeSessionId: 'sid-blocked',
      sessionAttachState: {},
    })
    loadSessionsForProjectMock.mockImplementation(
      async (): Promise<SessionRecord[]> => [createSessionRecord('sid-blocked')],
    )

    render(
      <WebSocketProvider>
        <AttachStateProbe />
      </WebSocketProvider>,
    )

    await waitFor(() => {
      expect(websocketHarness.connectMock).toHaveBeenCalledTimes(1)
    })

    act(() => {
      websocketHarness.emitConnection(true)
      websocketHarness.emitMessage({
        type: 'session.response',
        content: '',
        data: {
          action: 'attach',
          success: false,
          code: 'REPLAY_REQUIRED',
          session_id: 'sid-blocked',
        },
      } as WSMessage)
    })

    await waitFor(() => {
      expect(screen.getByTestId('attach-state')).toHaveTextContent(
        JSON.stringify({ 'sid-blocked': 'replay_required' }),
      )
    })
  })

  it('TestForcedRefreshAfterRevisionJump', async () => {
    const applySnapshotMock = vi.fn((snapshot) => snapshot)
    const fetchSnapshotMock = vi.fn(async () => EMPTY_RUNTIME_SNAPSHOT)

    useRuntimeSnapshotStore.setState({
      snapshot: {
        ...EMPTY_RUNTIME_SNAPSHOT,
        config_revision: 'config-rev-1',
        runtime_revision: 'runtime-rev-1',
        tool_schema_revision: 'schema-rev-1',
      },
      loading: false,
      applySnapshot: applySnapshotMock,
      fetchSnapshot: fetchSnapshotMock,
    })

    render(
      <WebSocketProvider>
        <AttachStateProbe />
      </WebSocketProvider>,
    )

    await waitFor(() => {
      expect(websocketHarness.connectMock).toHaveBeenCalledTimes(1)
    })

    fetchSnapshotMock.mockClear()
    applySnapshotMock.mockClear()

    act(() => {
      websocketHarness.emitMessage({
        type: 'runtime_snapshot',
        content: '',
        data: {
          ...EMPTY_RUNTIME_SNAPSHOT,
          config_revision: 'config-rev-2',
          runtime_revision: 'runtime-rev-2',
          tool_schema_revision: 'schema-rev-2',
        },
      } as WSMessage)
    })

    await waitFor(() => {
      expect(fetchSnapshotMock).toHaveBeenCalledTimes(1)
    })
    expect(applySnapshotMock).not.toHaveBeenCalled()
  })

  it('TestSnapshotInvalidation', async () => {
    const applySnapshotMock = vi.fn((snapshot) => snapshot)
    const fetchSnapshotMock = vi.fn(async () => EMPTY_RUNTIME_SNAPSHOT)

    useRuntimeSnapshotStore.setState({
      snapshot: {
        ...EMPTY_RUNTIME_SNAPSHOT,
        config_revision: 'config-rev-2',
        runtime_revision: 'runtime-rev-2',
      },
      loading: false,
      applySnapshot: applySnapshotMock,
      fetchSnapshot: fetchSnapshotMock,
    })

    render(
      <WebSocketProvider>
        <AttachStateProbe />
      </WebSocketProvider>,
    )

    await waitFor(() => {
      expect(websocketHarness.connectMock).toHaveBeenCalledTimes(1)
    })

    fetchSnapshotMock.mockClear()
    applySnapshotMock.mockClear()

    act(() => {
      websocketHarness.emitMessage({
        type: 'runtime_snapshot',
        content: '',
        data: {
          ...EMPTY_RUNTIME_SNAPSHOT,
          config_revision: 'config-rev-2',
          runtime_revision: 'runtime-rev-2',
          stale: true,
        },
      } as WSMessage)
    })

    await waitFor(() => {
      expect(fetchSnapshotMock).toHaveBeenCalledTimes(1)
    })
    expect(applySnapshotMock).not.toHaveBeenCalled()
  })

  it('surfaces unsupported protocol version from session responses', async () => {
    render(
      <WebSocketProvider>
        <AttachStateProbe />
      </WebSocketProvider>,
    )

    await waitFor(() => {
      expect(websocketHarness.connectMock).toHaveBeenCalledTimes(1)
    })

    act(() => {
      websocketHarness.emitMessage({
        type: 'session.response',
        content: '',
        session_id: 'sid-reconnect',
        data: {
          action: 'attach',
          success: false,
          code: 'UNSUPPORTED_PROTOCOL_VERSION',
          user_action: '请升级客户端后重试。',
        },
      } as WSMessage)
    })

    await waitFor(() => {
      expect(useConnectionStore.getState().startupError).toContain('请升级客户端后重试。')
    })
    expect(useConnectionStore.getState().connected).toBe(false)
    expect(useConnectionStore.getState().backendReady).toBe(false)
  })

  it('surfaces unsupported protocol version from websocket errors', async () => {
    render(
      <WebSocketProvider>
        <AttachStateProbe />
      </WebSocketProvider>,
    )

    await waitFor(() => {
      expect(websocketHarness.connectMock).toHaveBeenCalledTimes(1)
    })

    act(() => {
      websocketHarness.emitMessage({
        type: 'error',
        content: '',
        session_id: 'sid-reconnect',
        data: {
          code: 'UNSUPPORTED_PROTOCOL_VERSION',
          user_action: '协议版本不兼容，请刷新页面。',
        },
      } as WSMessage)
    })

    await waitFor(() => {
      expect(useConnectionStore.getState().startupError).toContain('协议版本不兼容，请刷新页面。')
    })
    expect(useConnectionStore.getState().connected).toBe(false)
    expect(useConnectionStore.getState().backendReady).toBe(false)
  })

  it('does not coerce unknown run_event payloads into legacy status messages', async () => {
    render(
      <WebSocketProvider>
        <AttachStateProbe />
      </WebSocketProvider>,
    )

    await waitFor(() => {
      expect(websocketHarness.connectMock).toHaveBeenCalledTimes(1)
    })

    act(() => {
      websocketHarness.emitMessage({
        type: 'run_event',
        content: 'unexpected legacy fallback',
        session_id: 'sid-reconnect',
        data: {
          event: 'mystery_event',
          payload: {
            session_id: 'sid-reconnect',
            content: 'unexpected legacy fallback',
          },
        },
      } as WSMessage)
    })

    await waitFor(() => {
      expect(useChatStore.getState().sessionMessages['sid-reconnect'] || []).toHaveLength(0)
      expect(useChatStore.getState().messages).toHaveLength(0)
    })
  })

  it('receives run_event directly without legacy coercion', async () => {
    render(
      <WebSocketProvider>
        <AttachStateProbe />
      </WebSocketProvider>,
    )

    await waitFor(() => {
      expect(websocketHarness.connectMock).toHaveBeenCalledTimes(1)
    })

    act(() => {
      websocketHarness.emitMessage({
        type: 'run_event',
        content: '',
        session_id: 'sid-reconnect',
        data: {
          event: 'message',
          source_type: 'response',
          payload: {
            session_id: 'sid-reconnect',
            content: 'should stay run_event',
            agent: 'test_agent'
          },
        },
      } as WSMessage)
    })

    await waitFor(() => {
      const messages = useChatStore.getState().sessionMessages['sid-reconnect'] || []
      expect(messages).toHaveLength(0)
      expect(useChatStore.getState().messages).toHaveLength(0)
    })
  })

  it('restores flow-only snapshots, skips covered replay and folds tagged process messages', async () => {
    render(<WebSocketProvider><AttachStateProbe /></WebSocketProvider>)
    await waitFor(() => expect(websocketHarness.connectMock).toHaveBeenCalledTimes(1))
    const snapshot = { api_version: 'flow-run/v1', flow_run_id: 'flow-r', session_id: 'sid-reconnect', flow_id: 'demo', status: 'waiting_user', started_at: '2026-10-21T00:00:00Z', steps: [{ id: 's1', index: 1, title: '检查', role: '数据角色', status: 'waiting_user', attempts: [], outputs: {} }], result: { deliverables: {}, issues: [], summary: '' } }
    act(() => {
      websocketHarness.emitMessage({ type: 'run_event', content: '', session_id: 'sid-reconnect', data: { source_type: 'session_state', event: 'session_state', payload: { session_id: 'sid-reconnect', flow_run: snapshot, flow_run_event_seq: 12 } } })
      websocketHarness.emitMessage({ type: 'run_event', content: '', session_id: 'sid-reconnect', data: { seq: 11, replayed: true, source_type: 'flow_event', payload: { event: 'flow.step_started', session_id: 'sid-reconnect', flow_run_id: 'flow-r', payload: { step_id: 's1', attempt: 1, run_id: 'old' } } } })
    })
    expect(useFlowRunStore.getState().runs['sid-reconnect'][0].steps[0].status).toBe('waiting_user')
    act(() => websocketHarness.emitMessage({ type: 'run_event', content: '', session_id: 'sid-reconnect', data: { seq: 13, replayed: true, source_type: 'agent_workflow', payload: { content: '真实能力回执', session_id: 'sid-reconnect', flow_run_id: 'flow-r', step_id: 's1', phase: 'tool_result', tool: 'get_layers_info', result: '图层存在' } } }))
    expect(useChatStore.getState().messages.at(-1)).toMatchObject({ type: 'tool', content: '真实能力回执', data: { flow_run_id: 'flow-r', step_id: 's1' } })
    expect(useChatStore.getState().workflowBlocks).toEqual([])
    expect(useFlowRunStore.getState().runs['sid-reconnect'][0].steps[0].status).toBe('waiting_user')
    act(() => websocketHarness.emitMessage({ type: 'flow_event', content: '', session_id: 'other-session', data: { event: 'flow.started', session_id: 'other-session', flow_run_id: 'other-r', payload: { flow_id: 'demo', steps: [] } } }))
    expect(useFlowRunStore.getState().runs['other-session'][0].flow_run_id).toBe('other-r')
    expect(useChatStore.getState().sessionId).toBe('sid-reconnect')
    await act(async () => {})
  })


  it('restores and routes cartographic plans without turning them into ordinary messages', async () => {
    render(<WebSocketProvider><AttachStateProbe /></WebSocketProvider>)
    await waitFor(() => expect(websocketHarness.connectMock).toHaveBeenCalledTimes(1))
    const plan = { api_version:'carto-plan/v1',plan_id:'p',version:1,goal:'test',facts:[],assumptions:[],data:[],steps:[],deliverables:[],checks:[] }
    act(() => websocketHarness.emitMessage({ type:'plan_event',session_id:'sid-reconnect',content:'',data:{plan,status:'awaiting_confirmation',needs_confirmation:true,execution_mode:'build'} }))
    expect(useFlowRunStore.getState().plans['sid-reconnect'][0].plan_id).toBe('p')
    act(() => websocketHarness.emitMessage({type:'run_event',session_id:'other',content:'',data:{source_type:'plan_event',payload:{session_id:'other',plan:{...plan,version:2},status:'completed',execution_mode:'build'}}}))
    expect(useFlowRunStore.getState().plans.other[0].status).toBe('completed')
    expect(useChatStore.getState().sessionId).toBe('sid-reconnect')
    act(() => websocketHarness.emitMessage({type:'run_event',session_id:'sid-reconnect',content:'',data:{source_type:'session_state',payload:{session_id:'sid-reconnect',carto_plans:[{plan,status:'running',execution_mode:'build'}]}}}))
    expect(useFlowRunStore.getState().plans['sid-reconnect'][0].status).toBe('running')
    expect(useChatStore.getState().messages.some(item=>item.content==='test')).toBe(false)
  })

  it('shows fatal startup shell when unsupported protocol arrives during initial connect', async () => {
    websocketHarness.connectMock.mockImplementationOnce(() => {
      websocketHarness.emitMessage({
        type: 'error',
        content: '',
        session_id: 'sid-reconnect',
        data: {
          code: 'UNSUPPORTED_PROTOCOL_VERSION',
          user_action: '协议版本不兼容，请刷新页面。',
        },
      } as WSMessage)
    })

    render(
      <WebSocketProvider>
        <div>diagnostics child content</div>
      </WebSocketProvider>,
    )

    expect(await screen.findByText('无法连接到后端服务')).toBeInTheDocument()
    expect(screen.getByText('协议版本不兼容，请刷新页面。')).toBeInTheDocument()
    expect(screen.queryByText('diagnostics child content')).not.toBeInTheDocument()
  })

  it('surfaces sticky unsupported protocol errors via the global failure shell', async () => {
    websocketHarness.connectMock.mockImplementationOnce(() => {
      websocketHarness.emitMessage({
        type: 'error',
        content: '',
        session_id: 'sid-reconnect',
        data: {
          code: 'UNSUPPORTED_PROTOCOL_VERSION',
          user_action: '协议版本不兼容，请刷新页面。',
        },
      } as WSMessage)
    })

    render(
      <WebSocketProvider>
        <DiagnosticsLikeChild />
      </WebSocketProvider>,
    )

    expect(await screen.findByText('无法连接到后端服务')).toBeInTheDocument()
    expect(screen.getByText('协议版本不兼容，请刷新页面。')).toBeInTheDocument()
    expect(screen.queryByText('运行时诊断')).not.toBeInTheDocument()
  })

  it('keeps sticky unsupported protocol startup error in store after connect-time failure', async () => {
    websocketHarness.connectMock.mockImplementationOnce(() => {
      websocketHarness.emitMessage({
        type: 'error',
        content: '',
        session_id: 'sid-reconnect',
        data: {
          code: 'UNSUPPORTED_PROTOCOL_VERSION',
          user_action: '协议版本不兼容，请刷新页面。',
        },
      } as WSMessage)
    })

    render(
      <WebSocketProvider>
        <DiagnosticsLikeChild />
      </WebSocketProvider>,
    )

    expect(await screen.findByText('无法连接到后端服务')).toBeInTheDocument()
    expect(useConnectionStore.getState().startupError).toContain('协议版本不兼容')
    expect(useConnectionStore.getState().backendReady).toBe(false)
    expect(screen.getByText('协议版本不兼容，请刷新页面。')).toBeInTheDocument()
  })
})
