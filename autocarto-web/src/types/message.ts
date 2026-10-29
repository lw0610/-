export type ExecutionMode = 'plan' | 'build'
export type RunIntent = 'plan_only' | 'execute'
export type AutoPhase = 'idle' | 'planning' | 'evaluating' | 'building' | 'completed'

export interface ChatAttachment { path: string; kind: 'file' | 'folder' }
export interface ChatLayerReference { id: string; name: string }
export interface ChatInputMetadata {
  attachments?: ChatAttachment[]
  layers?: ChatLayerReference[]
  guide_id?: string
}

export interface Message {
  id: string
  type:
    | 'user'
    | 'assistant'
    | 'status'
    | 'thinking'
    | 'tool'
    | 'agent'
    | 'error'
    | 'decision'
    | 'interrupt'
    | 'context'
    | 'phase_transition'
  content: string
  timestamp: number
  agent?: string
  agentDisplayName?: string
  phase?: string
  data?: Record<string, unknown>
  workflowBlockId?: string
}

export type WorkflowBlockStatus = 'running' | 'waiting_user' | 'completed' | 'error'

export interface WorkflowToolCard {
  id: string
  callId?: string
  tool: string
  status: 'running' | 'completed' | 'failed'
  args?: Record<string, unknown>
  result?: string
  statusSource?: string
  success?: boolean
  startedAt: number
  finishedAt?: number
}

export interface WorkflowEntry {
  id: string
  type: 'thinking' | 'tool' | 'status' | 'decision' | 'note'
  content: string
  phase?: string
  timestamp: number
  data?: Record<string, unknown>
  toolCard?: WorkflowToolCard
}

export interface WorkflowBlock {
  id: string
  agent: string
  agentDisplayName: string
  summary: string
  status: WorkflowBlockStatus
  startedAt: number
  updatedAt: number
  finishedAt?: number
  entries: WorkflowEntry[]
}

export interface TodoItem {
  content?: string
  status?: string
  priority?: string
  owner?: string
}

export interface InterruptOption {
  label?: string
  value?: string
  reason?: string
}

export type DecisionSelectMode = 'single' | 'multiple' | 'confirm' | 'text'

export interface DecisionItem {
  id?: string
  title?: string
  description?: string
  select_mode?: DecisionSelectMode
  options?: InterruptOption[]
  default?: string
  defaults?: string[]
  required?: boolean
  category?: string
}

export interface InterruptContext {
  id?: string
  info?: unknown
  is_root_cause?: boolean
  address?: string
}

export interface InterruptRequestData {
  flow_run_id?: string
  step_id?: string
  checkpoint_id?: string
  interrupt_contexts?: InterruptContext[]
}

export interface ContextResponseData {
  request_id?: string
  success?: boolean
  error?: string
  summary?: string
  compressed_context?: Array<Record<string, unknown>>
  original_count?: number
  compressed_count?: number
  message_count?: number
  total_chars?: number
  estimated_tokens?: number
}

export interface WSMessage {
  type: string
  content: string
  session_id?: string
  data?: Record<string, unknown>
  agent?: string
  agent_display_name?: string
  phase?: string
  event?: string
  run_id?: string
}
