import { create } from 'zustand'

import { reportClientIssue } from '@/lib/clientDiagnostics'

const STORAGE_KEYS = {
  sessionPanelCollapsed: 'autocarto_session_panel_collapsed',
  inspectorOpen: 'autocarto_inspector_open',
} as const

function readBool(key: string, fallback: boolean): boolean {
  if (typeof window === 'undefined') return fallback
  try {
    const raw = window.localStorage.getItem(key)
    if (raw === '1') return true
    if (raw === '0') return false
    return fallback
  } catch (error) {
    reportClientIssue(
      `读取本地 UI 状态失败: ${key}`,
      error,
      '读取本地 UI 状态失败',
    )
    return fallback
  }
}

function writeBool(key: string, value: boolean) {
  if (typeof window === 'undefined') return
  try {
    window.localStorage.setItem(key, value ? '1' : '0')
  } catch (error) {
    reportClientIssue(
      `写入本地 UI 状态失败: ${key}`,
      error,
      '写入本地 UI 状态失败',
    )
  }
}

export type WorkspaceTab = 'files' | 'tasks' | 'process'
export interface WorkspaceState {
  open: boolean
  tab: WorkspaceTab
  width: number
  previewWidth: number
}
const initialWorkspace: WorkspaceState = {
  open: false,
  tab: 'files',
  width: 360,
  previewWidth: 520,
}
function readSaved<T>(key: string, fallback: T): T {
  if (typeof window === 'undefined') return fallback
  try {
    return JSON.parse(window.localStorage.getItem(key) || 'null') || fallback
  } catch {
    return fallback
  }
}
function save(key: string, value: unknown) {
  if (typeof window !== 'undefined') {
    try {
      window.localStorage.setItem(key, JSON.stringify(value))
    } catch (error) {
      reportClientIssue('保存工作区状态失败', error, '保存工作区状态失败')
    }
  }
}
const savedWorkspace = readSaved<WorkspaceState>(
  'autocarto_workspace',
  initialWorkspace,
)
const workspace: WorkspaceState = {
  open: savedWorkspace.open === true,
  tab: ['files', 'tasks', 'process'].includes(savedWorkspace.tab)
    ? savedWorkspace.tab
    : 'files',
  width: Math.max(300, Math.min(560, Number(savedWorkspace.width) || 360)),
  previewWidth: Math.max(420, Number(savedWorkspace.previewWidth) || 520),
}
interface UIState {
  workspace: WorkspaceState
  filesViewedAt: Record<string, number>
  highlightedFileIds: string[]
  setWorkspace: (value: Partial<WorkspaceState>) => void
  openWorkspace: (tab: WorkspaceTab, fileIds?: string[]) => void
  markFilesViewed: (sessionId: string, timestamp?: number) => void

  inspectorOpen: boolean
  focusedWorkflowBlockId: string | null
  sessionPanelCollapsed: boolean
  displaySessionId: string | null

  setInspectorOpen: (open: boolean) => void
  toggleInspectorOpen: () => void
  setFocusedWorkflowBlockId: (id: string | null) => void
  setSessionPanelCollapsed: (collapsed: boolean) => void
  toggleSessionPanelCollapsed: () => void
  setDisplaySessionId: (id: string | null) => void
}

export const useUIStore = create<UIState>((set) => ({
  workspace,
  filesViewedAt: readSaved<Record<string, number>>(
    'autocarto_files_viewed',
    {},
  ),
  highlightedFileIds: [],
  setWorkspace: (value) =>
    set((state) => {
      const workspace = { ...state.workspace, ...value }
      save('autocarto_workspace', workspace)
      return { workspace, inspectorOpen: workspace.open }
    }),
  openWorkspace: (tab, highlightedFileIds = []) =>
    set((state) => {
      const workspace = { ...state.workspace, open: true, tab }
      save('autocarto_workspace', workspace)
      return { workspace, inspectorOpen: true, highlightedFileIds }
    }),
  markFilesViewed: (sessionId, timestamp = Date.now()) =>
    set((state) => {
      const filesViewedAt = { ...state.filesViewedAt, [sessionId]: timestamp }
      save('autocarto_files_viewed', filesViewedAt)
      return { filesViewedAt }
    }),
  inspectorOpen: workspace.open,
  focusedWorkflowBlockId: null,
  sessionPanelCollapsed: readBool(STORAGE_KEYS.sessionPanelCollapsed, false),
  displaySessionId: null as string | null,

  setInspectorOpen: (inspectorOpen) =>
    set((state) => {
      const workspace = { ...state.workspace, open: inspectorOpen }
      save('autocarto_workspace', workspace)
      return { inspectorOpen, workspace }
    }),
  toggleInspectorOpen: () =>
    set((s) => {
      const workspace = { ...s.workspace, open: !s.workspace.open }
      save('autocarto_workspace', workspace)
      return { workspace, inspectorOpen: workspace.open }
    }),
  setFocusedWorkflowBlockId: (focusedWorkflowBlockId) =>
    set({ focusedWorkflowBlockId }),
  setSessionPanelCollapsed: (sessionPanelCollapsed) => {
    writeBool(STORAGE_KEYS.sessionPanelCollapsed, sessionPanelCollapsed)
    set({ sessionPanelCollapsed })
  },
  toggleSessionPanelCollapsed: () =>
    set((s) => {
      const next = !s.sessionPanelCollapsed
      writeBool(STORAGE_KEYS.sessionPanelCollapsed, next)
      return { sessionPanelCollapsed: next }
    }),
  setDisplaySessionId: (displaySessionId) => set({ displaySessionId }),
}))
