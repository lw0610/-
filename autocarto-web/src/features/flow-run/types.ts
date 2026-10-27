import type { AcquisitionPlan, FlowPlan } from '@/features/flows/types'

export interface AcquisitionItemState {
  key:string;status:string;source_id?:string;store_key?:string;bytes?:number;total?:number;stage?:string;notes?:string[];error?:string;required?:boolean;choices?:unknown[]
  output?:{paths?:string[];count?:number;extent?:unknown;layer_id?:string;layer_definition?:Record<string,unknown>};attempts?:unknown[]
}
export interface AcquisitionState {plan?:AcquisitionPlan;items:AcquisitionItemState[]}

export type ResourceResumeChoice = 'continue_snapshot' | 'restart'

export interface FlowSubmission {
  id: string
  inputs?: Record<string, unknown>
  resume_run_id?: string
  resume_resource_choice?: ResourceResumeChoice
  resource_fingerprint?: string
}
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
  acquisition?: AcquisitionState
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
export interface FlowPreview {
  execution_mode?: 'build' | 'plan'
  flow_id: string
  name?: string
  plan: FlowPlan
  diagnostics?: { level: string; message: string; code?: string }[]
  input_check?: { valid?: boolean; missing?: unknown[]; [key: string]: unknown }
}
