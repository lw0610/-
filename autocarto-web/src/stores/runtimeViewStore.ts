import { create } from 'zustand'
import type { LLMProviderConfig } from '@/types/config'
import type {
  AutoPhase,
  ContextResponseData,
  ExecutionMode,
} from '@/types/message'

export type RuntimeLLMModel = Pick<
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

function asRecord(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return {}
  }
  return value as Record<string, unknown>
}

function normalizeSessionId(value: string | null | undefined) {
  return String(value || '').trim()
}

function normalizeSessionRuntimeState(
  sessionId: string,
  state: Partial<SessionRuntimeStateRecord> | null | undefined,
): SessionRuntimeStateRecord | null {
  const sid = normalizeSessionId(sessionId)
  if (!sid) return null
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

interface RuntimeViewState {
  runningSessionIds: string[]
  sessionRuntimeStates: Record<string, SessionRuntimeStateRecord>

  availableModels: RuntimeLLMModel[]
  availableAgentConfigs: string[]
  activeModelName: string
  activeAgentConfig: string

  executionMode: ExecutionMode
  executionModeTracked: boolean
  autoPhase: AutoPhase
  thinkingEnabled: boolean

  contextStats: {
    messageCount: number
    totalChars: number
    estimatedTokens: number
    updatedAt: number
  }
  lastContextResponse: ContextResponseData | null

  setRunningSessionIds: (ids: string[]) => void
  addRunningSession: (sessionId: string) => void
  removeRunningSession: (sessionId: string) => void
  setSessionRuntimeState: (sessionId: string, state: Partial<SessionRuntimeStateRecord>) => void
  setAvailableModels: (models: RuntimeLLMModel[]) => void
  setAvailableAgentConfigs: (configs: string[]) => void
  setActiveModelName: (name: string) => void
  setActiveAgentConfig: (name: string) => void
  setExecutionMode: (mode: ExecutionMode) => void
  setExecutionModeTracked: (tracked: boolean) => void
  setAutoPhase: (phase: AutoPhase) => void
  setThinkingEnabled: (enabled: boolean) => void
  setContextStats: (stats: { messageCount: number; totalChars: number; estimatedTokens: number }) => void
  setLastContextResponse: (data: ContextResponseData | null) => void
}

export const useRuntimeViewStore = create<RuntimeViewState>((set) => ({
  runningSessionIds: [],
  sessionRuntimeStates: {},

  availableModels: [],
  availableAgentConfigs: [],
  activeModelName: '',
  activeAgentConfig: '',

  executionMode: 'build',
  executionModeTracked: false,
  autoPhase: 'idle',
  thinkingEnabled: false,

  contextStats: {
    messageCount: 0,
    totalChars: 0,
    estimatedTokens: 0,
    updatedAt: 0,
  },
  lastContextResponse: null,

  setRunningSessionIds: (ids) => set({ runningSessionIds: ids }),

  addRunningSession: (sessionId) => set((state) => {
    const sid = normalizeSessionId(sessionId)
    if (!sid || state.runningSessionIds.includes(sid)) return state
    return { runningSessionIds: [...state.runningSessionIds, sid] }
  }),

  removeRunningSession: (sessionId) => set((state) => {
    const sid = normalizeSessionId(sessionId)
    if (!sid || !state.runningSessionIds.includes(sid)) return state
    return { runningSessionIds: state.runningSessionIds.filter((id) => id !== sid) }
  }),

  setSessionRuntimeState: (sessionId, partialState) => set((state) => {
    const normalized = normalizeSessionRuntimeState(sessionId, partialState)
    if (!normalized) return state
    return {
      sessionRuntimeStates: {
        ...state.sessionRuntimeStates,
        [normalized.session_id]: {
          ...(state.sessionRuntimeStates[normalized.session_id] || {}),
          ...normalized,
        },
      },
    }
  }),

  setAvailableModels: (models) => set({ availableModels: models }),
  setAvailableAgentConfigs: (configs) => set({ availableAgentConfigs: configs }),
  setActiveModelName: (name) => set({ activeModelName: name }),
  setActiveAgentConfig: (name) => set({ activeAgentConfig: name }),
  setExecutionMode: (mode) => set({ executionMode: mode }),
  setExecutionModeTracked: (tracked) => set({ executionModeTracked: tracked }),
  setAutoPhase: (phase) => set({ autoPhase: phase }),
  setThinkingEnabled: (enabled) => set({ thinkingEnabled: enabled }),
  setContextStats: (stats) => set({
    contextStats: { ...stats, updatedAt: Date.now() },
  }),
  setLastContextResponse: (data) => set({ lastContextResponse: data }),
}))
