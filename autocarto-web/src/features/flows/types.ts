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
export interface InputProblem { label: string; level: string; code: string; message: string; step?: string; acquirable?: boolean; item?: string; candidates?: string[] }
export interface AcquisitionChoice { $acquire?: boolean; $fill?: boolean; path?: string; source?: string; sources?: Record<string,string>; files?: Record<string,string> }
export interface DataSourceChoice { source_id:string;name:string;tier:number;license:string;estimated_mb?:number;cache_hit?:boolean;store_key?:string;acquired_at?:string;attribution?:string;needs_login?:boolean;derived_from?:string }
export interface AcquisitionItem {
 key:string;requirement:string;entry?:string;category:string;params:Record<string,string>;fields:Record<string,string>;input:string;input_type?:string;resource?:string;filename?:string;required:boolean;acquirable:boolean;available:boolean;mode:string;geometry?:string;user_path?:string
 chosen?:DataSourceChoice|null;alternatives:DataSourceChoice[];excluded:{source_id:string;name:string;reasons:string[]}[];reasons:string[];notes:string[];scale_denominator:number
 extent?:{bbox?:number[];path?:string;source?:string;region?:string;pending:boolean;dependency_step?:string;dependency_output?:string};extent_expression?:string;scale_expression?:string
}
export interface AcquisitionPlan {items:AcquisitionItem[]}
export interface InputCheckPlan {problems:InputProblem[];acquisition_plan?:AcquisitionPlan}
export interface FlowInput {
  acquirable?: boolean
  acquire_available?: boolean
  modes?: ('user'|'auto'|'fill')[]
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
  role_slot?: string
  role_source?: string
  required_packages?: string[]
  granted_packages?: string[]
  unresolved?: string
  acquisition_requirement?: string
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
  role_slots?: RoleSlot[]
  flow_roles?: FlowRole[]
  requirements?: DataRequirement[]
  acquisition_plan?: AcquisitionPlan
  auto_acquire?: 'ask'|'allow'|'deny'
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
  roles?: {scheme:number;augmented:number;flow_role:number;executor_default:number;unresolved:number;conditional_unresolved:number}
  data?: {requirements:number;items:number;acquirable:number;available:number}
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
export interface RoleResolution {source:string;role_id?:string;role_label?:string;granted?:string[];reason?:string;note?:string}
export interface RoleSlot {name:string;description?:string;packages:string[];executor?:string;needs_vision?:boolean;resolution:RoleResolution}
export interface FlowRole {id:string;slot:string;display_name:string;description?:string;kind:string;executor?:string;tool_packages:string[];provider?:string}
export interface SchemeRole {id:string;display_name:string;kind:string;executor?:string;enabled:boolean;is_main:boolean;tool_packages:string[]}
export interface RoleMappingResponse {flow_id:string;scheme:string;role_fill:string;role_mapping:Record<string,string>;role_slots:RoleSlot[];scheme_roles:SchemeRole[];flow_roles:FlowRole[]}
export interface DataRequirement {name:string;list?:string;category?:string;level?:string;extent:string;scale:string;params:Record<string,string>;fields:Record<string,string>;recommended_sources:string[];excluded_sources:string[];acquisition:string;line?:number}
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
