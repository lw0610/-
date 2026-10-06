'use client'

import { useEffect, useMemo, useRef } from 'react'
import { cn } from '@/lib/utils'
import type { QGISRecentProject } from '@/types/config'

export const PROJECT_LAUNCH_MENU_WIDTH = 360

export interface ProjectLaunchMenuProps {
  launching: boolean
  loadingRecent: boolean
  recentProjects: QGISRecentProject[]
  onCreateProject: () => void
  onOpenProject: () => void
  onLaunchRecent: (project: QGISRecentProject) => void
  onRequestClose?: () => void
  focusKey?: number
  className?: string
}

export default function ProjectLaunchMenu({
  launching,
  loadingRecent,
  recentProjects,
  onCreateProject,
  onOpenProject,
  onLaunchRecent,
  onRequestClose,
  focusKey = 0,
  className,
}: ProjectLaunchMenuProps) {
  const rootRef = useRef<HTMLDivElement | null>(null)
  const firstActionRef = useRef<HTMLButtonElement | null>(null)
  const visibleRecentProjects = useMemo(() => recentProjects.slice(0, 5), [recentProjects])

  const focusButtonAt = (index: number) => {
    const buttons = Array.from(
      rootRef.current?.querySelectorAll<HTMLButtonElement>('button[data-menu-item="true"]:not(:disabled)') || [],
    )
    if (buttons.length === 0) {
      return
    }
    const targetIndex = (index + buttons.length) % buttons.length
    buttons[targetIndex]?.focus()
  }

  useEffect(() => {
    if (!focusKey) {
      return
    }
    const rafId = window.requestAnimationFrame(() => {
      firstActionRef.current?.focus()
    })
    return () => window.cancelAnimationFrame(rafId)
  }, [focusKey, visibleRecentProjects.length])

  const handleKeyDown = (event: React.KeyboardEvent<HTMLDivElement>) => {
    const buttons = Array.from(
      rootRef.current?.querySelectorAll<HTMLButtonElement>('button[data-menu-item="true"]:not(:disabled)') || [],
    )
    if (buttons.length === 0) {
      if (event.key === 'Escape') {
        event.preventDefault()
        onRequestClose?.()
      }
      return
    }

    const currentIndex = buttons.findIndex((button) => button === document.activeElement)

    if (event.key === 'Escape') {
      event.preventDefault()
      onRequestClose?.()
      return
    }

    if (event.key === 'ArrowDown') {
      event.preventDefault()
      focusButtonAt(currentIndex >= 0 ? currentIndex + 1 : 0)
      return
    }

    if (event.key === 'ArrowUp') {
      event.preventDefault()
      focusButtonAt(currentIndex >= 0 ? currentIndex - 1 : buttons.length - 1)
      return
    }

    if (event.key === 'Home') {
      event.preventDefault()
      focusButtonAt(0)
      return
    }

    if (event.key === 'End') {
      event.preventDefault()
      focusButtonAt(buttons.length - 1)
    }
  }

  const actionButtonClassName =
    'rounded border border-zinc-200 bg-white px-3 py-2.5 text-left transition-colors hover:border-blue-200 hover:bg-blue-50/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:ring-offset-2 focus-visible:ring-offset-white disabled:opacity-50 dark:border-zinc-700 dark:bg-zinc-950/40 dark:hover:border-blue-900/60 dark:hover:bg-blue-950/20 dark:focus-visible:ring-offset-zinc-900'

  const recentButtonClassName =
    'block w-full rounded-sm border border-transparent px-2.5 py-2 text-left transition-colors hover:border-zinc-200 hover:bg-zinc-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:ring-offset-2 focus-visible:ring-offset-white disabled:opacity-50 dark:hover:border-zinc-700 dark:hover:bg-zinc-800/70 dark:focus-visible:ring-offset-zinc-900'

  const content = (
    <div ref={rootRef} onKeyDown={handleKeyDown} role="menu" aria-label="工程入口菜单">
      <div className="mb-3">
        <div className="text-xs font-semibold text-zinc-900 dark:text-zinc-100">工程入口</div>
        <div className="mt-1 text-[11px] leading-5 text-zinc-500 dark:text-zinc-400">
          启动新的 GIS 实例，或优先复用空白实例打开工程。
        </div>
      </div>

      <div className="grid gap-1.5 sm:grid-cols-2">
        <button
          ref={firstActionRef}
          data-menu-item="true"
          role="menuitem"
          type="button"
          onClick={onCreateProject}
          disabled={launching}
          className={actionButtonClassName}
        >
          <p className="text-xs font-medium text-zinc-900 dark:text-zinc-100">新建工程</p>
          <p className="mt-0.5 text-[11px] leading-5 text-zinc-500 dark:text-zinc-400">选择目录并创建新的 `.qgz` 工程</p>
        </button>

        <button
          data-menu-item="true"
          role="menuitem"
          type="button"
          onClick={onOpenProject}
          disabled={launching}
          className={actionButtonClassName}
        >
          <p className="text-xs font-medium text-zinc-900 dark:text-zinc-100">打开工程...</p>
          <p className="mt-0.5 text-[11px] leading-5 text-zinc-500 dark:text-zinc-400">选择现有 `.qgz` / `.qgs` 文件</p>
        </button>
      </div>

      <div className="mt-3 border-t border-zinc-200 pt-3 dark:border-zinc-700">
        <div className="mb-2 flex items-center justify-between gap-2">
          <p className="text-[11px] font-medium uppercase tracking-wide text-zinc-500 dark:text-zinc-400">最近工程</p>
          {visibleRecentProjects.length > 0 && (
            <span className="rounded-sm border border-zinc-200 px-1.5 py-0.5 text-[10px] text-zinc-500 dark:border-zinc-700 dark:text-zinc-400">
              {visibleRecentProjects.length} 项
            </span>
          )}
        </div>

        {loadingRecent ? (
          <p className="text-[11px] text-zinc-500 dark:text-zinc-400">加载中...</p>
        ) : visibleRecentProjects.length === 0 ? (
          <p className="rounded border border-zinc-200/80 bg-zinc-50/70 px-3 py-2 text-[11px] text-zinc-500 dark:border-zinc-800 dark:bg-zinc-900/50 dark:text-zinc-400">
            暂无最近工程记录
          </p>
        ) : (
          <div className="max-h-72 space-y-1 overflow-y-auto pr-1">
            {visibleRecentProjects.map((project) => (
              <button
                key={project.path}
                data-menu-item="true"
                role="menuitem"
                type="button"
                onClick={() => onLaunchRecent(project)}
                disabled={launching}
                className={recentButtonClassName}
                title={project.path}
              >
                <div className="truncate text-xs text-zinc-800 dark:text-zinc-100">{project.name}</div>
                <div className="mt-0.5 truncate text-[11px] text-zinc-500 dark:text-zinc-400">{project.path}</div>
              </button>
            ))}
          </div>
        )}
      </div>

      <div className="mt-3 flex items-center justify-between gap-2 border-t border-zinc-200 pt-2 text-[10px] text-zinc-500 dark:border-zinc-700 dark:text-zinc-400">
        <span>↑↓ 切换项目</span>
        <span>Esc 关闭</span>
      </div>
    </div>
  )

  return (
    <div className={cn('rounded border border-zinc-200 bg-white/95 p-3 shadow-[0_2px_8px_rgba(0,0,0,0.08)] dark:border-zinc-700 dark:bg-zinc-900/95', className)}>
      {content}
    </div>
  )
}
