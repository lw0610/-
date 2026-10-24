export interface Diagnostic {
  level: 'error' | 'warning' | 'info'
  code: string
  message: string
  step?: string
  resource?: string
  line?: number
  fix?: {
    action: string
    actions?: string[]
    role?: string
    package?: string
    resource?: string
    resource_key?: string
    resource_id?: string
    actual_type?: string
    candidates?: ResourceCandidate[]
    [key: string]: unknown
  }
}
export interface ResourceCandidate {
  id: string
  name: string
  type: string
  owner?: string
  version?: string
}
export interface ResourceEntry {
  name: string
  path: string
  kind: string
  exists: boolean | null
  exists_unknown?: boolean
  extra?: Record<string, string>
  fields?: Record<string, unknown>
}
export interface ResourceTypeDefinition {
  public?: boolean
  type: string
  label: string
  words: string[]
  group: string
  folder: boolean
  entry_extensions: string[]
}
export interface ResourceIdentity {
  id?: string
  name?: string
  version?: string
  digest?: string
  owner?: string
  overrides?: string
  overridden_by?: string
  legacy?: boolean
  root?: string
  entry?: string
  format?: string
  description?: string
  source?: { author?: string; license?: string; upstream?: string }
  problems?: Diagnostic[]
}
export interface ResourceReference {
  label?: string
  flow_id: string
  flow_name: string
  source: 'builtin' | 'user'
  via: 'lock' | 'name'
}
export interface ResolvedResource extends ResourceIdentity {
  key: string
  label: string
  type: string
  location: string
  origin?: string
  base_dir_from_input?: string
  base_dir?: string
  binding_key?: string
  base_dir_binding?: string
  entries: ResourceEntry[]
  problems?: Diagnostic[]
}
export interface LibraryItem extends ResourceIdentity {
  references?: ResourceReference[]
  name: string
  type: string
  origin: string
  path: string
  size: number
}
export interface InputProblem { label: string; level: string; code: string; message: string }
export interface FlowInput {
  remember?: boolean
  key: string
  label: string
  type: string
  required: boolean
  default?: unknown
  options?: string[]
  description?: string
}
export type PlanStatus = 'ready' | 'enriching' | 'partial' | 'basic'
export interface StepFailure { step_id: string; title: string; reason: string }
export interface EnrichmentStatus {
  done: number; total: number; job_id?: string; job_state?: string
  failed?: StepFailure[]; updated_at?: string
}
export interface FlowStep {
  enrichment?: 'ready' | 'pending' | 'failed' | 'none' 
  id: string
  index: number
  title: string
  role?: string
  role_label?: string
  kind: string
  instructions: string
  condition?: { text: string }
  suggested_tools?: string[]
  expected_outputs?: {
    label: string
    type: string
    format?: string
    required: boolean
  }[]
  refs?: { resources?: string[] }
  on_failure: string
}
export interface FlowPlan {
  plan_status?: PlanStatus
  enrichment?: EnrichmentStatus
  api_version: string
  flow_id: string
  name: string
  scheme: string
  compile_key?: string
  link_fingerprint?: string
  linked_at?: string
  inputs: FlowInput[]
  input_constraints?: { op: string; inputs: string[] }[]
  steps: FlowStep[]
  resources: ResolvedResource[]
  diagnostics?: Diagnostic[]
}
export interface FlowSummary {
  id: string
  name: string
  description: string
  version: string
  source: 'builtin' | 'user'
  updated_at: string
  enabled: boolean
  plan_status?: PlanStatus
  diagnostics_summary: { errors: number; warnings: number; infos: number }
}
export interface FlowDetail {
  meta: { id: string; name: string; description?: string; version: string }
  source: 'builtin' | 'user'
  flow_md: string
  resources: { path: string; type: string; size: number; exists: boolean }[]
  plan: FlowPlan | null
  diagnostics: Diagnostic[]
}
export interface ToolCapability {
  name: string
  display_name?: string
  description?: string
  keywords?: string[]
  package_id?: string
  package_display_name?: string
}

export interface CaptureRequest {
  instance_id: string; kind: 'layout' | 'style' | 'style_set'; name: string
  layout?: string; main_map_item?: string; layers?: string[]; replace_id?: string
}
export interface ProjectLayout {
  name: string; map_items: { id: string; uuid?: string; name?: string }[]
}
export interface ProjectLayer { id: string; name: string; type?: string; visible?: boolean }
