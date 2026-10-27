import api, { buildControlContractHeaders } from '@/lib/api'
import { useConnectionStore } from '@/stores/connectionStore'
import { invoke } from '@tauri-apps/api/core'
import { isDesktopRuntime } from '@/lib/config-form-helpers'
export interface SessionFile {
  id: string
  path: string
  name: string
  kind: string
  group: string
  members: string[]
  size: number
  registered_at: string
  modified_at: string
  exists: boolean
  origin: {
    type: string
    label?: string
    tool?: string
    call_id?: string
    agent?: string
    source_id?: string
    license?: string
  }
  flow_run_id?: string
  step_id?: string
}
export const groupLabels: Record<string, string> = {
  deliverable: '成果',
  data: '数据',
  source: '下载数据',
  flow: '流程运行',
  intermediate: '中间文件',
  other: '其他',
}
export const kindLabels: Record<string, string> = {
  image: '图片',
  pdf: 'PDF',
  vector: '矢量',
  raster: '栅格',
  table: '表格',
  text: '文本',
  json: 'JSON',
  qgis_style: '样式',
  qgis_layout: '版面',
  archive: '压缩包',
  run: '运行记录',
  folder: '数据集',
  other: '文件',
}
export function filesPath(sid: string, project: string, suffix = '') {
  return `/api/sessions/${encodeURIComponent(sid)}/files${suffix}?project_path=${encodeURIComponent(project)}`
}
export function contentPath(sid: string, project: string, item: SessionFile) {
  return filesPath(sid, project, `/${item.id}/content`)
}
export function backendURL(path: string) {
  return `${useConnectionStore.getState().backendUrl.replace(/\/$/, '')}${path}`
}
export const fileApi = {
  list: (sid: string, project: string, all: boolean) =>
    api.get<{ items: SessionFile[]; session_dir: string }>(
      `${filesPath(sid, project)}&all=${all ? 1 : 0}`,
    ),
  action: <T = Record<string, unknown>>(
    sid: string,
    project: string,
    item: SessionFile,
    action: string,
    layer_name?: string,
  ) =>
    api.post<T>(
      filesPath(sid, project, `/${item.id}/${action}`),
      layer_name ? { layer_name } : {},
      { timeoutMs: 50000 },
    ),
}
export async function textPreview(path: string, signal: AbortSignal) {
  const response = await fetch(backendURL(path), {
    headers: buildControlContractHeaders(),
    signal,
  })
  if (!response.ok) {
    const body = await response.json().catch(() => ({}))
    throw new Error(
      typeof body.error === 'string'
        ? body.error
        : body.error?.message || '文件无法预览',
    )
  }
  return {
    text: await response.text(),
    truncated: response.headers.get('X-Preview-Truncated') === '1',
  }
}
export async function localFileAction(
  path: string,
  action: 'open' | 'reveal' | 'copy',
) {
  if (action !== 'copy' && isDesktopRuntime()) {
    await invoke(action === 'open' ? 'open_local_path' : 'reveal_local_path', {
      path,
    })
    return ''
  }
  if (!navigator.clipboard?.writeText)
    throw new Error(`浏览器无法自动复制，请手工复制路径：${path}`)
  await navigator.clipboard.writeText(path)
  return '路径已复制'
}
export function fileSize(bytes: number) {
  return bytes < 1024
    ? `${bytes} B`
    : bytes < 1024 ** 2
      ? `${(bytes / 1024).toFixed(1)} KB`
      : `${(bytes / 1024 ** 2).toFixed(1)} MB`
}
