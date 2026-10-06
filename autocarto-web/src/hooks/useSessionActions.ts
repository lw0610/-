'use client'

import { usePathname, useRouter } from 'next/navigation'
import { useCallback, useState } from 'react'
import api, { SessionRecord } from '@/lib/api'
import { getGlobalPersistenceController } from '@/hooks/useGlobalPersistence'
import { sanitizeFileName } from '@/lib/session-utils'
import wsClient, { getInstanceSelectRollbackTarget } from '@/lib/websocket'
import type { QGISInstance } from '@/types/instance'
import { useChatStore } from '@/stores/chatStore'
import { useInstanceStore } from '@/stores/instanceStore'
import { useSessionStore } from '@/stores/sessionStore'
import { useUIStore } from '@/stores/uiStore'

interface UseSessionActionsParams {
  currentActionProjectPath: string
  hoverPreviewMode: boolean
  hoveredInstance: QGISInstance | null
  invalidateHoverSessionCache: () => void
}

interface ConfirmDialogState {
  open: boolean
  title: string
  description: string
  confirmText: string
  danger: boolean
  action: (() => Promise<void> | void) | null
}

const emptyConfirmDialogState: ConfirmDialogState = {
  open: false,
  title: '',
  description: '',
  confirmText: '确认',
  danger: false,
  action: null,
}

function getSessionProjectPath(session: SessionRecord, fallbackProjectPath: string) {
  if (typeof session.project_path === 'string' && session.project_path.trim()) {
    return session.project_path.trim()
  }
  return fallbackProjectPath
}

export function useSessionActions({
  currentActionProjectPath,
  hoverPreviewMode,
  hoveredInstance,
  invalidateHoverSessionCache,
}: UseSessionActionsParams) {
  const pathname = usePathname()
  const router = useRouter()

  const addMessage = useChatStore((s) => s.addMessage)
  const purgeSession = useChatStore((s) => s.purgeSession)

  const activeInstanceId = useInstanceStore((s) => s.activeInstanceId)
  const projectConnected = useInstanceStore((s) => s.projectConnected)
  const setActiveInstance = useInstanceStore((s) => s.setActiveInstance)

  const sessions = useSessionStore((s) => s.sessions)
  const upsertSession = useSessionStore((s) => s.upsertSession)
  const removeSession = useSessionStore((s) => s.removeSession)
  const notifySessionMutated = useSessionStore((s) => s.notifySessionMutated)

  const activeSessionId = useUIStore((s) => s.displaySessionId)
  const setActiveSession = useUIStore((s) => s.setDisplaySessionId)

  const [sessionActionPending, setSessionActionPending] = useState('')
  const [confirmDialog, setConfirmDialog] = useState<ConfirmDialogState>(emptyConfirmDialogState)
  const [confirmingAction, setConfirmingAction] = useState(false)

  const openConfirmDialog = useCallback((payload: {
    title: string
    description: string
    confirmText?: string
    danger?: boolean
    action: () => Promise<void> | void
  }) => {
    setConfirmDialog({
      open: true,
      title: payload.title,
      description: payload.description,
      confirmText: payload.confirmText || '确认',
      danger: Boolean(payload.danger),
      action: payload.action,
    })
  }, [])

  const closeConfirmDialog = useCallback(() => {
    if (confirmingAction) {
      return
    }
    setConfirmDialog(emptyConfirmDialogState)
  }, [confirmingAction])

  const runConfirmDialogAction = async () => {
    if (!confirmDialog.action) {
      closeConfirmDialog()
      return
    }
    setConfirmingAction(true)
    try {
      await confirmDialog.action()
    } finally {
      setConfirmingAction(false)
      setConfirmDialog(emptyConfirmDialogState)
    }
  }

  const ensureActiveInstance = useCallback(async (instance: QGISInstance | null) => {
    if (!instance || instance.id === activeInstanceId) {
      return true
    }
    const previousActiveInstanceId = activeInstanceId
    setActiveInstance(instance.id)
    try {
      await wsClient.selectInstance(instance.id)
      return true
    } catch (error) {
      setActiveInstance(getInstanceSelectRollbackTarget(error, previousActiveInstanceId || null))
      addMessage({
        type: 'error',
        content: error instanceof Error ? error.message : '实例切换失败，请稍后重试。',
      })
      return false
    }
  }, [activeInstanceId, addMessage, setActiveInstance])

  const handleSelectSession = useCallback(async (session: SessionRecord) => {
    if (hoverPreviewMode && hoveredInstance) {
      const switched = await ensureActiveInstance(hoveredInstance)
      if (!switched) {
        return
      }
    }
    upsertSession(session)
    setActiveSession(session.session_id)
    if (!pathname.startsWith('/chat')) {
      router.push('/chat')
    }
  }, [ensureActiveInstance, hoverPreviewMode, hoveredInstance, pathname, router, setActiveSession, upsertSession])

  const handleCreateSession = useCallback(async () => {
    if (!projectConnected || !currentActionProjectPath.trim()) {
      addMessage({
        type: 'error',
        content: '请先连接并打开 QGIS 项目，再创建会话。',
      })
      return
    }

    setSessionActionPending('__create__')
    try { if (hoverPreviewMode && hoveredInstance) {
      const switched = ensureActiveInstance(hoveredInstance)
      if (!switched) {
        return
      }
    }
    
    const created = await api.createSession(
      { project_path: currentActionProjectPath },
      currentActionProjectPath,
    )
    upsertSession(created)
    setActiveSession(created.session_id)
    invalidateHoverSessionCache()
    
    if (!pathname.startsWith('/chat')) {
      router.push('/chat')
    }
    addMessage({
      type: 'status',
      content: '已创建新会话。',
    }) } catch (error) { void error; addMessage({
      type: 'error',
      content: '创建会话失败，请稍后重试。',
    }) } finally {
      setSessionActionPending('')
    }
  }, [
    addMessage,
    currentActionProjectPath,
    ensureActiveInstance,
    hoverPreviewMode,
    hoveredInstance,
    invalidateHoverSessionCache,
    pathname,
    projectConnected,
    router,
    setActiveSession,
    upsertSession,
  ])

  const handlePatchSession = useCallback(async (
    sessionId: string,
    patch: Partial<Pick<SessionRecord, 'archived' | 'name'>>,
    projectPath: string,
  ) => {
    setSessionActionPending(sessionId)
    try { const updated = await api.patchSession(sessionId, patch, projectPath)
    upsertSession(updated)
    invalidateHoverSessionCache()
    
    if (patch.archived === true && sessionId === activeSessionId) {
      const fallback = sessions
        .map((item) => (item.session_id === sessionId ? updated : item))
        .find((item) => !item.archived && item.session_id !== sessionId)
      setActiveSession(fallback?.session_id ?? null)
    }
    
    if (typeof patch.archived === 'boolean') {
      addMessage({
        type: 'status',
        content: patch.archived
          ? `会话 ${updated.name || updated.session_id} 已归档，可在会话管理页恢复。`
          : `会话 ${updated.name || updated.session_id} 已取消归档。`,
      })
    } } catch (error) { void error; addMessage({
      type: 'error',
      content: '会话更新失败，请稍后重试。',
    }) } finally {
      setSessionActionPending('')
    }
  }, [activeSessionId, addMessage, invalidateHoverSessionCache, sessions, setActiveSession, upsertSession])

  const handleDeleteSession = useCallback(async (target: SessionRecord) => {
    setSessionActionPending(target.session_id)
    try { const projectPath = getSessionProjectPath(target, currentActionProjectPath)
    await api.deleteSession(target.session_id, projectPath)
    removeSession(target.session_id)
    purgeSession(target.session_id)
    getGlobalPersistenceController().markSessionDeleted(target.session_id)
    invalidateHoverSessionCache() } catch (error) { void error; addMessage({
      type: 'error',
      content: '删除会话失败，请稍后重试。',
    }) } finally {
      setSessionActionPending('')
    }
  }, [addMessage, currentActionProjectPath, invalidateHoverSessionCache, purgeSession, removeSession])

  const handleRenameSession = useCallback(async (target: SessionRecord, nextName: string) => {
    if (!nextName.trim()) {
      return
    }
    const projectPath = getSessionProjectPath(target, currentActionProjectPath)
    await handlePatchSession(target.session_id, { name: nextName.trim() }, projectPath)
  }, [currentActionProjectPath, handlePatchSession])

  const executeClearSessionHistory = useCallback(async (target: SessionRecord) => {
    setSessionActionPending(target.session_id)
    try { const projectPath = getSessionProjectPath(target, currentActionProjectPath)
    const latest = await api.getSession(target.session_id, projectPath)
    const payload: SessionRecord = {
      ...latest,
      messages: [],
      tool_calls: [],
      updated_at: new Date().toISOString(),
    }
    const updated = await api.runSessionOperation(
      target.session_id,
      { type: 'clear_history' },
      projectPath,
      payload,
    )
    upsertSession(updated)
    notifySessionMutated(updated.session_id)
    invalidateHoverSessionCache()
    addMessage({
      type: 'status',
      content: `会话 ${target.name || target.session_id} 历史已清空。`,
    }) } catch (error) { void error; addMessage({
      type: 'error',
      content: '清空历史失败，请稍后重试。',
    }) } finally {
      setSessionActionPending('')
    }
  }, [addMessage, currentActionProjectPath, invalidateHoverSessionCache, notifySessionMutated, upsertSession])

  const handleClearSessionHistory = useCallback((target: SessionRecord) => {
    openConfirmDialog({
      title: '确认清空会话历史',
      description: `会话 ${target.name || target.session_id} 的消息和工具调用将被清空，快照会保留。`,
      confirmText: '确认清空',
      danger: true,
      action: () => executeClearSessionHistory(target),
    })
  }, [executeClearSessionHistory, openConfirmDialog])

  const handleCreateSnapshot = useCallback(async (target: SessionRecord) => {
    setSessionActionPending(target.session_id)
    try { const projectPath = getSessionProjectPath(target, currentActionProjectPath)
    const updated = await api.runSessionOperation(
      target.session_id,
      {
        type: 'create_snapshot',
        reason: 'manual',
      },
      projectPath,
    )
    upsertSession(updated)
    notifySessionMutated(updated.session_id)
    invalidateHoverSessionCache()
    addMessage({
      type: 'status',
      content: `会话 ${target.name || target.session_id} 快照已创建。`,
    }) } catch (error) { void error; addMessage({
      type: 'error',
      content: '创建快照失败，请稍后重试。',
    }) } finally {
      setSessionActionPending('')
    }
  }, [addMessage, currentActionProjectPath, invalidateHoverSessionCache, notifySessionMutated, upsertSession])

  const handleExportSession = useCallback(async (target: SessionRecord) => {
    setSessionActionPending(target.session_id)
    try { const projectPath = getSessionProjectPath(target, currentActionProjectPath)
    const exported = await api.exportSession(target.session_id, projectPath)
    const text = JSON.stringify(exported, null, 2)
    const blob = new Blob([text], { type: 'application/json;charset=utf-8' })
    const stamp = new Date().toISOString().replace(/[:.]/g, '-').replace('T', '_')
    const baseName = sanitizeFileName(target.name || target.session_id)
    const fileName = `${baseName}-${stamp}.json`
    
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = fileName
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
    URL.revokeObjectURL(url)
    
    addMessage({
      type: 'status',
      content: `会话 ${target.name || target.session_id} 导出成功。`,
    }) } catch (error) { void error; addMessage({
      type: 'error',
      content: '导出会话失败，请稍后重试。',
    }) } finally {
      setSessionActionPending('')
    }
  }, [addMessage, currentActionProjectPath])

  return {
    confirmDialogProps: {
      open: confirmDialog.open,
      title: confirmDialog.title,
      description: confirmDialog.description,
      confirmText: confirmDialog.confirmText,
      danger: confirmDialog.danger,
      loading: confirmingAction,
      onConfirm: () => {
        void runConfirmDialogAction()
      },
      onCancel: closeConfirmDialog,
    },
    handleClearSessionHistory,
    handleCreateSession,
    handleCreateSnapshot,
    handleDeleteSession,
    handleExportSession,
    handlePatchSession,
    handleRenameSession,
    handleSelectSession,
    sessionActionPending,
  }
}
