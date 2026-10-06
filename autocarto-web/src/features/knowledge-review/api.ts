import api from '@/lib/api'
import { useConnectionStore } from '@/stores/connectionStore'
import type { KnowledgeGraph, ReviewDocument, ReviewReference } from './model'

export interface CalloutImportResult {
  review: ReviewDocument
  imported_count: number
  updated_count: number
  image_count: number
  new_place_count: number
  warnings: string[]
}

function path(ref: ReviewReference) {
  return `/api/sessions/${encodeURIComponent(ref.session_id)}/knowledge-reviews/${encodeURIComponent(ref.review_id)}`
}
export const reviewApi = {
  get: (ref: ReviewReference) => api.get<ReviewDocument>(path(ref)),
  save: (ref: ReviewReference, base_revision: number, graph: KnowledgeGraph) => api.put<ReviewDocument>(path(ref), { base_revision, graph }),
  geocode: (ref: ReviewReference, base_revision: number) => api.post<ReviewDocument>(`${path(ref)}/geocode`, { base_revision }, { timeoutMs: 300_000 }),
  confirm: (ref: ReviewReference, base_revision: number, request_id: string) => api.post<ReviewDocument>(`${path(ref)}/confirm`, { base_revision, request_id }),
  importAsset: (ref: ReviewReference, source_path: string) => api.post<{ image_path: string; preview_url: string }>(`${path(ref)}/assets`, { source_path }),
  importCallouts: (ref: ReviewReference, base_revision: number, source_path: string, graph: KnowledgeGraph, signal?: AbortSignal) => api.post<CalloutImportResult>(`${path(ref)}/callouts/import`, { base_revision, source_path, graph }, { timeoutMs: 600_000, ...(signal ? { signal } : {}) }),
}
export function assetPreviewURL(value: string): string {
  // Only display assets served by this backend; never interpret arbitrary local or remote URLs.
  if (!value.startsWith('/api/sessions/') || value.startsWith('//')) return ''
  const baseURL = useConnectionStore.getState().backendUrl
  return `${baseURL.replace(/\/$/, '')}${value}`
}
