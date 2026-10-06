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
    reportClientIssue(`读取本地 UI 状态失败: ${key}`, error, '读取本地 UI 状态失败')
    return fallback
  }
}

function writeBool(key: string, value: boolean) {
  if (typeof window === 'undefined') return
  try {
    window.localStorage.setItem(key, value ? '1' : '0')
  } catch (error) {
    reportClientIssue(`写入本地 UI 状态失败: ${key}`, error, '写入本地 UI 状态失败')
  }
}

interface UIState {
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
  inspectorOpen: false,
  focusedWorkflowBlockId: null,
  sessionPanelCollapsed: readBool(STORAGE_KEYS.sessionPanelCollapsed, false),
  displaySessionId: null as string | null,

  setInspectorOpen: (inspectorOpen) => set({ inspectorOpen }),
  toggleInspectorOpen: () => set((s) => ({ inspectorOpen: !s.inspectorOpen })),
  setFocusedWorkflowBlockId: (focusedWorkflowBlockId) => set({ focusedWorkflowBlockId }),
  setSessionPanelCollapsed: (sessionPanelCollapsed) => {
    writeBool(STORAGE_KEYS.sessionPanelCollapsed, sessionPanelCollapsed)
    set({ sessionPanelCollapsed })
  },
  toggleSessionPanelCollapsed: () => set((s) => {
    const next = !s.sessionPanelCollapsed
    writeBool(STORAGE_KEYS.sessionPanelCollapsed, next)
    return { sessionPanelCollapsed: next }
  }),
  setDisplaySessionId: (displaySessionId) => set({ displaySessionId }),
}))
