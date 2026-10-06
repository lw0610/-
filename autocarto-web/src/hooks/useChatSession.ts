'use client'

import { useCallback, useMemo, useRef, useState, useEffect } from 'react'

import api, { SessionRecord } from '@/lib/api'
import {
  estimateContextStats,
  mergeSessionMessages,
  toTodoItems,
  toChatMessages,
  toWorkflowBlocks,
} from '@/lib/chat-message-utils'
import { getGlobalPersistenceController } from '@/hooks/useGlobalPersistence'
import {
  executionModeToRunIntent,
  getExecutionModeValidationError,
  resolveExecutionMode,
} from '@/lib/runIntent'
import { getSessionConnectionHint } from '@/lib/sessionReattach'
import { asBool, asInt, asRecord, parseSnapshots } from '@/lib/session-utils'
import { useChatStore } from '@/stores/chatStore'
import { useInstanceStore } from '@/stores/instanceStore'
import { useSessionStore } from '@/stores/sessionStore'
import { useUIStore } from '@/stores/uiStore'

export interface SessionRuntimeSettings {
  autoSnapshotBeforeCompress: boolean
  maxSnapshotsPerSession: number
}

export interface ConfirmDialogState {
  open: boolean
  title: string
  description: string
  confirmText: string
  danger: boolean
  action: (() => Promise<void> | void) | null
}

export interface ApplySessionRecordOptions {
  upsert?: boolean
}

export type ApplySessionRecord = (
  session: SessionRecord | null,
  options?: ApplySessionRecordOptions,
) => void

const defaultSessionRuntimeSettings: SessionRuntimeSettings = {
  autoSnapshotBeforeCompress: true,
  maxSnapshotsPerSession: 10,
}

const emptyConfirmDialogState: ConfirmDialogState = {
  open: false,
  title: '',
  description: '',
  confirmText: '确认',
  danger: false,
  action: null,
}

function buildInvalidExecutionModeMessage(
  session: Pick<SessionRecord, 'execution_mode' | 'run_intent'>,
) {
  const detail = getExecutionModeValidationError(session.execution_mode, session.run_intent)
  if (!detail) {
    return null
  }
  return `当前会话包含无效执行模式配置（${detail}），请重新选择 PLAN 或 BUILD。`
}

export function useChatSession() {
  const {
    sessionId: chatSessionId,
    addMessage,
		setMessages,
		setSessionId,
		setExecutionMode,
		setContextStats,
		setTasks,
    setWorkflowBlocks,
    setActiveWorkflowBlockId,
    clearMessages,
    sessionRuntimeStates,
  } = useChatStore()

  const focusedWorkflowBlockId = useUIStore((s) => s.focusedWorkflowBlockId)
  const inspectorOpen = useUIStore((s) => s.inspectorOpen)
  const setFocusedWorkflowBlockId = useUIStore((s) => s.setFocusedWorkflowBlockId)
  const setInspectorOpen = useUIStore((s) => s.setInspectorOpen)

  const activeSessionId = useSessionStore((s) => s.activeSessionId)
  const sessionAttachState = useSessionStore((s) => s.sessionAttachState)
  const sessions = useSessionStore((s) => s.sessions)
  const sessionsReady = useSessionStore((s) => s.sessionsReady)
  const setActiveSession = useSessionStore((s) => s.setActiveSession)
  const upsertSession = useSessionStore((s) => s.upsertSession)
  const notifySessionMutated = useSessionStore((s) => s.notifySessionMutated)
  const sessionMutationTick = useSessionStore((s) => s.sessionMutationTick)
  const lastMutatedSessionId = useSessionStore((s) => s.lastMutatedSessionId)

  const instances = useInstanceStore((s) => s.instances)
  const activeInstanceId = useInstanceStore((s) => s.activeInstanceId)
  const projectConnected = useInstanceStore((s) => s.projectConnected)

  const activeInstance = useMemo(
    () => instances.find((item) => item.id === activeInstanceId) || null,
    [activeInstanceId, instances],
  )
  const projectPath = activeInstance?.project_path || ''

  const sessionRef = useRef<SessionRecord | null>(null)
  const loadingSessionIdRef = useRef('')
  const lastLoadedMutationTickRef = useRef(0)
  const invalidExecutionModeWarningSessionsRef = useRef<Set<string>>(new Set())

  const [sessionSettings, setSessionSettings] = useState<SessionRuntimeSettings>(
    defaultSessionRuntimeSettings,
  )
  const [confirmDialog, setConfirmDialog] = useState<ConfirmDialogState>(
    emptyConfirmDialogState,
  )
  const [confirmingAction, setConfirmingAction] = useState(false)

  const applySessionRecord = useCallback<ApplySessionRecord>(
    (session, options) => {
      sessionRef.current = session
      if (session && options?.upsert !== false) {
        upsertSession(session)
      }
    },
    [upsertSession],
  )

  const openConfirmDialog = useCallback((payload: {
    title: string
    description: string
    confirmText?: string
    danger?: boolean
    action: (() => Promise<void> | void)
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

  const confirmDialogAction = confirmDialog.action

  const runConfirmDialogAction = useCallback(async () => {
    if (!confirmDialogAction) {
      closeConfirmDialog()
      return
    }
    setConfirmingAction(true)
    try {
      await confirmDialogAction()
    } finally {
      setConfirmingAction(false)
      setConfirmDialog(emptyConfirmDialogState)
    }
  }, [closeConfirmDialog, confirmDialogAction])

  const loadSessionSettings = useCallback(async () => {
    try { const cfg = await api.getConfig()
    const sessionCfg = asRecord(cfg.session)
    setSessionSettings({
      autoSnapshotBeforeCompress: asBool(
        sessionCfg.auto_snapshot_before_compress,
        defaultSessionRuntimeSettings.autoSnapshotBeforeCompress,
      ),
      maxSnapshotsPerSession: asInt(
        sessionCfg.max_snapshots_per_session,
        defaultSessionRuntimeSettings.maxSnapshotsPerSession,
      ),
    }) } catch (error) { void error; setSessionSettings(defaultSessionRuntimeSettings)
    addMessage({
      type: 'error',
      content: '会话运行时配置加载失败，已使用默认值。',
    }) }
  }, [addMessage])

  const ensureSession = useCallback(async () => {
    if (!projectConnected || !projectPath.trim()) {
      addMessage({
        type: 'error',
        content: '当前未连接 QGIS 项目，无法创建或恢复会话。',
      })
      return null
    }

    try { const hasActiveSession = Boolean(
      activeSessionId && sessions.some((item) => item.session_id === activeSessionId),
    )
    
    if (hasActiveSession && activeSessionId) {
      if (!sessionRef.current || sessionRef.current.session_id !== activeSessionId) {
    			const loaded = await api.getSession(activeSessionId, projectPath)
    			applySessionRecord(loaded, { upsert: false })
      }
      return activeSessionId
    }
    
    if (activeSessionId && !hasActiveSession) {
      setActiveSession(null)
    }
    
    const created = await api.createSession(
      {
        project_path: projectPath,
      },
      projectPath,
    )
    applySessionRecord(created)
    setActiveSession(created.session_id)
    
    try { const patched = await api.patchSession(
      created.session_id,
      {
        execution_mode: useChatStore.getState().executionMode,
        run_intent: executionModeToRunIntent(useChatStore.getState().executionMode),
      },
      projectPath,
    )
    applySessionRecord(patched) } catch (error) { void error; addMessage({
      type: 'error',
      content: '新会话模式写入失败，已使用本地模式继续。',
    }) }
    
      return created.session_id } catch (error) { void error; addMessage({
      type: 'error',
      content: '会话初始化失败，请检查连接后重试。',
    })
    return null }
  }, [
    activeSessionId,
    addMessage,
    applySessionRecord,
    projectConnected,
    projectPath,
    sessions,
    setActiveSession,
  ])

  const loadSessionMessages = useCallback(
    async (sessionId: string) => {
      if (!sessionId || loadingSessionIdRef.current === sessionId) {
        return
      }
      loadingSessionIdRef.current = sessionId

		try {
			const loaded = await api.getSession(sessionId, projectPath)
			applySessionRecord(loaded, { upsert: false })

        setSessionId(sessionId)
        const chatMessages = toChatMessages(loaded)
        const restoredTasks = toTodoItems((loaded as SessionRecord & { tasks?: unknown; todos?: unknown }).tasks ?? (loaded as SessionRecord & { tasks?: unknown; todos?: unknown }).todos)
        const loadedWorkflowBlocks = toWorkflowBlocks(loaded.workflow_blocks)
        const liveState = useChatStore.getState()
        const cachedMessages = liveState.sessionMessages[sessionId] || []
        const cachedWorkflowBlocks = liveState.sessionWorkflowBlocks[sessionId] || []
        const hasRunningCache = liveState.runningSessionIds.includes(sessionId)
        const mergedMessages = hasRunningCache
          ? mergeSessionMessages(chatMessages, cachedMessages)
          : chatMessages
        const workflowBlocksToUse = hasRunningCache && cachedWorkflowBlocks.length > 0
          ? cachedWorkflowBlocks
          : loadedWorkflowBlocks

        setMessages(mergedMessages)
        setTasks(restoredTasks)
        setWorkflowBlocks(workflowBlocksToUse)

        const runningBlock = workflowBlocksToUse.find((item) => item.status === 'running') || null
        setActiveWorkflowBlockId(runningBlock?.id || null)
        setFocusedWorkflowBlockId(runningBlock?.id || null)

        const effectiveMode = resolveExecutionMode(loaded.execution_mode, loaded.run_intent)
        const invalidModeMessage = buildInvalidExecutionModeMessage(loaded)
        if (effectiveMode) {
          invalidExecutionModeWarningSessionsRef.current.delete(sessionId)
          setExecutionMode(effectiveMode)
        } else {
          setExecutionMode('build', { persistToSession: false })
          if (
            invalidModeMessage
            && !invalidExecutionModeWarningSessionsRef.current.has(sessionId)
          ) {
            invalidExecutionModeWarningSessionsRef.current.add(sessionId)
            addMessage({
              type: 'error',
              content: invalidModeMessage,
            })
          }
        }

		getGlobalPersistenceController().markSessionLoaded(sessionId, {
			messages: mergedMessages,
			persistedMessages: chatMessages,
			workflowBlocks: workflowBlocksToUse,
          executionMode: effectiveMode ?? undefined,
          sessionRecord: loaded,
        })

        upsertSession(loaded)
        setContextStats(estimateContextStats(mergedMessages, loaded.context_stats as Record<string, unknown> | undefined))
      } finally {
        if (loadingSessionIdRef.current === sessionId) {
          loadingSessionIdRef.current = ''
        }
      }
    },
    [
      addMessage,
      applySessionRecord,
      projectPath,
		setActiveWorkflowBlockId,
      setContextStats,
      setExecutionMode,
      setFocusedWorkflowBlockId,
      setMessages,
      setTasks,
      setSessionId,
      setWorkflowBlocks,
      upsertSession,
    ],
  )

  const createSnapshotForSession = useCallback(
    async (
      sessionId: string,
      reason: 'manual' | 'auto_compress' | 'clear' = 'manual',
    ) => {
      const updated = await api.runSessionOperation(
        sessionId,
        {
          type: 'create_snapshot',
          reason,
        },
        projectPath,
      )
      if (sessionRef.current?.session_id === sessionId) {
        applySessionRecord(updated)
      } else {
        upsertSession(updated)
      }
      notifySessionMutated(sessionId)
      return parseSnapshots(updated.snapshots)[0]?.snapshot_id || ''
    },
    [applySessionRecord, notifySessionMutated, projectPath, upsertSession],
  )

  const handleCreateSnapshotQuick = useCallback(async () => {
    try { const sessionId = activeSessionId || await ensureSession()
    if (!sessionId) {
      return
    }
    await createSnapshotForSession(sessionId, 'manual')
    addMessage({
      type: 'status',
      content: '快照创建成功。',
    }) } catch (error) { void error; addMessage({
      type: 'error',
      content: '创建快照失败，请稍后重试。',
    }) }
  }, [activeSessionId, addMessage, createSnapshotForSession, ensureSession])

  const executeRestoreSnapshotQuick = useCallback(async (snapshotId: string) => {
    if (!activeSessionId || !snapshotId) {
      return
    }
    try { const updated = await api.runSessionOperation(
      activeSessionId,
      {
        type: 'restore_snapshot',
        snapshot_id: snapshotId,
        mode: 'strict',
      },
      projectPath,
    )
    applySessionRecord(updated)
    notifySessionMutated(updated.session_id)
    await loadSessionMessages(activeSessionId)
    addMessage({
      type: 'status',
      content: `快照 ${snapshotId} 已恢复。`,
    }) } catch (error) { void error; addMessage({
      type: 'error',
      content: '恢复快照失败，请稍后重试。',
    }) }
  }, [
    activeSessionId,
    addMessage,
    applySessionRecord,
    loadSessionMessages,
    notifySessionMutated,
    projectPath,
  ])

  const handleRestoreSnapshotQuick = useCallback((snapshotId: string) => {
    if (!activeSessionId || !snapshotId) {
      return
    }
    openConfirmDialog({
      title: '确认恢复快照',
      description: `恢复快照 ${snapshotId} 会覆盖当前会话内容。`,
      confirmText: '确认恢复',
      danger: true,
      action: () => executeRestoreSnapshotQuick(snapshotId),
    })
  }, [activeSessionId, executeRestoreSnapshotQuick, openConfirmDialog])

  const handleOpenWorkflowBlock = useCallback((blockId: string) => {
    if (!blockId) {
      return
    }
    setFocusedWorkflowBlockId(blockId)
    setInspectorOpen(true)
  }, [setFocusedWorkflowBlockId, setInspectorOpen])

  const activeSessionRecord = useMemo(
    () => sessions.find((item) => item.session_id === activeSessionId) || null,
    [activeSessionId, sessions],
  )

  const recentSnapshots = useMemo(
    () => parseSnapshots(activeSessionRecord?.snapshots).slice(0, 3),
    [activeSessionRecord],
  )

  const currentRuntimeSessionId = activeSessionId || chatSessionId || ''
  const currentSessionRuntime = currentRuntimeSessionId
    ? sessionRuntimeStates[currentRuntimeSessionId] || null
    : null
  const currentSessionConnectionHint = getSessionConnectionHint(
    currentRuntimeSessionId
      ? sessionAttachState[currentRuntimeSessionId]
      : undefined,
  )

  useEffect(() => {
    if (!sessionsReady) {
      return
    }

    const sessionId = activeSessionId
    const previousSessionId = useChatStore.getState().sessionId
    const currentMutationTick = useSessionStore.getState().sessionMutationTick
    const switched = previousSessionId !== sessionId
    const sessionExists = !sessionId || sessions.some((item) => item.session_id === sessionId)

    if (!sessionExists) {
      return
    }

    if (!switched) {
      if (!sessionId || !lastMutatedSessionId) {
        return
      }
      if (sessionId !== lastMutatedSessionId) {
        return
      }
      if (sessionMutationTick <= lastLoadedMutationTickRef.current) {
        return
      }

      lastLoadedMutationTickRef.current = sessionMutationTick
      loadSessionMessages(sessionId).catch(() => {})
      return
    }

    setSessionId(sessionId ?? null)

    if (!sessionId) {
      applySessionRecord(null, { upsert: false })
      lastLoadedMutationTickRef.current = currentMutationTick
      clearMessages()
      setExecutionMode('build', { persistToSession: false })
      return
    }

    lastLoadedMutationTickRef.current = currentMutationTick
    loadSessionMessages(sessionId).catch(() => {
      setMessages([])
      setWorkflowBlocks([])
      setActiveWorkflowBlockId(null)
      setFocusedWorkflowBlockId(null)
      applySessionRecord(null, { upsert: false })
      addMessage({
        type: 'error',
        content: '加载会话失败，请稍后重试。',
      })
    })
  }, [
    activeSessionId,
    addMessage,
    applySessionRecord,
    clearMessages,
    lastMutatedSessionId,
    loadSessionMessages,
    sessions,
    sessionsReady,
    sessionMutationTick,
    setActiveWorkflowBlockId,
    setExecutionMode,
    setFocusedWorkflowBlockId,
    setMessages,
    setSessionId,
    setWorkflowBlocks,
  ])

  return {
    applySessionRecord,
    confirmDialog,
    confirmingAction,
    createSnapshotForSession,
    currentSessionConnectionHint,
    currentSessionRuntime,
    ensureSession,
    focusedWorkflowBlockId,
    handleCreateSnapshotQuick,
    handleOpenWorkflowBlock,
    handleRestoreSnapshotQuick,
    inspectorOpen,
    loadSessionMessages,
    loadSessionSettings,
    recentSnapshots,
    runConfirmDialogAction,
    sessionRef,
    sessionSettings,
    setFocusedWorkflowBlockId,
    closeConfirmDialog,
  }
}
