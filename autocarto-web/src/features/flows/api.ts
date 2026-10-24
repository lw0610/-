import api from '@/lib/api'
import type {
  InputProblem,
  CaptureRequest,
  ProjectLayout,
  ProjectLayer,
  FlowDetail,
  FlowSummary,
  LibraryItem,
  ResolvedResource,
  ToolCapability,
  ResourceReference,
  ResourceTypeDefinition,
} from './types'
const path = (id: string) => `/api/flows/${encodeURIComponent(id)}`
const query = (scheme: string) => `?scheme=${encodeURIComponent(scheme)}`
export const flowApi = {
  list: (scheme: string) =>
    api.get<FlowSummary[]>(`/api/flows${query(scheme)}`),
  bindingStatus: () => api.get<{ orphaned_count: number }>('/api/flows/bindings/status'),
  get: (id: string, scheme: string) =>
    api.get<FlowDetail>(`${path(id)}${query(scheme)}`),
  create: (flow_md: string, id?: string) =>
    api.post<FlowDetail>('/api/flows', { flow_md, ...(id ? { id } : {}) }),
  save: (id: string, flow_md: string, scheme: string) =>
    api.put<FlowDetail>(`${path(id)}${query(scheme)}`, { flow_md }),
  remove: (id: string) => api.delete<{ deleted: boolean }>(path(id)),
  import: (source_path: string, overwrite: boolean, new_id?: string) =>
    api.post<{ flow: FlowDetail }>('/api/flows/import', {
      source_path,
      overwrite,
      ...(new_id ? { new_id } : {}),
    }),
  copy: (id: string, new_id: string, new_name: string) =>
    api.post<FlowDetail>(`${path(id)}/copy`, { new_id, new_name }),
  export: (id: string, target_path: string, include_data_files: boolean, include_library_deps = true) =>
    api.post<{ zip_path: string }>(`${path(id)}/export`, {
      target_path,
      include_data_files,
      include_library_deps,
    }),
  bind: (id: string, resource_key: string, base_dir: string) =>
    api.put(`${path(id)}/bindings`, { resource_key, base_dir }),
  inputMemory: (id: string) => api.get<Record<string, string>>(`${path(id)}/inputs/memory`),
  setInputMemory: (id: string, label: string, value: string) =>
    api.put(`${path(id)}/inputs/memory/${encodeURIComponent(label)}`, { value }),
  clearInputMemory: (id: string, label: string) =>
    api.delete(`${path(id)}/inputs/memory/${encodeURIComponent(label)}`),
  checkInputs: (id: string, scheme: string, values: Record<string, unknown>) =>
    api.post<InputProblem[]>(`${path(id)}/inputs/check`, { scheme, values }),
  adoptResource: (id: string, resource_id: string, label: string, scheme = 'default') =>
    api.post<FlowDetail>(`${path(id)}/resources/adopt${query(scheme)}`, { resource_id, label }),
  repairResource: (id: string, action: string, resource_key: string, resource_id: string | undefined, scheme: string) =>
    api.post<FlowDetail>(`${path(id)}/resources/repair${query(scheme)}`, {
      action, resource_key, ...(resource_id ? { resource_id } : {}),
    }),
  importResource: (id: string, source_path: string, target_subdir: string) =>
    api.post(`${path(id)}/resources/import`, {
      source_path,
      target_subdir:
        target_subdir.startsWith('resources/') || target_subdir === 'resources'
          ? target_subdir
          : `resources/${target_subdir}`,
    }),
  removeResource: (id: string, relative: string) =>
    api.delete(`${path(id)}/resources?path=${encodeURIComponent(relative)}`),
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
  retired: () => api.get<LibraryItem[]>('/api/resources?scope=retired'),
  types: () => api.get<ResourceTypeDefinition[]>('/api/resources/types'),
  get: (id: string) =>
    api.get<ResolvedResource>(`/api/resources/by-id/${encodeURIComponent(id)}`),
  import: (source_path: string, name?: string) =>
    api.post<ResolvedResource>('/api/resources/import', { source_path, ...(name?.trim() ? { name: name.trim() } : {}) }),
  copy: (id: string, override: boolean, name?: string) =>
    api.post<ResolvedResource>(`${resourceIDPath(id)}/copy`, { override, ...(name?.trim() ? { name: name.trim() } : {}) }),
  replace: (id: string, source_path: string) =>
    api.put<ResolvedResource>(`${resourceIDPath(id)}/content`, { source_path }),
  updateAliases: (id: string, entry_aliases: Record<string, string>) =>
    api.put<ResolvedResource>(`${resourceIDPath(id)}/content`, { entry_aliases }),
  capture: (request: CaptureRequest) => api.post<ResolvedResource>('/api/resources/capture', request),
  layouts: (instance: string) => api.get<{ layouts: ProjectLayout[] }>(`/api/instances/${encodeURIComponent(instance)}/layouts`),
  layers: (instance: string) => api.get<{ items: ProjectLayer[] }>(`/api/instances/${encodeURIComponent(instance)}/layers`),
  references: (id: string) =>
    api.get<ResourceReference[]>(`${resourceIDPath(id)}/references`),
  remove: (id: string, force = false) => api.delete(`${resourceIDPath(id)}?force=${force}`),
  upgrade: (id: string) => api.post<ResolvedResource>(`${resourceIDPath(id)}/upgrade`, {}),
  bind: (binding_key: string, base_dir: string) =>
    api.put('/api/resources/bindings', { binding_key, base_dir }),
}

export async function loadResourcePreview(
  id: string,
  entry?: number,
): Promise<Blob> {
  const { useConnectionStore } = await import('@/stores/connectionStore')
  const { buildControlContractHeaders } = await import('@/lib/api')
  const base = useConnectionStore.getState().backendUrl.replace(/\/$/, '')
  const response = await fetch(
    `${base}/api/resources/by-id/${encodeURIComponent(id)}/preview${entry == null ? '' : `?entry=${entry}`}`,
    {
      headers: buildControlContractHeaders(),
      signal: AbortSignal.timeout(15000),
    },
  )
  if (!response.ok) throw new Error('资源预览不可用')
  return response.blob()
}
