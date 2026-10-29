import api from '@/lib/api'
import type { CaptureRequest, ProjectLayout, ProjectLayer, LibraryItem, ResolvedResource, ResourceWriteResult, ToolCapability, ResourceReference, ResourceTypeDefinition, GuideDetail, GuideSummary, ConversionReport } from './types'
const path = (id: string) => `/api/guides/${encodeURIComponent(id)}`
export const guideApi = {
  list: () => api.get<GuideSummary[]>('/api/guides'),
  get: (id: string) => api.get<GuideDetail>(path(id)),
  create: (guide_md: string, id?: string) => api.post<GuideDetail>('/api/guides', { guide_md, id }),
  save: (id: string, guide_md: string) => api.put<GuideDetail>(path(id), { guide_md }),
  remove: (id: string) => api.delete(path(id)),
  setEnabled: (id: string, enabled: boolean) => api.put<GuideDetail>(`${path(id)}/enabled`, { enabled }),
  import: (source_path: string, overwrite: boolean, new_id?: string) => api.post<{guide: GuideDetail; report?: ConversionReport}>('/api/guides/import', { source_path, overwrite, new_id }),
  copy: (id: string, new_id: string, new_name?: string) => api.post<GuideDetail>(`${path(id)}/copy`, { new_id, new_name }),
  export: (id: string, target_path: string, include_data_files: boolean, include_library_deps = true) => api.post<{zip_path: string}>(`${path(id)}/export`, { target_path, include_data_files, include_library_deps }),
  capabilities: async (): Promise<ToolCapability[]> => {
    const [catalog, packages] = await Promise.allSettled([
      api.getToolCatalog('global'),
      api.get<{
        packages?: {
          package_id?: string
          name?: string
          display_name?: string
        }[]
      }>('/api/skill-packages?scope=all'),
    ])
    if (catalog.status === 'rejected') throw catalog.reason
    const labels = new Map(
      packages.status === 'fulfilled'
        ? (packages.value.packages || []).map((item) => [
            item.package_id || item.name,
            item.display_name,
          ])
        : [],
    )
    return catalog.value.tool_infos
      .map((item) => {
        const label = labels.get(item.package_id)
        return {
          ...item,
          name: item.name,
          ...(label ? { package_display_name: label } : {}),
        }
      })
      .filter((item) => item.name)
  },
}
const resourceIDPath = (id: string) => `/api/resources/${encodeURIComponent(id)}`
export const resourceApi = {
  list: () => api.get<LibraryItem[]>('/api/resources?scope=all'),
  types: () => api.get<ResourceTypeDefinition[]>('/api/resources/types'),
  get: (id: string) =>
    api.get<ResolvedResource>(`/api/resources/by-id/${encodeURIComponent(id)}`),
  import: (source_path: string, name?: string) =>
    api.post<ResourceWriteResult>('/api/resources/import', { source_path, ...(name?.trim() ? { name: name.trim() } : {}) }),
  copy: (id: string, override: boolean, name?: string) =>
    api.post<ResolvedResource>(`${resourceIDPath(id)}/copy`, { override, ...(name?.trim() ? { name: name.trim() } : {}) }),
  replace: (id: string, source_path: string) =>
    api.put<ResolvedResource>(`${resourceIDPath(id)}/content`, { source_path }),
  capture: (request: CaptureRequest) => api.post<ResourceWriteResult>('/api/resources/capture', request),
  update: (id: string, metadata: {name: string; description: string; tags: string[]}) => api.patch<ResolvedResource>(`/api/resources/by-id/${encodeURIComponent(id)}`, metadata),
  regeneratePreview: (id: string) => api.post<ResolvedResource>(`/api/resources/by-id/${encodeURIComponent(id)}/preview`, {}),
  export: (id: string, target_path: string) => api.post<{path?: string; zip_path?: string}>(`/api/resources/by-id/${encodeURIComponent(id)}/export`, { target_path }),
  layouts: (instance: string) => api.get<{ layouts: ProjectLayout[] }>(`/api/instances/${encodeURIComponent(instance)}/layouts`),
  layers: (instance: string) => api.get<{ items: ProjectLayer[] }>(`/api/instances/${encodeURIComponent(instance)}/layers`),
  references: (id: string) =>
    api.get<ResourceReference[]>(`${resourceIDPath(id)}/references`),
  remove: (id: string, force = false) => api.delete(`${resourceIDPath(id)}?force=${force}`),
}

async function fetchPreviewImage(path: string, init: RequestInit, signal: AbortSignal | undefined, timeoutMs: number): Promise<Blob> {
  const { useConnectionStore } = await import('@/stores/connectionStore')
  const { buildControlContractHeaders } = await import('@/lib/api')
  const base = useConnectionStore.getState().backendUrl.replace(/\/$/, '')
  const controller = new AbortController()
  const abort = () => controller.abort(signal?.reason)
  if (signal?.aborted) abort()
  else signal?.addEventListener('abort', abort, {once: true})
  let timedOut = false
  const timeout = setTimeout(() => { timedOut = true; controller.abort() }, timeoutMs)
  try {
    const response = await fetch(`${base}${path}`, {...init, headers: buildControlContractHeaders(init.headers), signal: controller.signal})
    if (!response.ok) {
      const body = await response.json().catch(() => null) as {error?: string | {message?: string}; message?: string} | null
      const message = typeof body?.error === 'string' ? body.error : body?.error?.message || body?.message
      throw new Error(message || `预览生成失败（${response.status}）`)
    }
    const blob = await response.blob()
    if (!blob.type.startsWith('image/')) throw new Error('服务未返回有效预览图片')
    return blob
  } catch (reason) {
    if (timedOut && !signal?.aborted) throw new Error('预览请求超时，请稍后重试。')
    throw reason
  } finally {
    clearTimeout(timeout)
    signal?.removeEventListener('abort', abort)
  }
}

export function loadResourcePreview(id: string, entry?: number, signal?: AbortSignal): Promise<Blob> {
  return fetchPreviewImage(`/api/resources/by-id/${encodeURIComponent(id)}/preview${entry == null ? '' : `?entry=${entry}`}`, {}, signal, 15000)
}

export function loadStyleLayerPreview(id: string, instanceId: string, layerId: string, signal?: AbortSignal): Promise<Blob> {
  return fetchPreviewImage(`/api/resources/by-id/${encodeURIComponent(id)}/preview-layer`, {
    method: 'POST', body: JSON.stringify({instance_id: instanceId, layer_id: layerId}),
  }, signal, 60000)
}

const layerPath = (id: string) => `/api/layers/${encodeURIComponent(id)}`
export const layerApi = {
  list: () => api.get<import('./types').LayerCatalog>('/api/layers'),
  scan: (path: string, pack?: string) => api.post<import('./types').LayerScan>('/api/layers/import/scan', {path, pack}, {timeoutMs: 120000}),
  import: (pack: string, entries: import('./types').LayerManifest[]) => api.post<{items: import('./types').LayerItem[]}>('/api/layers/import/confirm', {pack, entries}, {timeoutMs: 120000}),
  update: (id: string, edit: Partial<import('./types').LayerManifest>) => api.patch<import('./types').LayerItem>(layerPath(id), edit),
  remove: (id: string) => api.delete(layerPath(id)),
  regeneratePreview: (id: string) => api.post<import('./types').LayerItem>(`${layerPath(id)}/preview`, {}, {timeoutMs: 60000}),
}
export function loadLayerPreview(id: string, signal?: AbortSignal): Promise<Blob> {
  return fetchPreviewImage(`${layerPath(id)}/preview`, {}, signal, 60000)
}
