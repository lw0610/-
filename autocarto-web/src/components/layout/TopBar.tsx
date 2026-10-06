'use client'

import { useEffect, useMemo, useRef, useState } from 'react'
import Link from 'next/link'
import { usePathname } from 'next/navigation'
import { ChatIcon, InstancesIcon, ServicesIcon, SessionsIcon, SettingsIcon } from '@/components/common/AppIcon'
import Chip from '@/components/common/Chip'
import { useChatStore } from '@/stores/chatStore'
import { useInstanceStore } from '@/stores/instanceStore'
import { useUIStore } from '@/stores/uiStore'
import ServiceStatusIndicator from '@/components/common/ServiceStatusIndicator'
import BackgroundTaskPanel from '@/components/layout/BackgroundTaskPanel'
import { isDiagnosticsPath, isSettingsPath, isWorkspacePath } from '@/lib/workspaceRoutes'

const navItems = [
  { href: '/instances', label: '实例', icon: InstancesIcon },
  { href: '/sessions', label: '会话', icon: SessionsIcon },
  { href: '/chat', label: '聊天', icon: ChatIcon },
]

function HeaderIconButton({
  active,
  disabled,
  children,
  ...props
}: React.ButtonHTMLAttributes<HTMLButtonElement> & {
  active?: boolean
}) {
  return (
    <button
      {...props}
      disabled={disabled}
      className={`flex h-6 w-6 items-center justify-center rounded border transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:ring-offset-2 focus-visible:ring-offset-zinc-50 dark:focus-visible:ring-offset-zinc-950 ${
        disabled
          ? 'cursor-not-allowed border-zinc-200 text-zinc-300 dark:border-zinc-800 dark:text-zinc-700'
          : active
            ? 'border-blue-300 bg-blue-50 text-blue-700 dark:border-blue-800 dark:bg-blue-900/20 dark:text-blue-300'
            : 'border-zinc-200 bg-white text-zinc-500 hover:bg-zinc-100 dark:border-zinc-800 dark:bg-zinc-900 dark:text-zinc-300 dark:hover:bg-zinc-800'
      } ${props.className || ''}`}
    >
      {children}
    </button>
  )
}

export default function TopBar() {
  const pathname = usePathname()
  const qgisAvailable = useInstanceStore((s) => s.qgisAvailable)
  const instances = useInstanceStore((s) => s.instances)
  const activeInstanceId = useInstanceStore((s) => s.activeInstanceId)
  const projectConnected = useInstanceStore((s) => s.projectConnected)
  const runtimeStale = useInstanceStore((s) => s.stale)
  const sessionPanelCollapsed = useUIStore((s) => s.sessionPanelCollapsed)
  const toggleSessionPanelCollapsed = useUIStore((s) => s.toggleSessionPanelCollapsed)
  const inspectorOpen = useUIStore((s) => s.inspectorOpen)
  const toggleInspectorOpen = useUIStore((s) => s.toggleInspectorOpen)
  const runningSessionIds = useChatStore((s) => s.runningSessionIds)
  const sessionPendingInterrupts = useChatStore((s) => s.sessionPendingInterrupts)
  const inChatPage = pathname === '/chat' || pathname.startsWith('/chat/')
  const workspacePath = isWorkspacePath(pathname)
  const canToggleSessionPanel = !workspacePath && projectConnected
  const runningTaskCount = runningSessionIds.length
  const pendingInterruptCount = useMemo(
    () => runningSessionIds.reduce((sum, sessionId) => sum + (sessionPendingInterrupts[sessionId]?.length || 0), 0),
    [runningSessionIds, sessionPendingInterrupts],
  )

  const [backgroundPanelOpen, setBackgroundPanelOpen] = useState(false)
  const taskPanelWrapperRef = useRef<HTMLDivElement | null>(null)
  const taskPanelRef = useRef<HTMLDivElement | null>(null)

  const activeInstance = useMemo(
    () => instances.find((item) => item.id === activeInstanceId) || null,
    [instances, activeInstanceId],
  )

  const projectStatusText = useMemo(() => {
    if (runtimeStale) {
      return '同步中'
    }
    if (projectConnected) {
      return ''
    }
    if (instances.length === 0) {
      return 'QGIS 未连接'
    }
    if (!qgisAvailable) {
      return 'QGIS 未运行'
    }
    if (!activeInstance) {
      return '请选择实例'
    }
    if (activeInstance.status !== 'running') {
      return '实例未运行'
    }
    if (!String(activeInstance.project_path || '').trim()) {
      return '未打开项目'
    }
    return '项目未连接'
  }, [activeInstance, instances.length, projectConnected, qgisAvailable, runtimeStale])

  const sessionPanelTooltip = useMemo(() => {
    if (workspacePath) {
      return '当前工作区页面固定收起会话区'
    }
    if (!projectConnected) {
      return '连接项目后可用'
    }
    return sessionPanelCollapsed ? '展开会话区' : '收起会话区'
  }, [projectConnected, sessionPanelCollapsed, workspacePath])

  useEffect(() => {
    setBackgroundPanelOpen(false)
  }, [pathname])

  useEffect(() => {
    if (!backgroundPanelOpen) {
      return
    }

    const handleClickOutside = (event: MouseEvent) => {
      const target = event.target as Node | null
      if (!target) {
        return
      }
      if (taskPanelWrapperRef.current?.contains(target)) {
        return
      }
      if (taskPanelRef.current?.contains(target)) {
        return
      }
      setBackgroundPanelOpen(false)
    }

    const handleEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setBackgroundPanelOpen(false)
      }
    }

    document.addEventListener('mousedown', handleClickOutside)
    document.addEventListener('keydown', handleEscape)
    return () => {
      document.removeEventListener('mousedown', handleClickOutside)
      document.removeEventListener('keydown', handleEscape)
    }
  }, [backgroundPanelOpen])

  useEffect(() => {
    if (runningTaskCount === 0 && backgroundPanelOpen) {
      setBackgroundPanelOpen(false)
    }
  }, [backgroundPanelOpen, runningTaskCount])

  return (
    <header className="h-9 shrink-0 border-b border-zinc-200/70 bg-zinc-50/95 backdrop-blur supports-[backdrop-filter]:bg-zinc-50/80 dark:border-zinc-800/70 dark:bg-zinc-950/95 dark:supports-[backdrop-filter]:bg-zinc-950/85">
      <div className="flex h-full items-center justify-between gap-2 px-3">
        <div className="flex min-w-0 items-center gap-2">
          <Link href="/chat" title="AutoCarto 首页" className="shrink-0">
            <div className="flex h-[22px] w-[22px] items-center justify-center rounded bg-zinc-900 text-[10px] font-bold text-white">AC</div>
          </Link>

          <HeaderIconButton
            onClick={toggleSessionPanelCollapsed}
            title={sessionPanelTooltip}
            aria-label={sessionPanelTooltip}
            disabled={!canToggleSessionPanel}
            active={canToggleSessionPanel && !sessionPanelCollapsed}
          >
            <svg viewBox="0 0 20 20" className="h-3.5 w-3.5" fill="none" stroke="currentColor" strokeWidth="1.6" aria-hidden>
              <rect x="2.5" y="3" width="15" height="14" rx="2" />
              <path d="M13 3v14" />
              <path d="M4 7h6" />
              <path d="M4 10h6" />
              <path d="M4 13h6" />
            </svg>
          </HeaderIconButton>

          <nav className="flex min-w-0 items-center gap-1 overflow-x-auto rounded bg-zinc-100 p-0.5 dark:bg-zinc-800/50">
            {navItems.map((item) => {
              const active = pathname === item.href || pathname.startsWith(`${item.href}/`)
              const Icon = item.icon
              return (
                <Link
                  key={item.href}
                  href={item.href}
                  className={`inline-flex items-center gap-1 rounded-sm px-2.5 py-1 text-[11px] transition-colors ${
                    active
                      ? 'bg-white font-medium text-zinc-900 shadow-[0_1px_2px_rgba(0,0,0,0.06)] dark:bg-zinc-700 dark:text-zinc-100'
                      : 'text-zinc-500 hover:text-zinc-700 dark:text-zinc-400 dark:hover:text-zinc-300'
                  }`}
                >
                  <Icon className="h-3.5 w-3.5" />
                  <span>{item.label}</span>
                </Link>
              )
            })}
          </nav>
        </div>

        <div className="flex shrink-0 items-center gap-2">
          {runningTaskCount > 0 && (
            <>
              <div ref={taskPanelWrapperRef} className="relative hidden sm:block">
                <button
                  onClick={() => {
                    setBackgroundPanelOpen((prev) => !prev)
                  }}
                  title="查看后台任务"
                  className="inline-flex items-center gap-1 rounded-sm border border-blue-200 bg-blue-50 px-2 py-0.5 text-[11px] text-blue-700 dark:border-blue-900/60 dark:bg-blue-900/20 dark:text-blue-300"
                >
                  <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-blue-500" />
                  <span>后台任务 {runningTaskCount}</span>
                  {pendingInterruptCount > 0 && <Chip tone="amber" size="sm">中断 {pendingInterruptCount}</Chip>}
                </button>

                {backgroundPanelOpen && (
                  <BackgroundTaskPanel
                    anchorRef={taskPanelWrapperRef}
                    panelRef={taskPanelRef}
                    onClose={() => {
                      setBackgroundPanelOpen(false)
                    }}
                  />
                )}
              </div>

              <Link
                href="/sessions"
                title="查看后台任务会话"
                className="inline-flex items-center gap-1 rounded-sm border border-blue-200 bg-blue-50 px-2 py-0.5 text-[11px] text-blue-700 dark:border-blue-900/60 dark:bg-blue-900/20 dark:text-blue-300 sm:hidden"
              >
                <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-blue-500" />
                <span>{runningTaskCount}</span>
              </Link>
            </>
          )}

          <Link
            href="/settings"
            title="设置"
            className={`inline-flex items-center gap-1 rounded border px-2.5 py-1 text-[11px] transition-colors ${
              isSettingsPath(pathname)
                ? 'border-blue-200 bg-blue-50 font-medium text-blue-700 dark:border-blue-900/60 dark:bg-blue-900/20 dark:text-blue-300'
                : 'border-zinc-200 bg-white text-zinc-600 hover:bg-zinc-100 dark:border-zinc-800 dark:bg-zinc-900 dark:text-zinc-300 dark:hover:bg-zinc-800'
            }`}
          >
            <SettingsIcon className="h-3.5 w-3.5" />
            <span className="hidden sm:inline">设置</span>
          </Link>

          <Link
            href="/diagnostics"
            title="诊断"
            className={`inline-flex items-center gap-1 rounded border px-2.5 py-1 text-[11px] transition-colors ${
              isDiagnosticsPath(pathname)
                ? 'border-blue-200 bg-blue-50 font-medium text-blue-700 dark:border-blue-900/60 dark:bg-blue-900/20 dark:text-blue-300'
                : 'border-zinc-200 bg-white text-zinc-600 hover:bg-zinc-100 dark:border-zinc-800 dark:bg-zinc-900 dark:text-zinc-300 dark:hover:bg-zinc-800'
            }`}
          >
            <ServicesIcon className="h-3.5 w-3.5" />
            <span className="hidden sm:inline">诊断</span>
          </Link>

          <ServiceStatusIndicator
            projectConnected={projectConnected}
            projectStatusText={projectStatusText}
          />

          {inChatPage && (
            <HeaderIconButton
              onClick={toggleInspectorOpen}
              title={inspectorOpen ? '收起详情区' : '展开详情区'}
              aria-label={inspectorOpen ? '收起详情区' : '展开详情区'}
              active={inspectorOpen}
            >
              <svg viewBox="0 0 20 20" className="h-3.5 w-3.5" fill="none" stroke="currentColor" strokeWidth="1.6" aria-hidden>
                <rect x="2.5" y="3" width="15" height="14" rx="2" />
                <path d="M7 3v14" />
                <path d="M10 7h5" />
                <path d="M10 10h5" />
                <path d="M10 13h5" />
              </svg>
            </HeaderIconButton>
          )}
        </div>
      </div>
    </header>
  )
}
