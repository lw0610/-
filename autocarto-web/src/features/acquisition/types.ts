export interface DataCategory { id: string; name: string; fields?: Record<string, {meaning: string; required?: boolean}>; params?: Record<string, {values?: string[]; required?: boolean}> }
export interface DataProvide { category: string; path?: string; layer?: string; fields?: Record<string, string[]>; params?: Record<string, string> }
export interface DataSource { api_version: string; id: string; name: string; kind: string; root?: string; compliance?: string; license?: string; attribution?: string; provides: DataProvide[]; builtin: boolean; available?: boolean; reasons?: string[]; notes?: string[]; requires?: string[] }
export interface DraftEntry {name: string; path: string; layer?: string; kind: string; exists: boolean; category: string; fields: Record<string, string[]>; params?: Record<string, string>}
export interface LocalDraft {source: DataSource; entries: DraftEntry[]; messages: string[]}
export interface SourceCatalog {sources: DataSource[]; categories: DataCategory[]; diagnostics: {code: string; message: string; source_id?: string}[]}
export interface AcquisitionConfig { max_download_mb: number; store_max_gb: number; concurrency: number; per_host: number; auto_fallback: boolean; web_search_enabled: boolean; overpass_endpoint: string }
export interface StoreItem {key: string; spec: {source_id: string; category: string; bbox?: number[]}; created_at: string; expires_at?: string; bytes: number; valid: boolean; expired: boolean; directory: string; referencing_runs: string[]}
export interface DataStore {items: StoreItem[]; used_bytes: number; max_bytes: number; directory: string}
