import { create } from 'zustand'
import type { LLMProviderConfig } from '@/types/config'
import {
  AutoPhase,
  ContextResponseData,
  ExecutionMode,
  InterruptRequestData,
  Message,
  TodoItem,
  WorkflowBlock,
} from '@/types/message'
import { generateId } from '@/lib/utils'
import { parseReviewReference, reviewKey } from '@/features/knowledge-review/model'

function refreshedReviewMessage(messages: Message[], incoming: Omit<Message, 'id' | 'timestamp'>): Message | undefined {
  if (incoming.type !== 'interrupt') return undefined
  const ref = parseReviewReference(incoming.data as unknown as InterruptRequestData)
  if (!ref) return undefined
  const previous = messages.find(message => {
    const other = message.type === 'interrupt' ? parseReviewReference(message.data as unknown as InterruptRequestData) : null
    return other && reviewKey(other) === reviewKey(ref)
  })
  const old = previous ? parseReviewReference(previous.data as unknown as InterruptRequestData) : null
  if (old && ((old.revision || 0) > (ref.revision || 0) || (old.revision || 0) === (ref.revision || 0) && (Date.parse(old.updated_at || '') || 0) > (Date.parse(ref.updated_at || '') || 0))) return previous
  return previous ? { ...previous, ...incoming, id: previous.id, timestamp: previous.timestamp } : undefined
}

function asRecord(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return {}
  }
  return value as Record<string, unknown>
}

type RuntimeLLMModel = Pick<
  LLMProviderConfig,
  | 'name'
  | 'model'
  | 'provider'
  | 'has_api_key'
  | 'supports_thinking'
  | 'supports_plan_execute'
  | 'supports_forced_tool_choice'
  | 'ready_for_chat'
  | 'ready_for_plan'
  | 'ready_for_auto_title'
  | 'readiness_reason'
  | 'max_context_tokens'
  | 'auto_compress_threshold'
>

function normalizeSessionId(value: string | null | undefined) {
  return String(value || '').trim()
}

export interface SessionRuntimeStateRecord {
  session_id: string
  run_id?: string
  status?: string
  execution_mode?: string
  phase?: string
  bound_instance_id?: string
  bound_project_path?: string
  checkpoint_id?: string
  runtime_revision?: string
  active_agent_config?: string
  last_error?: string
  failure_code?: string
  failure_reason?: string
  updated_at?: string
}

function normalizeSessionRuntimeState(
  sessionId: string,
  state: Partial<SessionRuntimeStateRecord> | null | undefined,
): SessionRuntimeStateRecord | null {
  const sid = normalizeSessionId(sessionId)
  if (!sid) {
    return null
  }
  const record = asRecord(state)
  return {
    session_id: sid,
    run_id: String(record.run_id || '').trim() || undefined,
    status: String(record.status || '').trim().toLowerCase() || undefined,
    execution_mode: String(record.execution_mode || '').trim() || undefined,
    phase: String(record.phase || '').trim() || undefined,
    bound_instance_id: String(record.bound_instance_id || '').trim() || undefined,
    bound_project_path: String(record.bound_project_path || '').trim() || undefined,
    checkpoint_id: String(record.checkpoint_id || '').trim() || undefined,
    runtime_revision: String(record.runtime_revision || '').trim() || undefined,
    active_agent_config: String(record.active_agent_config || '').trim() || undefined,
    last_error: String(record.last_error || '').trim() || undefined,
    failure_code: String(record.failure_code || '').trim() || undefined,
    failure_reason: String(record.failure_reason || '').trim() || undefined,
    updated_at: String(record.updated_at || '').trim() || undefined,
  }
}

function updateRunningSessionIds(list: string[], sessionId: string, running: boolean) {
  if (!sessionId) {
    return list
  }
  const exists = list.includes(sessionId)
  if (running) {
    if (exists) {
      return list
    }
    return [...list, sessionId]
  }
  if (!exists) {
    return list
  }
  return list.filter((item) => item !== sessionId)
}

function getDefaultFocusedWorkflowBlockId(blocks: WorkflowBlock[]) {
  const runningBlock = blocks.find((item) => item.status === 'running')
  if (runningBlock?.id) {
    return runningBlock.id
  }
  return null
}

interface ChatState {
  sessionId: string | null
  messages: Message[]
  sessionMessages: Record<string, Message[]>
  isThinking: boolean
  isRunning: boolean
  runningSessionIds: string[]
  sessionRuntimeStates: Record<string, SessionRuntimeStateRecord>
  sessionThinking: Record<string, boolean>
  sessionExecutionMode: Record<string, ExecutionMode>
  sessionThinkingEnabled: Record<string, boolean>
  sessionActiveModelName: Record<string, string>
  executionMode: ExecutionMode
  executionModeTracked: boolean
  autoPhase: AutoPhase
  thinkingEnabled: boolean
  activeModelName: string
  activeAgentConfig: string
  availableModels: RuntimeLLMModel[]
  availableAgentConfigs: string[]
  tasks: TodoItem[]
  sessionTasks: Record<string, TodoItem[]>
  pendingInterrupts: InterruptRequestData[]
  sessionPendingInterrupts: Record<string, InterruptRequestData[]>
  workflowBlocks: WorkflowBlock[]
  sessionWorkflowBlocks: Record<string, WorkflowBlock[]>
  activeWorkflowBlockId: string | null
  sessionActiveWorkflowBlockId: Record<string, string | null>
  focusedWorkflowBlockId: string | null
  sessionFocusedWorkflowBlockId: Record<string, string | null>
  inspectorOpen: boolean
  contextStats: {
    messageCount: number
    totalChars: number
    estimatedTokens: number
    updatedAt: number
  }
  lastContextResponse: ContextResponseData | null
  addMessage: (msg: Omit<Message, 'id' | 'timestamp'>) => Message
  addSessionMessage: (sessionId: string, msg: Omit<Message, 'id' | 'timestamp'>) => Message
  setMessages: (messages: Message[]) => void
  setSessionMessages: (sessionId: string, messages: Message[]) => void
  setSessionId: (sessionId: string | null) => void
  setThinking: (thinking: boolean) => void
  setSessionThinking: (sessionId: string, thinking: boolean) => void
  setRunning: (running: boolean) => void
  setSessionRunning: (sessionId: string, running: boolean) => void
  setSessionRuntimeState: (sessionId: string, state: Partial<SessionRuntimeStateRecord>) => void
  setExecutionMode: (mode: ExecutionMode, options?: { persistToSession?: boolean }) => void
  setAutoPhase: (phase: AutoPhase) => void
  setThinkingEnabled: (enabled: boolean) => void
  setActiveModelName: (name: string) => void
  setActiveAgentConfig: (name: string) => void
  setAvailableModels: (models: RuntimeLLMModel[]) => void
  setAvailableAgentConfigs: (configs: string[]) => void
  setTasks: (tasks: TodoItem[]) => void
  setSessionTasks: (sessionId: string, tasks: TodoItem[]) => void
  setWorkflowBlocks: (blocks: WorkflowBlock[]) => void
  setSessionWorkflowBlocks: (sessionId: string, blocks: WorkflowBlock[]) => void
  setActiveWorkflowBlockId: (blockId: string | null) => void
  setSessionActiveWorkflowBlockId: (sessionId: string, blockId: string | null) => void
  setFocusedWorkflowBlockId: (blockId: string | null) => void
  setSessionFocusedWorkflowBlockId: (sessionId: string, blockId: string | null) => void
  setInspectorOpen: (open: boolean) => void
  toggleInspectorOpen: () => void
  addInterrupt: (interrupt: InterruptRequestData) => void
  addSessionInterrupt: (sessionId: string, interrupt: InterruptRequestData) => void
  resolveInterrupt: (checkpointId: string) => void
  resolveSessionInterrupt: (sessionId: string, checkpointId: string) => void
  clearInterrupts: () => void
  clearSessionInterrupts: (sessionId: string) => void
  clearSessionBuffer: (sessionId: string) => void
  purgeSession: (sessionId: string) => void
  setContextStats: (stats: { messageCount: number; totalChars: number; estimatedTokens: number }) => void
  setLastContextResponse: (data: ContextResponseData | null) => void
  clearMessages: () => void
}

export const useChatStore = create<ChatState>((set) => ({
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
  addMessage: (msg) => {
    let output: Message | null = null

    set((state) => {
      const sid = normalizeSessionId(state.sessionId)
      const currentMessages = sid
        ? (state.sessionMessages[sid] || state.messages)
        : state.messages

      const updatedReview = refreshedReviewMessage(currentMessages, msg)
      if (updatedReview) {
        output = updatedReview
        const next = currentMessages.map(item => item.id === updatedReview.id ? updatedReview : item)
        return { messages: next, ...(sid ? { sessionMessages: { ...state.sessionMessages, [sid]: next } } : {}) }
      }

      if (msg.type === 'interrupt') {
        const incoming = asRecord(msg.data)
        const incomingCheckpoint = String(incoming.checkpoint_id || '').trim()
        if (incomingCheckpoint && !parseReviewReference(incoming as unknown as InterruptRequestData)) {
          const existed = currentMessages.find((item) => {
            if (item.type !== 'interrupt') {
              return false
            }
            const payload = asRecord(item.data)
            return String(payload.checkpoint_id || '').trim() === incomingCheckpoint
          })
          if (existed) {
            output = existed
            return state
          }
        }
      }

      const payload: Message = {
        ...msg,
        id: generateId(),
        timestamp: Date.now(),
      }
      output = payload
      if (!sid) {
        return {
          messages: [...state.messages, payload],
        }
      }

      const nextMessages = [...currentMessages, payload]
      return {
        messages: nextMessages,
        sessionMessages: {
          ...state.sessionMessages,
          [sid]: nextMessages,
        },
      }
    })

    if (output) {
      return output
    }

    const fallback: Message = {
      ...msg,
      id: generateId(),
      timestamp: Date.now(),
    }
    set((state) => {
      const sid = normalizeSessionId(state.sessionId)
      if (!sid) {
        return {
          messages: [...state.messages, fallback],
        }
      }
      const currentMessages = state.sessionMessages[sid] || state.messages
      const nextMessages = [...currentMessages, fallback]
      return {
        messages: nextMessages,
        sessionMessages: {
          ...state.sessionMessages,
          [sid]: nextMessages,
        },
      }
    })
    return fallback
  },
  addSessionMessage: (sessionId, msg) => {
    let output: Message | null = null

    set((state) => {
      const sid = normalizeSessionId(sessionId)
      if (!sid) {
        return state
      }
      const currentMessages = state.sessionMessages[sid] || (state.sessionId === sid ? state.messages : [])

      const updatedReview = refreshedReviewMessage(currentMessages, msg)
      if (updatedReview) {
        output = updatedReview
        const next = currentMessages.map(item => item.id === updatedReview.id ? updatedReview : item)
        return { sessionMessages: { ...state.sessionMessages, [sid]: next }, ...(state.sessionId === sid ? { messages: next } : {}) }
      }

      if (msg.type === 'interrupt') {
        const incoming = asRecord(msg.data)
        const incomingCheckpoint = String(incoming.checkpoint_id || '').trim()
        if (incomingCheckpoint && !parseReviewReference(incoming as unknown as InterruptRequestData)) {
          const existed = currentMessages.find((item) => {
            if (item.type !== 'interrupt') {
              return false
            }
            const payload = asRecord(item.data)
            return String(payload.checkpoint_id || '').trim() === incomingCheckpoint
          })
          if (existed) {
            output = existed
            return state
          }
        }
      }

      const payload: Message = {
        ...msg,
        id: generateId(),
        timestamp: Date.now(),
      }
      output = payload
      const nextMessages = [...currentMessages, payload]
      const patch: Partial<ChatState> = {
        sessionMessages: {
          ...state.sessionMessages,
          [sid]: nextMessages,
        },
      }
      if (state.sessionId === sid) {
        patch.messages = nextMessages
      }
      return patch
    })

    if (output) {
      return output
    }

    const fallback: Message = {
      ...msg,
      id: generateId(),
      timestamp: Date.now(),
    }
    set((state) => {
      const sid = normalizeSessionId(sessionId)
      if (!sid) {
        return state
      }
      const currentMessages = state.sessionMessages[sid] || (state.sessionId === sid ? state.messages : [])
      const nextMessages = [...currentMessages, fallback]
      const patch: Partial<ChatState> = {
        sessionMessages: {
          ...state.sessionMessages,
          [sid]: nextMessages,
        },
      }
      if (state.sessionId === sid) {
        patch.messages = nextMessages
      }
      return patch
    })
    return fallback
  },
  setMessages: (messages) =>
    set((state) => {
      const sid = normalizeSessionId(state.sessionId)
      if (!sid) {
        return { messages }
      }
      return {
        messages,
        sessionMessages: {
          ...state.sessionMessages,
          [sid]: messages,
        },
      }
    }),
  setSessionMessages: (sessionId, messages) =>
    set((state) => {
      const sid = normalizeSessionId(sessionId)
      if (!sid) {
        return state
      }
      const patch: Partial<ChatState> = {
        sessionMessages: {
          ...state.sessionMessages,
          [sid]: messages,
        },
      }
      if (state.sessionId === sid) {
        patch.messages = messages
      }
      return patch
    }),
  setSessionId: (sessionId) =>
    set((state) => {
      const previousSid = normalizeSessionId(state.sessionId)
      const sid = normalizeSessionId(sessionId)
      const patch: Partial<ChatState> = {}

      if (previousSid) {
        if (state.executionModeTracked) {
          patch.sessionExecutionMode = {
            ...state.sessionExecutionMode,
            [previousSid]: state.executionMode,
          }
        }
        patch.sessionThinkingEnabled = {
          ...state.sessionThinkingEnabled,
          [previousSid]: state.thinkingEnabled,
        }
        patch.sessionActiveModelName = {
          ...state.sessionActiveModelName,
          [previousSid]: state.activeModelName,
        }
      }

      if (!sid) {
        return {
          ...patch,
          sessionId: null,
          messages: [],
          isThinking: false,
          isRunning: false,
          tasks: [],
          pendingInterrupts: [],
          workflowBlocks: [],
          activeWorkflowBlockId: null,
          focusedWorkflowBlockId: null,
          executionMode: 'build',
          executionModeTracked: false,
          autoPhase: 'idle' as AutoPhase,
        }
      }

      const workflowBlocks = state.sessionWorkflowBlocks[sid] || []
      const activeWorkflowBlockId = state.sessionActiveWorkflowBlockId[sid] ?? null
      const focusedWorkflowBlockId = state.sessionFocusedWorkflowBlockId[sid] ?? getDefaultFocusedWorkflowBlockId(workflowBlocks)
      const messages = state.sessionMessages[sid] || []
      const hasTrackedExecutionMode = Object.prototype.hasOwnProperty.call(state.sessionExecutionMode, sid)
      const nextExecutionMode = hasTrackedExecutionMode ? state.sessionExecutionMode[sid] : 'build'
      const nextThinkingEnabled = state.sessionThinkingEnabled[sid] ?? false
      const nextActiveModelName = state.sessionActiveModelName[sid] ?? state.activeModelName
		const nextActiveAgentConfig = ''

      return {
        ...patch,
        sessionId: sid,
        messages,
        isThinking: Boolean(state.sessionThinking[sid]),
        isRunning: state.runningSessionIds.includes(sid),
        tasks: state.sessionTasks[sid] || [],
        pendingInterrupts: state.sessionPendingInterrupts[sid] || [],
        workflowBlocks,
        activeWorkflowBlockId,
        focusedWorkflowBlockId,
        executionMode: nextExecutionMode,
        executionModeTracked: hasTrackedExecutionMode,
        autoPhase: 'idle',
        thinkingEnabled: nextThinkingEnabled,
        activeModelName: nextActiveModelName,
        activeAgentConfig: nextActiveAgentConfig,
      }
    }),
  setThinking: (isThinking) =>
    set((state) => {
      const sid = normalizeSessionId(state.sessionId)
      if (!sid) {
        return { isThinking }
      }
      return {
        isThinking,
        sessionThinking: {
          ...state.sessionThinking,
          [sid]: isThinking,
        },
      }
    }),
  setSessionThinking: (sessionId, isThinking) =>
    set((state) => {
      const sid = normalizeSessionId(sessionId)
      if (!sid) {
        return state
      }
      const patch: Partial<ChatState> = {
        sessionThinking: {
          ...state.sessionThinking,
          [sid]: isThinking,
        },
      }
      if (state.sessionId === sid) {
        patch.isThinking = isThinking
      }
      return patch
    }),
  setRunning: (isRunning) =>
    set((state) => {
      const sid = normalizeSessionId(state.sessionId)
      if (!sid) {
        return { isRunning }
      }
      return {
        isRunning,
        runningSessionIds: updateRunningSessionIds(state.runningSessionIds, sid, isRunning),
      }
    }),
  setSessionRunning: (sessionId, isRunning) =>
    set((state) => {
      const sid = normalizeSessionId(sessionId)
      if (!sid) {
        return state
      }
      const patch: Partial<ChatState> = {
        runningSessionIds: updateRunningSessionIds(state.runningSessionIds, sid, isRunning),
      }
      if (state.sessionId === sid) {
        patch.isRunning = isRunning
      }
      return patch
    }),
  setSessionRuntimeState: (sessionId, nextState) =>
    set((state) => {
      const sid = normalizeSessionId(sessionId)
      const normalized = normalizeSessionRuntimeState(sid, nextState)
      if (!sid || !normalized) {
        return state
      }
      return {
        sessionRuntimeStates: {
          ...state.sessionRuntimeStates,
          [sid]: normalized,
        },
      }
    }),
  setExecutionMode: (executionMode, options) =>
    set((state) => {
      const sid = normalizeSessionId(state.sessionId)
      const persistToSession = options?.persistToSession ?? Boolean(sid)
      const patch: Partial<ChatState> = {
        executionMode,
        executionModeTracked: persistToSession,
        autoPhase: 'idle',
      }
      if (sid && persistToSession) {
        patch.sessionExecutionMode = {
          ...state.sessionExecutionMode,
          [sid]: executionMode,
        }
      }
      return patch
    }),
  setAutoPhase: (autoPhase) => set({ autoPhase }),
  setThinkingEnabled: (thinkingEnabled) =>
    set((state) => {
      const sid = normalizeSessionId(state.sessionId)
      const patch: Partial<ChatState> = { thinkingEnabled }
      if (sid) {
        patch.sessionThinkingEnabled = {
          ...state.sessionThinkingEnabled,
          [sid]: thinkingEnabled,
        }
      }
      return patch
    }),
  setActiveModelName: (activeModelName) =>
    set((state) => {
      const sid = normalizeSessionId(state.sessionId)
      const patch: Partial<ChatState> = { activeModelName }
      if (sid) {
        patch.sessionActiveModelName = {
          ...state.sessionActiveModelName,
          [sid]: activeModelName,
        }
      }
      return patch
    }),
  setActiveAgentConfig: (activeAgentConfig) =>
		set(() => {
			return { activeAgentConfig }
		}),
  setAvailableModels: (availableModels) => set({ availableModels }),
  setAvailableAgentConfigs: (availableAgentConfigs) => set({ availableAgentConfigs }),
  setTasks: (tasks) =>
    set((state) => {
      const sid = normalizeSessionId(state.sessionId)
      if (!sid) {
        return { tasks }
      }
      return {
        tasks,
        sessionTasks: {
          ...state.sessionTasks,
          [sid]: tasks,
        },
      }
    }),
  setSessionTasks: (sessionId, tasks) =>
    set((state) => {
      const sid = normalizeSessionId(sessionId)
      if (!sid) {
        return state
      }
      const patch: Partial<ChatState> = {
        sessionTasks: {
          ...state.sessionTasks,
          [sid]: tasks,
        },
      }
      if (state.sessionId === sid) {
        patch.tasks = tasks
      }
      return patch
    }),
  setWorkflowBlocks: (workflowBlocks) =>
    set((state) => {
      const sid = normalizeSessionId(state.sessionId)
      if (!sid) {
        return { workflowBlocks }
      }
      return {
        workflowBlocks,
        sessionWorkflowBlocks: {
          ...state.sessionWorkflowBlocks,
          [sid]: workflowBlocks,
        },
      }
    }),
  setSessionWorkflowBlocks: (sessionId, workflowBlocks) =>
    set((state) => {
      const sid = normalizeSessionId(sessionId)
      if (!sid) {
        return state
      }
      const patch: Partial<ChatState> = {
        sessionWorkflowBlocks: {
          ...state.sessionWorkflowBlocks,
          [sid]: workflowBlocks,
        },
      }
      if (state.sessionId === sid) {
        patch.workflowBlocks = workflowBlocks
      }
      return patch
    }),
  setActiveWorkflowBlockId: (activeWorkflowBlockId) =>
    set((state) => {
      const sid = normalizeSessionId(state.sessionId)
      if (!sid) {
        return { activeWorkflowBlockId }
      }
      return {
        activeWorkflowBlockId,
        sessionActiveWorkflowBlockId: {
          ...state.sessionActiveWorkflowBlockId,
          [sid]: activeWorkflowBlockId,
        },
      }
    }),
  setSessionActiveWorkflowBlockId: (sessionId, activeWorkflowBlockId) =>
    set((state) => {
      const sid = normalizeSessionId(sessionId)
      if (!sid) {
        return state
      }
      const patch: Partial<ChatState> = {
        sessionActiveWorkflowBlockId: {
          ...state.sessionActiveWorkflowBlockId,
          [sid]: activeWorkflowBlockId,
        },
      }
      if (state.sessionId === sid) {
        patch.activeWorkflowBlockId = activeWorkflowBlockId
      }
      return patch
    }),
  setFocusedWorkflowBlockId: (focusedWorkflowBlockId) =>
    set((state) => {
      const sid = normalizeSessionId(state.sessionId)
      if (!sid) {
        return { focusedWorkflowBlockId }
      }
      return {
        focusedWorkflowBlockId,
        sessionFocusedWorkflowBlockId: {
          ...state.sessionFocusedWorkflowBlockId,
          [sid]: focusedWorkflowBlockId,
        },
      }
    }),
  setSessionFocusedWorkflowBlockId: (sessionId, focusedWorkflowBlockId) =>
    set((state) => {
      const sid = normalizeSessionId(sessionId)
      if (!sid) {
        return state
      }
      const patch: Partial<ChatState> = {
        sessionFocusedWorkflowBlockId: {
          ...state.sessionFocusedWorkflowBlockId,
          [sid]: focusedWorkflowBlockId,
        },
      }
      if (state.sessionId === sid) {
        patch.focusedWorkflowBlockId = focusedWorkflowBlockId
      }
      return patch
    }),
  setInspectorOpen: (inspectorOpen) => set({ inspectorOpen }),
  toggleInspectorOpen: () =>
    set((state) => ({
      inspectorOpen: !state.inspectorOpen,
    })),
  addInterrupt: (interrupt) =>
    set((state) => {
      const sid = normalizeSessionId(state.sessionId)
      if (!sid) {
        return state
      }
      const checkpointId = String(interrupt.checkpoint_id || '').trim()
      if (!checkpointId) {
        return state
      }
      const currentInterrupts = state.sessionPendingInterrupts[sid] || []
      const exists = currentInterrupts.some(
        (item) => String(item.checkpoint_id || '').trim() === checkpointId,
      )
      const nextInterrupts = exists ? currentInterrupts.map(item => item.checkpoint_id === checkpointId ? interrupt : item) : [...currentInterrupts, interrupt]
      return {
        pendingInterrupts: nextInterrupts,
        sessionPendingInterrupts: {
          ...state.sessionPendingInterrupts,
          [sid]: nextInterrupts,
        },
      }
    }),
  addSessionInterrupt: (sessionId, interrupt) =>
    set((state) => {
      const sid = normalizeSessionId(sessionId)
      if (!sid) {
        return state
      }
      const checkpointId = String(interrupt.checkpoint_id || '').trim()
      if (!checkpointId) {
        return state
      }
      const currentInterrupts = state.sessionPendingInterrupts[sid] || []
      const exists = currentInterrupts.some(
        (item) => String(item.checkpoint_id || '').trim() === checkpointId,
      )
      const nextInterrupts = exists ? currentInterrupts.map(item => item.checkpoint_id === checkpointId ? interrupt : item) : [...currentInterrupts, interrupt]
      const patch: Partial<ChatState> = {
        sessionPendingInterrupts: {
          ...state.sessionPendingInterrupts,
          [sid]: nextInterrupts,
        },
      }
      if (state.sessionId === sid) {
        patch.pendingInterrupts = nextInterrupts
      }
      return patch
    }),
  resolveInterrupt: (checkpointId) =>
    set((state) => {
      const sid = normalizeSessionId(state.sessionId)
      if (!sid) {
        return state
      }
      const nextInterrupts = (state.sessionPendingInterrupts[sid] || []).filter(
        (item) => String(item.checkpoint_id || '').trim() !== checkpointId,
      )
      return {
        pendingInterrupts: nextInterrupts,
        sessionPendingInterrupts: {
          ...state.sessionPendingInterrupts,
          [sid]: nextInterrupts,
        },
      }
    }),
  resolveSessionInterrupt: (sessionId, checkpointId) =>
    set((state) => {
      const sid = normalizeSessionId(sessionId)
      if (!sid) {
        return state
      }
      const nextInterrupts = (state.sessionPendingInterrupts[sid] || []).filter(
        (item) => String(item.checkpoint_id || '').trim() !== checkpointId,
      )
      const patch: Partial<ChatState> = {
        sessionPendingInterrupts: {
          ...state.sessionPendingInterrupts,
          [sid]: nextInterrupts,
        },
      }
      if (state.sessionId === sid) {
        patch.pendingInterrupts = nextInterrupts
      }
      return patch
    }),
  clearInterrupts: () =>
    set((state) => {
      const sid = normalizeSessionId(state.sessionId)
      if (!sid) {
        return { pendingInterrupts: [] }
      }
      return {
        pendingInterrupts: [],
        sessionPendingInterrupts: {
          ...state.sessionPendingInterrupts,
          [sid]: [],
        },
      }
    }),
  clearSessionInterrupts: (sessionId) =>
    set((state) => {
      const sid = normalizeSessionId(sessionId)
      if (!sid) {
        return state
      }
      const patch: Partial<ChatState> = {
        sessionPendingInterrupts: {
          ...state.sessionPendingInterrupts,
          [sid]: [],
        },
      }
      if (state.sessionId === sid) {
        patch.pendingInterrupts = []
      }
      return patch
    }),
  clearSessionBuffer: (sessionId) =>
    set((state) => {
      const sid = normalizeSessionId(sessionId)
      if (!sid) {
        return state
      }
      const patch: Partial<ChatState> = {
        sessionMessages: {
          ...state.sessionMessages,
          [sid]: [],
        },
      }
      if (state.sessionId === sid) {
        patch.messages = []
      }
      return patch
    }),
  purgeSession: (sessionId) =>
    set((state) => {
      const sid = normalizeSessionId(sessionId)
      if (!sid) {
        return state
      }

      const restSessionMessages = { ...state.sessionMessages }
      delete restSessionMessages[sid]

      const restSessionTasks = { ...state.sessionTasks }
      delete restSessionTasks[sid]

      const restSessionInterrupts = { ...state.sessionPendingInterrupts }
      delete restSessionInterrupts[sid]

      const restSessionWorkflowBlocks = { ...state.sessionWorkflowBlocks }
      delete restSessionWorkflowBlocks[sid]

      const restSessionActiveBlock = { ...state.sessionActiveWorkflowBlockId }
      delete restSessionActiveBlock[sid]

      const restSessionFocusedBlock = { ...state.sessionFocusedWorkflowBlockId }
      delete restSessionFocusedBlock[sid]

      const restSessionThinking = { ...state.sessionThinking }
      delete restSessionThinking[sid]

      const restSessionRuntimeStates = { ...state.sessionRuntimeStates }
      delete restSessionRuntimeStates[sid]

      const restSessionExecutionMode = { ...state.sessionExecutionMode }
      delete restSessionExecutionMode[sid]

      const restSessionThinkingEnabled = { ...state.sessionThinkingEnabled }
      delete restSessionThinkingEnabled[sid]

      const restSessionActiveModelName = { ...state.sessionActiveModelName }
      delete restSessionActiveModelName[sid]

		const patch: Partial<ChatState> = {
        sessionMessages: restSessionMessages,
        sessionTasks: restSessionTasks,
        sessionPendingInterrupts: restSessionInterrupts,
        sessionWorkflowBlocks: restSessionWorkflowBlocks,
        sessionActiveWorkflowBlockId: restSessionActiveBlock,
        sessionFocusedWorkflowBlockId: restSessionFocusedBlock,
        sessionThinking: restSessionThinking,
        sessionRuntimeStates: restSessionRuntimeStates,
        sessionExecutionMode: restSessionExecutionMode,
			sessionThinkingEnabled: restSessionThinkingEnabled,
			sessionActiveModelName: restSessionActiveModelName,
			runningSessionIds: state.runningSessionIds.filter((item) => item !== sid),
		}

      if (state.sessionId === sid) {
        patch.messages = []
        patch.tasks = []
        patch.pendingInterrupts = []
        patch.workflowBlocks = []
        patch.activeWorkflowBlockId = null
        patch.focusedWorkflowBlockId = null
        patch.isThinking = false
        patch.isRunning = false
      }

      return patch
    }),
  setContextStats: (stats) =>
    set({
      contextStats: {
        messageCount: Math.max(0, Number(stats.messageCount || 0)),
        totalChars: Math.max(0, Number(stats.totalChars || 0)),
        estimatedTokens: Math.max(0, Number(stats.estimatedTokens || 0)),
        updatedAt: Date.now(),
      },
    }),
  setLastContextResponse: (lastContextResponse) => set({ lastContextResponse }),
  clearMessages: () =>
    set((state) => {
      const sid = normalizeSessionId(state.sessionId)
      if (!sid) {
        return {
          messages: [],
          tasks: [],
          pendingInterrupts: [],
          workflowBlocks: [],
          activeWorkflowBlockId: null,
          focusedWorkflowBlockId: null,
          autoPhase: 'idle' as AutoPhase,
          lastContextResponse: null,
          contextStats: {
            messageCount: 0,
            totalChars: 0,
            estimatedTokens: 0,
            updatedAt: 0,
          },
        }
      }

      return {
        messages: [],
        tasks: [],
        pendingInterrupts: [],
        workflowBlocks: [],
        activeWorkflowBlockId: null,
        focusedWorkflowBlockId: null,
        isThinking: false,
        isRunning: false,
        autoPhase: 'idle' as AutoPhase,
        lastContextResponse: null,
        contextStats: {
          messageCount: 0,
          totalChars: 0,
          estimatedTokens: 0,
          updatedAt: 0,
        },
        sessionTasks: {
          ...state.sessionTasks,
          [sid]: [],
        },
        sessionMessages: {
          ...state.sessionMessages,
          [sid]: [],
        },
        sessionPendingInterrupts: {
          ...state.sessionPendingInterrupts,
          [sid]: [],
        },
        sessionWorkflowBlocks: {
          ...state.sessionWorkflowBlocks,
          [sid]: [],
        },
        sessionActiveWorkflowBlockId: {
          ...state.sessionActiveWorkflowBlockId,
          [sid]: null,
        },
        sessionFocusedWorkflowBlockId: {
          ...state.sessionFocusedWorkflowBlockId,
          [sid]: null,
        },
        sessionThinking: {
          ...state.sessionThinking,
          [sid]: false,
        },
        runningSessionIds: updateRunningSessionIds(state.runningSessionIds, sid, false),
      }
    }),
}))
