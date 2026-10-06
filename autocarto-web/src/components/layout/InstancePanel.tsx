'use client'

import { useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { toast } from 'sonner'
import Chip, { MetricPill } from '@/components/common/Chip'
import { InstancesIcon, PlusIcon } from '@/components/common/AppIcon'
import NewProjectDialog from '@/components/instances/NewProjectDialog'
import ProjectLaunchPopover from '@/components/instances/ProjectLaunchPopover'
import { useInstanceTooltip } from '@/hooks/useInstanceTooltip'
import type { TooltipState, TooltipSummaryView } from '@/hooks/useInstanceTooltip'
import { useQGISProjectLauncher } from '@/hooks/useQGISProjectLauncher'
import { deriveProjectName } from '@/lib/qgisProjects'
import { sortInstancesByCreatedAt } from '@/lib/instanceSort'
import { pickQGISProjectDirectory } from '@/lib/tauri'
import type { QGISRecentProject } from '@/types/config'
import wsClient, { getInstanceSelectRollbackTarget } from '@/lib/websocket'
import type { QGISInstance } from '@/types/instance'
import { useInstanceStore } from '@/stores/instanceStore'

interface InstancePanelProps {
  sessionPanelCollapsed: boolean
  onInstanceHover?: (instanceId: string | null) => void
}

type InstanceVariant = 'project' | 'blank' | 'other'

function getStatusDotClass(status: string) {
  if (status === 'running') {
    return 'bg-emerald-500'
  }
  if (status === 'stopped') {
    return 'bg-amber-500'
  }
  return 'bg-red-500'
}

function getStatusText(status: string) {
  if (status === 'running') {
    return '运行中'
  }
  if (status === 'stopped') {
    return '已停止'
  }
  return '异常'
}

function getProjectPath(instance: QGISInstance) {
  const value = typeof instance.project_path === 'string' ? instance.project_path.trim() : ''
  return value
}

function isReusableBlankInstance(instance: QGISInstance) {
  return instance.status === 'running'
    && instance.source === 'reverse_ws'
    && !getProjectPath(instance)
}

function getInstanceVariant(instance: QGISInstance): InstanceVariant {
  if (getProjectPath(instance)) {
    return 'project'
  }
  if (isReusableBlankInstance(instance)) {
    return 'blank'
  }
  return 'other'
}

const instanceButtonClassName: Record<InstanceVariant, string> = {
  project: 'border-zinc-200 bg-white text-zinc-600 hover:-translate-y-0.5 hover:border-zinc-300 hover:bg-zinc-50 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-300 dark:hover:border-zinc-600 dark:hover:bg-zinc-800',
  blank: 'border-amber-200 bg-amber-50 text-amber-700 hover:-translate-y-0.5 hover:border-amber-300 hover:bg-amber-50/90 dark:border-amber-900/60 dark:bg-amber-900/20 dark:text-amber-300 dark:hover:border-amber-800 dark:hover:bg-amber-900/25',
  other: 'border-zinc-200 bg-zinc-50 text-zinc-500 hover:-translate-y-0.5 hover:border-zinc-300 hover:bg-zinc-50 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-400 dark:hover:border-zinc-600 dark:hover:bg-zinc-800',
}

interface InstanceTooltipPortalProps {
  activeInstanceId: string | null
  currentTooltipSummary: TooltipSummaryView
  sessionPanelCollapsed: boolean
  tooltipInstance: QGISInstance | null
  tooltipState: TooltipState | null
}

function InstanceTooltipPortal({
  activeInstanceId,
  currentTooltipSummary,
  sessionPanelCollapsed,
  tooltipInstance,
  tooltipState,
}: InstanceTooltipPortalProps) {
  if (typeof document === 'undefined' || !tooltipState || !tooltipInstance || sessionPanelCollapsed) {
    return null
  }

  return createPortal(
    <div
      className="fixed z-50 w-72 rounded border border-zinc-200 bg-white/95 p-3 shadow-[0_2px_8px_rgba(0,0,0,0.08)] dark:border-zinc-700 dark:bg-zinc-900/95"
      style={{
        top: tooltipState.top,
        left: tooltipState.left,
        transform: 'translateY(-50%)',
      }}
    >
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0 flex-1">
          <div className="truncate text-xs font-semibold text-zinc-900 dark:text-zinc-100">
            {getProjectPath(tooltipInstance)
              ? deriveProjectName(getProjectPath(tooltipInstance))
              : '未打开工程'}
          </div>
          <div className="mt-1 flex flex-wrap items-center gap-1.5">
            <Chip dot dotClassName={getStatusDotClass(tooltipInstance.status)}>
              {getStatusText(tooltipInstance.status)}
            </Chip>
            {tooltipInstance.id === activeInstanceId && <Chip tone="blue">当前活跃</Chip>}
            {getInstanceVariant(tooltipInstance) === 'blank' && <Chip tone="amber">空白可复用</Chip>}
          </div>
        </div>
        <span className="max-w-[112px] truncate rounded-sm bg-zinc-100 px-1.5 py-0.5 font-mono text-[10px] text-zinc-500 dark:bg-zinc-800 dark:text-zinc-400">
          {tooltipInstance.id}
        </span>
      </div>

      <div className="mt-3 rounded border border-zinc-200/80 bg-zinc-50/70 px-2.5 py-2 dark:border-zinc-800 dark:bg-zinc-950/40">
        <div className="text-[10px] font-medium uppercase tracking-wide text-zinc-400 dark:text-zinc-500">
          {getProjectPath(tooltipInstance) ? '工程路径' : '实例信息'}
        </div>
        <div className="mt-0.5 truncate text-[11px] text-zinc-600 dark:text-zinc-300">
          {getProjectPath(tooltipInstance) || '当前实例尚未打开工程'}
        </div>
      </div>

      <div className="mt-2 flex flex-wrap items-center gap-1.5">
        <MetricPill label="PID" value={String(tooltipInstance.pid || '-')} />
        <MetricPill label="QGIS" value={String(tooltipInstance.qgis_version || '-')} />
      </div>

      <div className="my-3 border-t border-zinc-200 dark:border-zinc-700" />

      {currentTooltipSummary.loading ? (
        <div className="text-[11px] text-zinc-500">加载会话信息中...</div>
      ) : currentTooltipSummary.error ? (
        <div className="text-[11px] text-zinc-500">会话信息加载失败</div>
      ) : (
        <>
          <div className="text-[11px] text-zinc-500">{currentTooltipSummary.total} 个会话</div>
          {currentTooltipSummary.recent.length > 0 ? (
            <div className="mt-1.5 space-y-1">
              {currentTooltipSummary.recent.map((name) => (
                <div key={name} className="truncate rounded-sm bg-zinc-50 px-2 py-1 text-[11px] text-zinc-600 dark:bg-zinc-800/70 dark:text-zinc-300">
                  {name}
                </div>
              ))}
            </div>
          ) : (
            <div className="mt-1 text-[11px] text-zinc-500">暂无会话</div>
          )}
        </>
      )}
    </div>,
    document.body,
  )
}

function InstanceGlyph({ variant, status, active }: { variant: InstanceVariant; status: string; active: boolean }) {
  const wrapperClassName: Record<InstanceVariant, string> = {
    project: 'border-zinc-200 bg-white text-zinc-600 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-300',
    blank: 'border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-900/60 dark:bg-amber-900/20 dark:text-amber-300',
    other: 'border-zinc-200 bg-zinc-50 text-zinc-500 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-400',
  }

  return (
            <span className={`relative inline-flex h-4.5 w-4.5 items-center justify-center rounded-sm border ${wrapperClassName[variant]}`}>
      {variant === 'project' ? (
        <svg viewBox="0 0 20 20" className="h-3 w-3" fill="none" stroke="currentColor" strokeWidth="1.7" aria-hidden>
          <path d="M6.5 3.75h5l2.5 2.5v10h-7.5a1.5 1.5 0 0 1-1.5-1.5v-9.5a1.5 1.5 0 0 1 1.5-1.5Z" />
          <path d="M11.5 3.75v2.5h2.5" />
        </svg>
      ) : variant === 'blank' ? (
        <svg viewBox="0 0 20 20" className="h-3 w-3" fill="none" stroke="currentColor" strokeWidth="1.7" aria-hidden>
          <path d="M6.5 3.75h5l2.5 2.5v10h-7.5a1.5 1.5 0 0 1-1.5-1.5v-9.5a1.5 1.5 0 0 1 1.5-1.5Z" />
          <path d="M11.5 3.75v2.5h2.5" />
          <path d="M10 8v4.5" />
          <path d="M7.75 10.25h4.5" />
        </svg>
      ) : (
        <svg viewBox="0 0 20 20" className="h-3 w-3" fill="none" stroke="currentColor" strokeWidth="1.7" aria-hidden>
          <rect x="4.5" y="4.5" width="11" height="11" rx="2" />
          <path d="M7.5 7.5h5" />
          <path d="M7.5 10h5" />
          <path d="M7.5 12.5h3" />
        </svg>
      )}
      <span className={`absolute -bottom-0.5 -right-0.5 h-2 w-2 rounded-full border border-white dark:border-zinc-950 ${getStatusDotClass(status)}`} />
      {active && <span className="absolute -left-0.5 -top-0.5 h-1.5 w-1.5 rounded-full bg-blue-500" />}
    </span>
  )
}

export default function InstancePanel({ sessionPanelCollapsed, onInstanceHover }: InstancePanelProps) {
  const qgisAvailable = useInstanceStore((s) => s.qgisAvailable)
  const instances = useInstanceStore((s) => s.instances)
  const activeInstanceId = useInstanceStore((s) => s.activeInstanceId)
  const setActiveInstance = useInstanceStore((s) => s.setActiveInstance)
  const loading = useInstanceStore((s) => s.loading)
  const launcher = useQGISProjectLauncher()

  const [launchMenuOpen, setLaunchMenuOpen] = useState(false)
  const [menuFocusKey, setMenuFocusKey] = useState(0)
  const [newProjectDialogOpen, setNewProjectDialogOpen] = useState(false)
  const launchTriggerRef = useRef<HTMLButtonElement | null>(null)

  const {
    currentTooltipSummary,
    handleInstanceMouseEnter,
    handleInstanceMouseLeave,
    handlePanelMouseLeave,
    tooltipInstance,
    tooltipState,
  } = useInstanceTooltip({
    instances,
    sessionPanelCollapsed,
    onInstanceHover,
  })

  const closeLaunchMenu = (restoreFocus = false) => {
    setLaunchMenuOpen(false)
    if (restoreFocus) {
      window.requestAnimationFrame(() => {
        launchTriggerRef.current?.focus()
      })
    }
  }

  const openLaunchMenu = () => {
    setLaunchMenuOpen(true)
    setMenuFocusKey((prev) => prev + 1)
    void launcher.loadRecentProjects()
  }

  const handleSelect = (id: string) => {
    if (!qgisAvailable || !id) {
      return
    }
    const previousActiveInstanceId = activeInstanceId
    setActiveInstance(id)
    void wsClient.selectInstance(id)
      .catch((error) => {
        setActiveInstance(getInstanceSelectRollbackTarget(error, previousActiveInstanceId || null))
        toast.error(error instanceof Error ? error.message : '实例切换失败')
      })
  }

  const orderedInstances = useMemo(() => sortInstancesByCreatedAt(instances), [instances])

  const handleTriggerKeyDown = (event: React.KeyboardEvent<HTMLButtonElement>) => {
    if (event.key === 'ArrowDown' || event.key === 'Enter' || event.key === ' ') {
      event.preventDefault()
      if (!launchMenuOpen) {
        openLaunchMenu()
      }
    }
  }

  const handleOpenProject = () => {
    closeLaunchMenu(false)
    void launcher.openProjectFromPicker()
      .then((result) => {
        if (!result) {
          return
        }
        toast.success(result.reused ? '已复用空白 QGIS 实例并打开工程' : '已启动新 QGIS 并自动打开工程')
      })
      .catch((error) => {
        toast.error(error instanceof Error ? error.message : '启动 QGIS 失败')
      })
  }

  const handleOpenRecentProject = (project: QGISRecentProject) => {
    closeLaunchMenu(false)
    void launcher.openRecentProject(project)
      .then((result) => {
        toast.success(result.reused ? '已复用空白 QGIS 实例并打开最近工程' : '已启动新 QGIS 并自动打开最近工程')
      })
      .catch((error) => {
        toast.error(error instanceof Error ? error.message : '启动 QGIS 失败')
      })
  }

  const handleCreateProject = async (payload: { directoryPath: string; projectName: string }) => {
    await launcher.createProject(payload)
    setNewProjectDialogOpen(false)
    closeLaunchMenu(false)
    toast.success('已启动新 QGIS 并创建工程')
  }

  return (
    <aside
      className="relative flex h-full min-h-0 w-12 shrink-0 flex-col border-r border-zinc-200/60 bg-zinc-50/95 dark:border-zinc-800/60 dark:bg-zinc-950"
      onMouseLeave={handlePanelMouseLeave}
    >
      <div className="flex items-center justify-center border-b border-zinc-200/70 py-2 dark:border-zinc-800/70">
        <div className="relative rounded border border-zinc-200 bg-white p-1 dark:border-zinc-700 dark:bg-zinc-900">
          <InstancesIcon className="h-4 w-4 text-zinc-500 dark:text-zinc-400" />
          {orderedInstances.length > 0 && (
            <span className="absolute -right-1 -top-1 inline-flex min-w-4 items-center justify-center rounded-sm bg-blue-500 px-1 text-[9px] font-medium text-white">
              {Math.min(orderedInstances.length, 9)}
            </span>
          )}
        </div>
      </div>

      <div className="flex-1 overflow-y-auto py-2 scrollbar-thin">
        <div className="flex flex-col items-center gap-1.5">
          {loading && orderedInstances.length === 0 ? (
            <span className="inline-flex h-7 w-7 items-center justify-center rounded border border-zinc-200 bg-white text-zinc-400 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-500">
              <span className="h-3.5 w-3.5 animate-spin rounded-full border-2 border-current border-t-transparent" />
            </span>
          ) : orderedInstances.length === 0 ? (
            <span className="inline-flex h-7 w-7 items-center justify-center rounded border border-zinc-200/80 bg-zinc-50/70 text-[10px] text-zinc-400 dark:border-zinc-800 dark:bg-zinc-950/60 dark:text-zinc-500">
              -
            </span>
          ) : (
            orderedInstances.map((inst) => {
              const variant = getInstanceVariant(inst)
              const active = inst.id === activeInstanceId

              return (
                <button
                  key={inst.id}
                  onClick={() => handleSelect(inst.id)}
                  onMouseEnter={(event) => handleInstanceMouseEnter(inst, event)}
                  onMouseLeave={() => handleInstanceMouseLeave(inst.id)}
                  disabled={!qgisAvailable}
                  title={`${deriveProjectName(getProjectPath(inst)) || '未打开工程'} · ${getStatusText(inst.status)}`}
                  className={`relative flex h-7 w-7 items-center justify-center rounded border transition-all duration-150 ease-out focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:ring-offset-2 focus-visible:ring-offset-zinc-50 dark:focus-visible:ring-offset-zinc-950 ${
                    active
                      ? 'border-blue-300 bg-blue-50 ring-1 ring-blue-200 dark:border-blue-700 dark:bg-blue-950/30 dark:ring-blue-900/60'
                      : instanceButtonClassName[variant]
                  } ${!qgisAvailable ? 'cursor-not-allowed opacity-50' : ''}`}
                >
                  {active && <span className="absolute inset-x-1 top-0 h-0.5 rounded-full bg-blue-500" />}
                  <InstanceGlyph variant={variant} status={inst.status} active={active} />
                </button>
              )
            })
          )}

          <div className="relative pt-1.5">
            <button
              ref={launchTriggerRef}
              type="button"
              title="新建或打开工程"
              aria-label="新建或打开工程"
              aria-expanded={launchMenuOpen}
              aria-haspopup="menu"
              className="flex h-7 w-7 items-center justify-center rounded border border-blue-200 bg-blue-50/80 text-blue-600 transition-colors hover:bg-blue-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:ring-offset-2 focus-visible:ring-offset-zinc-50 dark:border-blue-800 dark:bg-blue-950/25 dark:text-blue-300 dark:hover:bg-blue-950/40 dark:focus-visible:ring-offset-zinc-950"
              onClick={() => {
                if (launchMenuOpen) {
                  closeLaunchMenu(true)
                } else {
                  openLaunchMenu()
                }
              }}
              onKeyDown={handleTriggerKeyDown}
              disabled={launcher.launching}
            >
              <PlusIcon className="h-4 w-4" />
            </button>
          </div>

          <ProjectLaunchPopover
            open={launchMenuOpen}
            anchorRef={launchTriggerRef}
            placement="right-start"
            launching={launcher.launching}
            loadingRecent={launcher.loadingRecent}
            recentProjects={launcher.recentProjects}
            focusKey={menuFocusKey}
            onRequestClose={closeLaunchMenu}
            onCreateProject={() => {
              closeLaunchMenu(false)
              setNewProjectDialogOpen(true)
            }}
            onOpenProject={handleOpenProject}
            onLaunchRecent={handleOpenRecentProject}
          />
        </div>
      </div>

      <InstanceTooltipPortal
        activeInstanceId={activeInstanceId}
        currentTooltipSummary={currentTooltipSummary}
        sessionPanelCollapsed={sessionPanelCollapsed}
        tooltipInstance={tooltipInstance}
        tooltipState={tooltipState}
      />

      <NewProjectDialog
        open={newProjectDialogOpen}
        loading={launcher.launching}
        onClose={() => setNewProjectDialogOpen(false)}
        onPickDirectory={pickQGISProjectDirectory}
        onSubmit={handleCreateProject}
      />
    </aside>
  )
}
