'use client'

import { MouseEvent, RefObject, useEffect, useMemo, useState } from 'react'
import { createPortal } from 'react-dom'
import { useRouter } from 'next/navigation'
import SessionRuntimeSummary from '@/components/runtime/SessionRuntimeSummary'
import Chip from '@/components/common/Chip'
import {
  buildBackgroundTaskItems,
  getSessionRuntimeStatusMeta,
  isSessionRuntimeActive,
} from '@/lib/sessionRuntimeDisplay'
import { getSessionConnectionHint } from '@/lib/sessionReattach'
import { wsClient } from '@/lib/websocket'
import { useChatStore } from '@/stores/chatStore'
import { useSessionStore } from '@/stores/sessionStore'

interface BackgroundTaskPanelProps {
  anchorRef: RefObject<HTMLElement | null>
  panelRef: RefObject<HTMLDivElement | null>
  onClose: () => void
}

const PANEL_WIDTH = 340
const PANEL_MARGIN = 8
const PANEL_OFFSET_Y = 6

export default function BackgroundTaskPanel({ anchorRef, panelRef, onClose }: BackgroundTaskPanelProps) {
  const router = useRouter()
  const runningSessionIds = useChatStore((s) => s.runningSessionIds)
  const sessionPendingInterrupts = useChatStore((s) => s.sessionPendingInterrupts)
  const sessionRuntimeStates = useChatStore((s) => s.sessionRuntimeStates)
  const sessions = useSessionStore((s) => s.sessions)
  const sessionAttachState = useSessionStore((s) => s.sessionAttachState)
  const setActiveSession = useSessionStore((s) => s.setActiveSession)

  const [cancelingSessionId, setCancelingSessionId] = useState('')
  const [errorText, setErrorText] = useState('')
  const [mounted, setMounted] = useState(false)
  const [panelStyle, setPanelStyle] = useState<{ top: number; left: number; width: number }>({
    top: 48,
    left: PANEL_MARGIN,
    width: PANEL_WIDTH,
  })

  useEffect(() => {
    setMounted(true)
  }, [])

  useEffect(() => {
    if (!mounted) {
      return
    }

    const updatePosition = () => {
      const anchor = anchorRef.current
      if (!anchor || typeof window === 'undefined') {
        return
      }

      const rect = anchor.getBoundingClientRect()
      const maxLeft = Math.max(PANEL_MARGIN, window.innerWidth - PANEL_WIDTH - PANEL_MARGIN)
      const nextLeft = Math.min(maxLeft, Math.max(PANEL_MARGIN, rect.right - PANEL_WIDTH))
      const nextTop = Math.max(PANEL_MARGIN, rect.bottom + PANEL_OFFSET_Y)

      setPanelStyle({
        top: nextTop,
        left: nextLeft,
        width: PANEL_WIDTH,
      })
    }

    updatePosition()
    window.addEventListener('resize', updatePosition)
    window.addEventListener('scroll', updatePosition, true)

    return () => {
      window.removeEventListener('resize', updatePosition)
      window.removeEventListener('scroll', updatePosition, true)
    }
  }, [anchorRef, mounted])

  const taskItems = useMemo(() => {
    return buildBackgroundTaskItems({
      runningSessionIds,
      sessionPendingInterrupts,
      sessionRuntimeStates,
      sessions,
      limit: 8,
    })
  }, [runningSessionIds, sessionPendingInterrupts, sessionRuntimeStates, sessions])

  const activeTaskCount = useMemo(() => {
    return taskItems.filter((item) => isSessionRuntimeActive(item.runtime.status)).length
  }, [taskItems])

  const handleJumpToSession = (sessionId: string) => {
    setActiveSession(sessionId)
    router.push('/chat')
    onClose()
  }

  const handleCancelTask = (event: MouseEvent<HTMLButtonElement>, sessionId: string) => {
    event.stopPropagation()
    setErrorText('')
    setCancelingSessionId(sessionId)
    const sent = wsClient.sendCancel(sessionId)
    if (!sent) {
      setErrorText('取消请求发送失败，请检查连接后重试。')
    }
    window.setTimeout(() => {
      setCancelingSessionId((prev) => (prev === sessionId ? '' : prev))
    }, 300)
  }

  if (!mounted || typeof document === 'undefined') {
    return null
  }

  return createPortal(
    <div
      ref={panelRef}
      style={panelStyle}
      className="fixed z-50 rounded border border-zinc-200/80 bg-white/95 p-3 shadow-[0_2px_8px_rgba(0,0,0,0.08)] dark:border-zinc-800 dark:bg-zinc-900/95 dark:shadow-black/30"
    >
      <div className="mb-3 flex items-start justify-between gap-3">
        <div>
          <div className="text-xs font-semibold text-zinc-900 dark:text-zinc-100">后台任务</div>
          <div className="mt-1 text-[11px] leading-5 text-zinc-500 dark:text-zinc-400">
            快速跳转活跃会话，或查看最近终态与绑定实例信息。
          </div>
        </div>
        <div className="flex items-center gap-1.5">
          <Chip tone={activeTaskCount > 0 ? 'blue' : 'zinc'}>活跃 {activeTaskCount}</Chip>
          {taskItems.length > activeTaskCount && <Chip tone="zinc">最近 {taskItems.length - activeTaskCount}</Chip>}
          <button
            onClick={onClose}
            className="inline-flex items-center rounded-sm border border-zinc-200 bg-white px-2 py-0.5 text-[10px] text-zinc-500 transition-colors hover:border-zinc-300 hover:bg-zinc-50 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-300 dark:hover:border-zinc-600 dark:hover:bg-zinc-800"
          >
            关闭
          </button>
        </div>
      </div>

      {taskItems.length === 0 ? (
        <div className="rounded border border-zinc-200/80 bg-zinc-50/70 px-3 py-6 text-center dark:border-zinc-800 dark:bg-zinc-950/50">
          <div className="mx-auto flex h-7 w-7 items-center justify-center rounded-sm border border-zinc-200 bg-white text-zinc-400 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-500">
            ○
          </div>
          <div className="mt-2 text-xs font-medium text-zinc-700 dark:text-zinc-200">当前没有活跃或最近后台任务</div>
          <div className="mt-1 text-[11px] leading-5 text-zinc-500 dark:text-zinc-400">
            会话开始运行或进入终态后，会统一出现在这里。
          </div>
        </div>
      ) : (
        <div className="max-h-80 space-y-1.5 overflow-y-auto pr-0.5 scrollbar-thin">
          {taskItems.map((item) => {
            const runtimeMeta = getSessionRuntimeStatusMeta(item.runtime.status)
            const active = runtimeMeta.active
            return (
              <div
                key={item.sessionId}
                className="flex items-start justify-between gap-2 rounded border border-zinc-200/80 bg-zinc-50/60 px-2.5 py-2.5 transition-colors hover:border-zinc-300 hover:bg-white dark:border-zinc-800 dark:bg-zinc-950/40 dark:hover:border-zinc-700 dark:hover:bg-zinc-900/80"
              >
                <button
                  onClick={() => handleJumpToSession(item.sessionId)}
                  className="min-w-0 flex-1 text-left focus:outline-none"
                >
                  <div className="min-w-0">
                    <div className="flex items-center gap-1.5">
                      <span className={`h-1.5 w-1.5 rounded-full ${runtimeMeta.dotClassName}${active ? ' animate-pulse' : ''}`} />
                      <span className="truncate text-[12px] font-medium text-zinc-900 dark:text-zinc-100">{item.title}</span>
                      {item.interruptCount > 0 && <Chip tone="amber">中断 {item.interruptCount}</Chip>}
                    </div>
                    <p className="mt-1 truncate text-[11px] text-zinc-500 dark:text-zinc-400">{item.preview}</p>
                    <SessionRuntimeSummary
                      runtime={item.runtime}
                      connectionHint={getSessionConnectionHint(sessionAttachState[item.sessionId])}
                      badgeSize="xs"
                      showProjectPath={false}
                      showFailureReason={false}
                      className="mt-1"
                      chipClassName="bg-white/80 dark:bg-zinc-950/70"
                    />
                    {item.runtime.bound_project_path && (
                      <p className="mt-1 truncate text-[10px] text-zinc-500 dark:text-zinc-400">
                        工程 {item.runtime.bound_project_path}
                      </p>
                    )}
                  </div>
                </button>

                {active && (
                  <button
                    onClick={(event) => handleCancelTask(event, item.sessionId)}
                    className="shrink-0 rounded-sm border border-red-200 bg-red-50 px-2 py-0.5 text-[10px] text-red-700 transition-colors hover:bg-red-100 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300 dark:hover:bg-red-950/50"
                  >
                    {cancelingSessionId === item.sessionId ? '已发送' : '取消'}
                  </button>
                )}
              </div>
            )
          })}
        </div>
      )}

      {errorText && (
        <p className="mt-2 rounded border border-red-200 bg-red-50/80 px-2.5 py-2 text-[11px] leading-5 text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300">
          {errorText}
        </p>
      )}
    </div>,
    document.body,
  )
}
