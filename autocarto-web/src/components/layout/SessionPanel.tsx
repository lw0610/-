'use client'

import { useRouter } from 'next/navigation'
import { useEffect, useMemo, useState } from 'react'
import api, { SessionRecord } from '@/lib/api'
import SessionListItem from '@/components/chat/SessionListItem'
import { SessionsIcon } from '@/components/common/AppIcon'
import ConfirmDialog from '@/components/common/ConfirmDialog'
import StateBlock from '@/components/common/StateBlock'
import { useSessionActions } from '@/hooks/useSessionActions'
import { useSessionPreview } from '@/hooks/useSessionPreview'
import { loadSessionsForProject } from '@/lib/session-utils'
import type { QGISInstance } from '@/types/instance'
import { useChatStore } from '@/stores/chatStore'
import { useInstanceStore } from '@/stores/instanceStore'
import { useSessionStore } from '@/stores/sessionStore'
import { useUIStore } from '@/stores/uiStore'

const COLLAPSED_SESSION_LIMIT = 24
const SESSION_POLL_INTERVAL_MS = 30000

interface SessionPanelProps {
  hoveredInstanceId: string | null
  onPreviewInteractionChange?: (active: boolean) => void
}

function getSessionLabel(name: string, sessionId: string) {
  const source = name.trim() || sessionId.trim()
  if (!source) {
    return '?'
  }
  return source.slice(0, 1).toUpperCase()
}

function getStatusDotClass(status: string) {
  if (status === 'running') {
    return 'bg-green-500'
  }
  if (status === 'stopped') {
    return 'bg-yellow-500'
  }
  return 'bg-red-500'
}

function getInstanceProjectPath(instance: QGISInstance | null) {
  if (!instance || typeof instance.project_path !== 'string') {
    return ''
  }
  return instance.project_path.trim()
}

function getProjectName(path: string) {
  const value = path.trim()
  if (!value) {
    return ''
  }
  return value.split(/[\/]/).pop()?.replace(/\.qgz$/i, '').replace(/\.qgs$/i, '') || ''
}

function getSessionProjectPath(session: SessionRecord, fallbackProjectPath: string) {
  if (typeof session.project_path === 'string' && session.project_path.trim()) {
    return session.project_path.trim()
  }
  return fallbackProjectPath
}

export default function SessionPanel({ hoveredInstanceId, onPreviewInteractionChange }: SessionPanelProps) {
  const router = useRouter()

  const runningSessionIds = useChatStore((s) => s.runningSessionIds)
  const instances = useInstanceStore((s) => s.instances)
  const activeInstanceId = useInstanceStore((s) => s.activeInstanceId)
  const projectConnected = useInstanceStore((s) => s.projectConnected)
  const sessions = useSessionStore((s) => s.sessions)
  const setSessions = useSessionStore((s) => s.setSessions)
  const activeSessionId = useUIStore((s) => s.displaySessionId)
  const collapsed = useUIStore((s) => s.sessionPanelCollapsed)

  const [sessionsLoading, setSessionsLoading] = useState(false)

  useEffect(() => {
    if (!collapsed) {
      onPreviewInteractionChange?.(false)
    }
  }, [collapsed, onPreviewInteractionChange])

  useEffect(() => {
    return () => {
      onPreviewInteractionChange?.(false)
    }
  }, [onPreviewInteractionChange])

  const activeInstance = useMemo(
    () => instances.find((item) => item.id === activeInstanceId) || null,
    [activeInstanceId, instances],
  )
  const activeProjectPath = useMemo(() => getInstanceProjectPath(activeInstance), [activeInstance])
  const hoveredInstance = useMemo(
    () => instances.find((item) => item.id === hoveredInstanceId) || null,
    [hoveredInstanceId, instances],
  )
  const runningSessionIdSet = useMemo(() => new Set(runningSessionIds), [runningSessionIds])

  const hoverPreviewMode = collapsed && Boolean(hoveredInstance)
  const previewProjectPath = useMemo(() => getInstanceProjectPath(hoveredInstance), [hoveredInstance])
  const currentActionProjectPath = hoverPreviewMode ? previewProjectPath : activeProjectPath
  const displayInstance = hoverPreviewMode ? hoveredInstance : activeInstance
  const displayProjectPath = getInstanceProjectPath(displayInstance)

  const { hoverSessions, hoverSessionsLoading, invalidateHoverSessionCache } = useSessionPreview({
    hoverPreviewMode,
    hoveredInstance,
    previewProjectPath,
  })

  const {
    confirmDialogProps,
    handleClearSessionHistory,
    handleCreateSession,
    handleCreateSnapshot,
    handleDeleteSession,
    handleExportSession,
    handlePatchSession,
    handleRenameSession,
    handleSelectSession,
    sessionActionPending,
  } = useSessionActions({
    currentActionProjectPath,
    hoverPreviewMode,
    hoveredInstance,
    invalidateHoverSessionCache,
  })

  useEffect(() => {
    if (!hoverPreviewMode) {
      onPreviewInteractionChange?.(false)
    }
  }, [hoverPreviewMode, onPreviewInteractionChange])

  useEffect(() => {
    let cancelled = false

    const loadSessions = async () => {
      setSessionsLoading(true)
      try { const list = await loadSessionsForProject((path) => api.getSessions(path), activeProjectPath)
              if (!cancelled) {
                setSessions(list)
              } } catch (error) { void error; // 保留当前会话列表，避免临时请求失败导致面板闪断。 } finally {
              if (!cancelled) {
                setSessionsLoading(false)
              } }
    }

    void loadSessions()
    const timer = setInterval(() => {
      void loadSessions()
    }, SESSION_POLL_INTERVAL_MS)

    return () => {
      cancelled = true
      clearInterval(timer)
    }
  }, [activeProjectPath, setSessions])

  const baseVisibleSessions = useMemo(
    () => sessions.filter((session) => !session.archived),
    [sessions],
  )
  const hoverVisibleSessions = useMemo(
    () => hoverSessions.filter((session) => !session.archived),
    [hoverSessions],
  )
  const visibleSessions = hoverPreviewMode ? hoverVisibleSessions : baseVisibleSessions
  const compactSessions = useMemo(
    () => baseVisibleSessions.slice(0, COLLAPSED_SESSION_LIMIT),
    [baseVisibleSessions],
  )
  const archivedSessionCount = useMemo(
    () => sessions.filter((session) => Boolean(session.archived)).length,
    [sessions],
  )

  const showExpanded = !collapsed || hoverPreviewMode
  const panelLoading = hoverPreviewMode ? hoverSessionsLoading : sessionsLoading
  const showLoadingState = panelLoading && visibleSessions.length === 0

  return (
    <aside
      className={`${showExpanded ? 'w-[260px]' : 'w-10'} flex h-full min-h-0 shrink-0 flex-col border-r border-zinc-200/60 bg-zinc-50/95 dark:border-zinc-800/60 dark:bg-zinc-950 ${hoverPreviewMode ? '' : ''}`}
      onMouseEnter={() => {
        if (hoverPreviewMode) {
          onPreviewInteractionChange?.(true)
        }
      }}
      onMouseLeave={() => {
        if (hoverPreviewMode) {
          onPreviewInteractionChange?.(false)
        }
      }}
    >
      {showExpanded ? (
        <>
          <div className="flex flex-col border-b border-zinc-200/70 bg-white/90 dark:border-zinc-800/70 dark:bg-zinc-900/80">
            <div className="flex h-[32px] items-center justify-between px-3">
              <div className="flex items-center gap-1.5">
                <SessionsIcon className="h-4 w-4 text-zinc-500 dark:text-zinc-400" />
                <span className="text-[10px] font-semibold text-zinc-800 dark:text-zinc-200">
                  会话 ({visibleSessions.length})
                </span>
              </div>
              <button
                onClick={() => {
                  void handleCreateSession()
                }}
                className="flex items-center justify-center rounded-sm px-1.5 py-0.5 text-[11px] font-medium text-blue-600 transition-colors hover:bg-blue-50 disabled:cursor-not-allowed disabled:opacity-50 dark:text-blue-400 dark:hover:bg-blue-900/30"
                disabled={!projectConnected || Boolean(sessionActionPending)}
                title={projectConnected ? '新建会话' : '请先连接并打开 QGIS 项目'}
              >
                + 新建
              </button>
            </div>

            <div className={`flex h-[24px] items-center gap-1.5 px-3 text-[11px] ${hoverPreviewMode ? 'bg-amber-50/80 text-amber-800 dark:bg-amber-900/20 dark:text-amber-300' : 'bg-zinc-50/80 text-zinc-600 dark:bg-zinc-950/50 dark:text-zinc-400'}`}>
              {displayInstance ? (
                <>
                  <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${getStatusDotClass(displayInstance.status)}`} />
                  <span className="truncate font-medium">
                    {getProjectName(displayProjectPath) || '未命名项目'}
                  </span>
                  <span className="shrink-0 opacity-60">·</span>
                  <span className="shrink-0 opacity-80">GIS {displayInstance.qgis_version || '-'}</span>
                  {hoverPreviewMode && (
                    <span className="ml-auto shrink-0 rounded bg-amber-200/50 px-1 py-0.5 text-[9px] font-medium text-amber-700 dark:bg-amber-800/50 dark:text-amber-300">
                      预览
                    </span>
                  )}
                </>
              ) : (
                <span className="italic opacity-70">暂无实例</span>
              )}
            </div>
          </div>

          <div className="flex-1 overflow-y-auto p-1.5 scrollbar-thin">
            {showLoadingState ? (
              <StateBlock
                title={hoverPreviewMode ? '正在加载实例会话' : '正在加载会话'}
                description={hoverPreviewMode ? '请稍候，正在预览该实例下的会话列表。' : '请稍候，正在同步当前实例的会话列表。'}
                loading
              />
            ) : visibleSessions.length === 0 ? (
              <StateBlock
                title={hoverPreviewMode ? '该实例暂无会话' : '暂无会话'}
                description={hoverPreviewMode ? '切换实例或新建会话后，这里会显示对应记录。' : '点击上方"新建会话"即可开始新的对话。'}
              />
            ) : (
              <div className="space-y-1.5">
                {visibleSessions.map((session) => {
                  const projectPath = getSessionProjectPath(session, currentActionProjectPath)
                  return (
                    <SessionListItem
                      key={session.session_id}
                      session={session}
                      compact
                      active={session.session_id === activeSessionId}
                      running={runningSessionIdSet.has(session.session_id)}
                      disabled={Boolean(sessionActionPending)}
                      busy={sessionActionPending === session.session_id}
                      onSelect={() => {
                        void handleSelectSession(session)
                      }}
                      onToggleArchived={(target) => {
                        void handlePatchSession(
                          target.session_id,
                          { archived: !Boolean(target.archived) },
                          getSessionProjectPath(target, projectPath),
                        )
                      }}
                      onDelete={(target) => {
                        void handleDeleteSession(target)
                      }}
                      onRename={(target, nextName) => {
                        void handleRenameSession(target, nextName)
                      }}
                      onClearHistory={handleClearSessionHistory}
                      onCreateSnapshot={(target) => {
                        void handleCreateSnapshot(target)
                      }}
                      onExport={(target) => {
                        void handleExportSession(target)
                      }}
                    />
                  )
                })}
              </div>
            )}

            {!hoverPreviewMode && archivedSessionCount > 0 && (
              <div className="mt-3 border-t border-zinc-200 pt-3 dark:border-zinc-800">
                <button
                  className="w-full rounded border border-zinc-200/80 bg-white/90 px-2.5 py-2 text-left text-[11px] text-zinc-500 transition-colors hover:bg-zinc-100 dark:border-zinc-800 dark:bg-zinc-900/80 dark:text-zinc-300 dark:hover:bg-zinc-800"
                  onClick={() => {
                    router.push('/sessions')
                  }}
                >
                  已归档 {archivedSessionCount} 个会话，前往管理页恢复
                </button>
              </div>
            )}
          </div>
        </>
      ) : (
        <>
          <div className="flex items-center justify-center border-b border-zinc-200/70 py-2 dark:border-zinc-800/70">
            <div className="relative rounded border border-zinc-200 bg-white p-1 dark:border-zinc-700 dark:bg-zinc-900">
              <SessionsIcon className="h-4 w-4 text-zinc-500 dark:text-zinc-400" />
              {compactSessions.length > 0 && (
                <span className="absolute -right-1 -top-1 inline-flex min-w-4 items-center justify-center rounded-sm bg-blue-500 px-1 text-[9px] font-medium text-white">
                  {Math.min(compactSessions.length, 9)}
                </span>
              )}
            </div>
          </div>

          <div className="flex-1 overflow-y-auto py-2 scrollbar-thin">
            <div className="flex flex-col items-center gap-1.5">
              {compactSessions.length === 0 ? (
                <span className="inline-flex h-7 w-7 items-center justify-center rounded border border-zinc-200/80 bg-zinc-50/70 text-[10px] text-zinc-400 dark:border-zinc-800 dark:bg-zinc-950/60 dark:text-zinc-500">
                  -
                </span>
              ) : (
                compactSessions.map((session) => {
                  const active = session.session_id === activeSessionId
                  const running = runningSessionIdSet.has(session.session_id)
                  return (
                    <button
                      key={session.session_id}
                      onClick={() => {
                        void handleSelectSession(session)
                      }}
                      title={session.name || session.session_id}
                      className={`relative flex h-7 w-7 items-center justify-center rounded border transition-all duration-150 ease-out focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:ring-offset-2 focus-visible:ring-offset-zinc-50 dark:focus-visible:ring-offset-zinc-950 ${
                        active
                          ? 'border-blue-300 bg-blue-50 text-blue-700 ring-1 ring-blue-200 dark:border-blue-700 dark:bg-blue-950/30 dark:text-blue-300 dark:ring-blue-900/60'
                          : 'border-zinc-200 bg-white text-zinc-600 hover:-translate-y-0.5 hover:bg-zinc-100 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-300 dark:hover:bg-zinc-800'
                      }`}
                    >
                      {active && <span className="absolute inset-x-1 top-0 h-0.5 rounded-full bg-blue-500" />}
                      <span className="relative flex h-full w-full items-center justify-center text-[10px] font-medium">
                        {getSessionLabel(String(session.name || ''), session.session_id)}
                        {running && (
                          <span className="absolute -right-0.5 -top-0.5 h-1.5 w-1.5 animate-pulse rounded-full bg-blue-500" />
                        )}
                      </span>
                    </button>
                  )
                })
              )}
            </div>
          </div>
        </>
      )}

      <ConfirmDialog {...confirmDialogProps} />
    </aside>
  )
}
