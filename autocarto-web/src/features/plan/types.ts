import type { AcquisitionPlan, FlowPlan } from '@/features/guides/types'

export interface PlanOutput { label: string; type: string; format?: string; required?: boolean }
export interface PlanData {
  id: string; name: string; from?: 'conversation' | 'user' | 'project' | 'session' | 'step' | 'acquire'
  kind?: 'file' | 'dataset' | 'layer' | 'table'; find_id?: string; choice?: string | Record<string, string>; path?: string; layer?: string; file_id?: string; step?: string; output?: string
  layer_name?: string; style?: string; geometry?: string; label_field?: string
  source?: 'user' | 'project' | 'builtin' | 'retrieve'; ref?: string; method?: string
  need?: {category?: string; fields?: string[]; description?: string; params?: Record<string, unknown>; [key: string]: unknown}
  extent?: {region?: string; bbox?: number[]; from?: string; expand?: number}
}
export interface PlanStep {
  id: string; title: string; kind?: 'agent' | 'acquire' | 'confirm'; capabilities?: string[]; parameters?: Record<string, string>; uses?: string[]; data?: string[]
  inputs?: string[]; produces?: {id: string; kind: string; format?: string}[]
  layers?: string[]; guide_sections?: string[]; instructions?: string; outputs?: PlanOutput[]; depends_on?: string[]; optional?: boolean
}
export interface CartoPlan {
  api_version: 'carto-plan/v1' | 'carto-plan/v2'; plan_id: string; version: number; goal: string
  guide?: string | { id: string; name?: string; version: string } | null
  facts: { name: string; value: unknown; from: string }[]
  assumptions: { text: string; impact: 'low' | 'high' }[]
  data: PlanData[]; steps: PlanStep[]; deliverables: (PlanOutput | string)[]; checks: string[]
}
export interface DataCandidate { id: string; kind: string; name: string; source_id?: string; available: boolean; reasons: string[]; notes?: string[]; handbook?: string; path?: string; layer_id?: string; geometry?: string; confidence?: 'high' | 'confirm'; requires_confirmation?: boolean; match_reason?: string; sublayer?: string; tier?: number; network?: boolean }
export interface DataFindRow {
  row: string; category: string; recommended?: string; candidates: DataCandidate[] | null; excluded?: { source_id: string; reason: string }[]
}
export interface DataFindResult {
  find_id?: string; rows: DataFindRow[]
  diagnostics?: { code: string; message: string }[]; fallback?: DataCandidate[]; created_at?: string; expires_at?: string
}
export interface PlanSnapshot {
  session_id: string; plan_id: string; plan_version: number; plan: CartoPlan
  status: 'interrupted' | 'needs_data' | 'draft' | 'awaiting_confirmation' | 'ready' | 'running' | 'needs_replan' | 'waiting_user' | 'completed' | 'completed_with_issues' | 'failed' | 'cancelled' | 'preview'
  replanning?: boolean; needs_confirmation: boolean; confirmation_reasons?: string[]; execution_mode: 'plan' | 'build'
  precheck?: PlanPrecheck[]; notes?: string[]
  find_results?: Record<string, DataFindResult>; run?: FlowRunState; created_at?: string; updated_at?: string
}

export interface PlanPrecheck {data_id: string; status: 'available' | 'confirm' | 'unavailable'; reason?: string; suggestion?: string; source?: string; ref?: string; pack?: string; methods?: string[]; selection?: string; estimated_seconds?: number}

export interface AcquisitionLedgerEntry {method: string; params?: Record<string, unknown>; started_at: string; finished_at?: string; result: string; reason?: string; paths?: string[]}
export interface DataPreparation {started_at?: string | null; data_id: string; name: string; status: string; reason?: string; ledger?: AcquisitionLedgerEntry[]}

export interface AcquisitionItemState {
  key:string;status:string;source_id?:string;store_key?:string;bytes?:number;total?:number;percent?:number;bytes_per_second?:number;message?:string;stage?:string;notes?:string[];error?:string;required?:boolean;choices?:unknown[]
  output?:{paths?:string[];count?:number;extent?:unknown;layer_id?:string;layer_definition?:Record<string,unknown>};attempts?:unknown[]
}
export interface AcquisitionState {plan?:AcquisitionPlan;items:AcquisitionItemState[]}

export type ResourceResumeChoice = 'continue_snapshot'

export interface FlowReceipt {
  status: string
  summary: string
  outputs: Record<string, unknown>
  issues: string[]
}
export interface FlowAttempt {
  run_id: string
  mode?: string
  started_at: string
  finished_at?: string
  receipt?: FlowReceipt
  verification?: { ok: boolean; errors: string[] }
  user_choice?: { choice: string; guidance?: string }
  [key: string]: unknown
}
export interface FlowStepState {
  id: string
  index: number
  title: string
  role: string
  status: string
  attempts: FlowAttempt[]
  outputs: Record<string, unknown>
  condition?: { evaluated?: boolean; result?: boolean; by?: string; reason?: string }
  last_failure?: string
  awaiting_choice?: boolean
  [key: string]: unknown
}
// The wire names match internal/flowrun.RunState. Optional fields are absent
// until their event arrives; a session snapshot supplies the complete state.
export interface FlowRunState {
  plan_id?: string
  plan_version?: number
  carto_plan?: CartoPlan
  source_notes?: string[]
  checks?: {name?: string; text?: string; passed?: boolean; status?: string; message?: string}[]
  acquisition?: AcquisitionState
  data_preparation?: DataPreparation[]
  resource_change?: { fingerprint: string; message: string } | null
  api_version: string
  flow_run_id: string
  session_id: string
  flow_id: string
  status: string
  started_at: string
  finished_at?: string
  inputs?: Record<string, unknown>
  cursor?: number
  steps: FlowStepState[]
  result: { deliverables: Record<string, unknown>; issues: string[]; summary: string }
  run_json_path?: string
  plan?: FlowPlan | null
  name?: string
  [key: string]: unknown
}
export interface FlowEvent {
  event: string
  session_id: string
  flow_run_id?: string
  payload: Record<string, unknown>
}
export interface FlowBlocked {
  flow_id: string
  reasons: { code?: string; message: string }[]
}
