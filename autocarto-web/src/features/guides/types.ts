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
  geometry?: string
  label_field?: string
  paper?: string
  orientation?: string
  elements?: string[]
  tags?: string[]
  preview_url?: string
  preview_status?: 'ready' | 'pending' | 'failed'
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
export type ResourceWriteResult = ResolvedResource | { items: ResolvedResource[]; warnings?: string[] }
// Read-only legacy run snapshots still carry their original plan shape.
export interface AcquisitionItem { key:string;requirement:string;entry?:string;extent?:{pending:boolean};chosen?:{source_id:string;name:string} }
export interface AcquisitionPlan {items:AcquisitionItem[]}
export interface FlowPlan { name:string;steps:{id:string;role_label?:string;kind?:string;acquisition_requirement?:string}[] }
export interface ToolCapability {
  name: string
  display_name?: string
  description?: string
  keywords?: string[]
  package_id?: string
  package_display_name?: string
}

export interface CaptureRequest {
  instance_id: string; kind: 'layout' | 'style'; name: string
  layout?: string; main_map_item?: string; layers?: string[]; replace_id?: string
}
export interface ProjectLayout {
  name: string; map_items: { id: string; uuid?: string; name?: string }[]
}
export interface ProjectLayer { id: string; name: string; type?: string; visible?: boolean }

export interface ConversionReport { converted: boolean; message: string; items: string[] }
export interface GuideSummary { id: string; name: string; description: string; version: string; tags?: string[]; source: 'builtin' | 'user'; enabled: boolean; updated_at: string; conversion_report?: ConversionReport }
export interface GuideDoc {
  meta?: {id?: string; name?: string; description?: string; version?: string}
  body: string
  resources: {label: string; type: string; location: string}[]
  diagnostics: Diagnostic[]
}
export interface GuideDetail { meta: {id: string; name: string; description?: string; version: string}; source: 'builtin' | 'user'; enabled: boolean; guide_md: string; guide: GuideDoc; resources: {path: string; type: string; size: number; exists: boolean}[]; resolved_resources?: ResolvedResource[]; diagnostics: Diagnostic[]; conversion_report?: ConversionReport; updated_at: string }
