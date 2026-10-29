import api from '@/lib/api'
import type { DataFindResult, PlanData, PlanSnapshot } from './types'
const path = (session: string, plan: string) => `/api/sessions/${encodeURIComponent(session)}/plans/${encodeURIComponent(plan)}`
export const planApi = {
  list: (session: string) => api.get<{ plans: PlanSnapshot[] }>(`/api/sessions/${encodeURIComponent(session)}/plans`),
  start: (snapshot: PlanSnapshot, resume?: { resume_run_id: string; resume_resource_choice?: 'continue_snapshot'; resource_fingerprint?: string }) => api.post<PlanSnapshot>(`${path(snapshot.session_id, snapshot.plan_id)}/start`, { version: snapshot.plan_version, ...resume }),
  cancel: (snapshot: PlanSnapshot) => api.post<PlanSnapshot>(`${path(snapshot.session_id, snapshot.plan_id)}/cancel`, { version: snapshot.plan_version }),
  editData: (snapshot: PlanSnapshot, data_id: string, edit: Partial<PlanData>) => api.post<PlanSnapshot>(`${path(snapshot.session_id, snapshot.plan_id)}/edits`, { version: snapshot.plan_version, data_id, ...edit }),
  failure: (snapshot: PlanSnapshot, action: 'replan' | 'provide_data' | 'skip' | 'stop', options?: { path?: string; data_id?: string }) => api.post<PlanSnapshot>(`${path(snapshot.session_id, snapshot.plan_id)}/failure`, { version: snapshot.plan_version, action, ...options }),
  find: (session: string, id: string) => api.get<DataFindResult>(`/api/sessions/${encodeURIComponent(session)}/data/find/${encodeURIComponent(id)}`),
  config: () => api.get<{ confirm: 'when_needed' | 'always' | 'never' }>('/api/plans/config'),
  saveConfig: (confirm: 'when_needed' | 'always' | 'never') => api.put('/api/plans/config', { confirm }),
}
