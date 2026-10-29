import api from '@/lib/api'
import type {AcquisitionConfig, DataMemoryEntry, DataStore, SourceCatalog} from './types'
export const acquisitionApi = {
 planConfig:()=>api.get<{confirm:'when_needed'|'always'|'never'}>('/api/plans/config'),
 savePlanConfig:(confirm:'when_needed'|'always'|'never')=>api.put('/api/plans/config',{confirm}),
 memory:()=>api.get<{entries:DataMemoryEntry[]}>('/api/data-memory'),
 forget:(entry:DataMemoryEntry)=>api.delete('/api/data-memory?'+new URLSearchParams({guide:entry.guide||'',name:entry.name,category:entry.category||'',extent:entry.extent||''}).toString()),
 sources: () => api.get<SourceCatalog>('/api/data-sources'),
 store: () => api.get<DataStore>('/api/data-store'),
 deleteStored: (key: string) => api.delete(`/api/data-store/${encodeURIComponent(key)}?confirm=true`),
 cleanExpired: () => api.post<{deleted: string[]; retained_referenced: string[]}>('/api/data-store/clean-expired?confirm=true', {}),
 config: () => api.get<AcquisitionConfig>('/api/acquisition/config'),
 saveConfig: (value: AcquisitionConfig) => { const {reading_status, ...config} = value; void reading_status; return api.put('/api/acquisition/config', config) },
}
