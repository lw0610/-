'use client'

import { useEffect, useMemo, useRef, useState } from 'react'
import { toast } from 'sonner'
import AppShell from '@/components/layout/AppShell'
import { CloseIcon, PlusIcon } from '@/components/common/AppIcon'
import ConfirmDialog from '@/components/common/ConfirmDialog'
import NewProjectDialog from '@/components/instances/NewProjectDialog'
import ProjectLaunchPopover from '@/components/instances/ProjectLaunchPopover'
import { useQGISProjectLauncher } from '@/hooks/useQGISProjectLauncher'
import api from '@/lib/api'
import { getInstanceDegradedDictionary } from '@/lib/degradedDictionary'
import { sortInstancesByCreatedAt } from '@/lib/instanceSort'
import { pickQGISProjectDirectory } from '@/lib/tauri'
import wsClient, { getInstanceSelectRollbackTarget } from '@/lib/websocket'
import { useInstanceStore } from '@/stores/instanceStore'
import type { QGISRecentProject } from '@/types/config'
import type { QGISInstance } from '@/types/instance'
import StatusBadge from '@/components/settings/StatusBadge'
import Chip, { StatusDot } from '@/components/common/Chip'
import { ActionButton } from '@/components/settings/ActionBar'
import AlertPanel from '@/components/settings/AlertPanel'
import StateBlock from '@/components/common/StateBlock'
import { useConnectionStore } from '@/stores/connectionStore'

const INSTANCE_CLOSE_WAIT_TIMEOUT_MS = 20_000
const INSTANCE_CLOSE_POLL_INTERVAL_MS = 1_000

type InstanceVariant = 'project' | 'blank' | 'other'

const statusMeta: Record<string, { label: string; dotClassName: string; badgeClassName: string }> = {
  running: {
    label: '运行中',
    dotClassName: 'bg-emerald-500',
    badgeClassName: 'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900/60 dark:bg-emerald-900/20 dark:text-emerald-300',
  },
  stopped: {
    label: '已停止',
    dotClassName: 'bg-amber-500',
    badgeClassName: 'border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-900/60 dark:bg-amber-900/20 dark:text-amber-300',
  },
  dead: {
    label: '已失效',
    dotClassName: 'bg-red-500',
    badgeClassName: 'border-red-200 bg-red-50 text-red-700 dark:border-red-900/60 dark:bg-red-900/20 dark:text-red-300',
  },
}

function isManagedRunningInstance(instance: QGISInstance) {
  return instance.status === 'running'
    && instance.source === 'reverse_ws'
}

function isReusableBlankInstance(instance: QGISInstance) {
  return isManagedRunningInstance(instance)
    && !String(instance.project_path || '').trim()
}

function getInstanceVariant(instance: QGISInstance): InstanceVariant {
  if (String(instance.project_path || '').trim()) {
    return 'project'
  }
  if (isReusableBlankInstance(instance)) {
    return 'blank'
  }
  return 'other'
}

function canCloseInstance(instance: QGISInstance) {
  return isManagedRunningInstance(instance)
}

function shouldDisplayInstance(instance: QGISInstance) {
  return instance.status !== 'stopped'
}

class InstanceCloseWaitTimeoutError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'InstanceCloseWaitTimeoutError'
  }
}

function deriveProjectName(projectPath?: string) {
  const value = String(projectPath || '').trim()
  if (!value) {
    return ''
  }
  return value.split(/[\\/]/).pop()?.replace(/\.qgz$/i, '').replace(/\.qgs$/i, '') || ''
}

function formatCompactDateTime(value?: string) {
  if (!value) {
    return ''
  }
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) {
    return value
  }
  return parsed.toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function getStatusLabel(status: string) {
  return statusMeta[status]?.label || status
}

function getProjectPath(instance: QGISInstance) {
  return String(instance.project_path || '').trim()
}

function getInstanceExplainability(instance: QGISInstance) {
  const statusDetail = String(instance.status_detail || '').trim()
  if (statusDetail) {
    return {
      label: '状态说明',
      text: statusDetail,
    }
  }
  if (instance.status !== 'running') {
    return {
      label: '状态说明',
      text: '当前实例不在运行态，可等待恢复或重新启动。',
    }
  }
  return null
}

function shouldShowStartupError(startupError: string, backendReady: boolean) {
  if (!startupError.trim()) {
    return false
  }
  if (!backendReady) {
    return true
  }
  return startupError.includes('协议版本') || startupError.includes('升级客户端')
}

function getStartupErrorAlert(startupError: string, backendReady: boolean) {
  const normalized = startupError.trim()
  if (!shouldShowStartupError(normalized, backendReady)) {
    return null
  }

  const isProtocolMismatch = normalized.includes('协议版本') || normalized.includes('升级客户端')
  if (isProtocolMismatch) {
    return {
      title: '协议版本不兼容',
      message: normalized,
    }
  }

  const isPluginRootResolutionFailure = normalized.includes('AutoCarto 插件根目录')
    || normalized.includes('AUTOCARTO_HOME')
    || normalized.includes('skills/system')
  if (isPluginRootResolutionFailure) {
    return {
      title: '插件根目录解析失败',
      message: normalized,
    }
  }

  return {
    title: '后端启动失败',
    message: normalized,
  }
}

function MetaIcon({
  label,
  tone,
  children,
}: {
  label: string
  tone: 'blue' | 'emerald' | 'amber' | 'zinc'
  children: React.ReactNode
}) {
  const toneClassName: Record<string, string> = {
    blue: 'border-blue-200 bg-blue-50 text-blue-700 dark:border-blue-900/60 dark:bg-blue-900/20 dark:text-blue-300',
    emerald: 'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900/60 dark:bg-emerald-900/20 dark:text-emerald-300',
    amber: 'border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-900/60 dark:bg-amber-900/20 dark:text-amber-300',
    zinc: 'border-zinc-200 bg-zinc-50 text-zinc-500 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-300',
  }

  return (
    <span
      title={label}
      aria-label={label}
      className={`inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-sm border ${toneClassName[tone]}`}
    >
      {children}
    </span>
  )
}

function SourceMetaIcon({ source }: { source: string }) {
  if (source === 'register') {
    return (
      <MetaIcon label="主动注册实例" tone="blue">
        <svg viewBox="0 0 20 20" className="h-3.5 w-3.5" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden>
          <path d="M6 14 14 6" />
          <path d="M8 6h6v6" />
        </svg>
      </MetaIcon>
    )
  }

  if (source === 'reverse_ws') {
    return (
      <MetaIcon label="反向连接实例" tone="emerald">
        <svg viewBox="0 0 20 20" className="h-3.5 w-3.5" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden>
          <path d="M6 7.5h5a2.5 2.5 0 1 1 0 5H8.5" />
          <path d="M11.5 7.5H9a2.5 2.5 0 1 0 0 5h5" />
          <path d="M6.5 10h7" />
        </svg>
      </MetaIcon>
    )
  }

  return (
    <MetaIcon label="扫描发现实例" tone="zinc">
      <svg viewBox="0 0 20 20" className="h-3.5 w-3.5" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden>
        <circle cx="9" cy="9" r="4" />
        <path d="m12.5 12.5 3 3" />
      </svg>
    </MetaIcon>
  )
}

function InstanceSection({
  title,
  description,
  instances,
  activeInstanceId,
  closingInstanceId,
  selectable,
  onSelect,
  onClose,
}: {
  title: string
  description: string
  instances: QGISInstance[]
  activeInstanceId: string | null
  closingInstanceId: string | null
  selectable: boolean
  onSelect: (instanceId: string) => void
  onClose: (instance: QGISInstance) => void
}) {
  if (instances.length === 0) {
    return null
  }

  return (
    <section className="rounded border border-zinc-200 bg-white px-4 py-3 dark:border-zinc-800 dark:bg-zinc-900">
      <div className="flex items-start justify-between gap-3 border-b border-zinc-200/70 pb-2.5 dark:border-zinc-800/70">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <span className="h-2 w-2 shrink-0 rounded-full bg-blue-500" />
            <h2 className="text-xs font-semibold text-zinc-800 dark:text-zinc-100">{title}</h2>
          </div>
          <p className="mt-1 text-[11px] leading-5 text-zinc-500 dark:text-zinc-400">{description}</p>
        </div>
        <Chip tone="blue" size="sm" className="shrink-0 font-medium">
          {instances.length}
        </Chip>
      </div>

      <div className="mt-3 grid gap-2.5 lg:grid-cols-2 2xl:grid-cols-3">
        {instances.map((instance) => {
          const variant = getInstanceVariant(instance)
          return (
            <InstanceCard
              key={instance.id}
              instance={instance}
              isActive={instance.id === activeInstanceId}
              closing={instance.id === closingInstanceId}
              selectable={selectable}
              variant={variant}
              onSelect={() => onSelect(instance.id)}
              onClose={() => onClose(instance)}
            />
          )
        })}
      </div>
    </section>
  )
}

function InstanceCard({
  instance,
  isActive,
  closing,
  selectable,
  variant,
  onSelect,
  onClose,
}: {
  instance: QGISInstance
  isActive: boolean
  closing: boolean
  selectable: boolean
  variant: InstanceVariant
  onSelect: () => void
  onClose: () => void
}) {
  const projectPath = getProjectPath(instance)
  const projectName = deriveProjectName(projectPath)
  const closable = canCloseInstance(instance)
  const explainability = getInstanceExplainability(instance)
  const degraded = getInstanceDegradedDictionary(instance)
  const detailLabel = explainability
    ? explainability.label
    : projectPath ? '工程路径' : variant === 'blank' ? '复用提示' : '实例信息'
  const detailText = explainability
    ? explainability.text
    : projectPath
      ? projectPath
      : variant === 'blank'
        ? '当前为空白实例，可直接复用打开工程'
        : '当前实例尚未绑定工程文件'
  const metricAccent: 'zinc' | 'blue' | 'amber' = isActive ? 'blue' : variant === 'blank' ? 'amber' : 'zinc'

  const variantClassName: Record<string, string> = {
    project: 'border-zinc-200 bg-white hover:-translate-y-0.5 hover:border-zinc-300 hover:bg-zinc-50 dark:border-zinc-800 dark:bg-zinc-900/70 dark:hover:border-zinc-700 dark:hover:bg-zinc-900',
    blank: 'border-amber-200 bg-amber-50/70 hover:-translate-y-0.5 hover:border-amber-300 hover:bg-amber-50 dark:border-amber-900/50 dark:bg-amber-950/10 dark:hover:border-amber-800 dark:hover:bg-amber-950/20',
    other: 'border-zinc-200 bg-zinc-50/70 hover:-translate-y-0.5 hover:border-zinc-300 hover:bg-zinc-50 dark:border-zinc-800 dark:bg-zinc-900/40 dark:hover:border-zinc-700 dark:hover:bg-zinc-900/70',
  }

  return (
    <div className="group relative">
      {closable && (
        <button
          type="button"
          title={closing ? '正在关闭实例' : '关闭实例'}
          aria-label={closing ? '正在关闭实例' : '关闭实例'}
          onClick={(event) => {
            event.stopPropagation()
            onClose()
          }}
          disabled={closing}
          className="absolute right-2.5 top-2.5 z-10 inline-flex h-7 w-7 items-center justify-center rounded border border-zinc-200 bg-white/90 text-zinc-400 transition-colors hover:border-red-200 hover:bg-red-50 hover:text-red-600 disabled:cursor-not-allowed disabled:border-amber-200 disabled:bg-amber-50 disabled:text-amber-600 dark:border-zinc-700 dark:bg-zinc-900/90 dark:text-zinc-400 dark:hover:border-red-900/60 dark:hover:bg-red-950/30 dark:hover:text-red-300 dark:disabled:border-amber-900/60 dark:disabled:bg-amber-950/30 dark:disabled:text-amber-300"
        >
          {closing ? (
            <span className="h-3.5 w-3.5 animate-spin rounded-full border-2 border-current border-t-transparent" />
          ) : (
            <CloseIcon className="h-3.5 w-3.5" />
          )}
        </button>
      )}

      <button
        type="button"
        disabled={!selectable}
        aria-pressed={isActive}
        onClick={selectable ? onSelect : undefined}
        className={`relative w-full rounded border p-2.5 text-left transition-all duration-150 ease-out focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:ring-offset-2 focus-visible:ring-offset-zinc-50 dark:focus-visible:ring-offset-zinc-950 ${
          closable ? 'pr-12' : ''
        } ${
          !selectable
            ? 'cursor-not-allowed opacity-60'
            : isActive
              ? 'border-blue-500 bg-blue-50/90 ring-1 ring-blue-200/80 dark:border-blue-500 dark:bg-blue-950/30 dark:ring-blue-900/60'
              : variantClassName[variant]
        }`}
      >
        {isActive && (
          <div className="pointer-events-none absolute inset-x-0 top-0 h-0.5 rounded-t-xl bg-blue-500" />
        )}

        <div className="flex items-start justify-between gap-2">
          <div className="min-w-0 flex-1">
            <div className="flex min-w-0 flex-wrap items-center gap-1.5">
              {isActive && (
                <Chip tone="blue" size="xs" className="shrink-0 font-medium">
                  当前活跃
                </Chip>
              )}
              {variant === 'blank' && !isActive && (
                <Chip tone="amber" size="xs" className="shrink-0 font-medium">
                  空白可复用
                </Chip>
              )}
              {variant === 'other' && !isActive && (
                <Chip tone="zinc" size="xs" className="shrink-0 font-medium">
                  未打开工程
                </Chip>
              )}
              <div className="min-w-0 truncate text-[11px] font-semibold text-zinc-900 dark:text-zinc-100">
                {projectName || '未打开工程'}
              </div>
            </div>

            <div className="mt-1.5 flex flex-wrap items-center gap-1.5">
              <StatusBadge tone={instance.status === 'running' ? 'success' : instance.status === 'stopped' ? 'warning' : instance.status === 'dead' ? 'danger' : 'neutral'} size="xs" className="gap-1.5">
                <StatusDot status={instance.status} className="rounded-full" />
                {getStatusLabel(instance.status)}
              </StatusBadge>
              <SourceMetaIcon source={instance.source} />
              {isReusableBlankInstance(instance) && (
                <MetaIcon label="空白实例，可复用打开工程" tone="amber">
                  <svg viewBox="0 0 20 20" className="h-3.5 w-3.5" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden>
                    <path d="M6.5 3.75h5l2.5 2.5v10h-7.5a1.5 1.5 0 0 1-1.5-1.5v-9.5a1.5 1.5 0 0 1 1.5-1.5Z" />
                    <path d="M11.5 3.75v2.5h2.5" />
                  </svg>
                </MetaIcon>
              )}
            </div>
          </div>

          <span
            title={instance.id}
            className="max-w-[120px] shrink-0 truncate rounded-sm bg-zinc-100/90 px-1.5 py-0.5 font-mono text-[10px] text-zinc-500 dark:bg-zinc-800 dark:text-zinc-400"
          >
            {instance.id}
          </span>
        </div>

        <div className="mt-2.5 grid gap-2 border-t border-zinc-200/80 pt-2 md:grid-cols-[minmax(0,1fr),auto] md:items-center dark:border-zinc-800/80">
          {degraded ? (
            <div className="md:col-span-2">
              <AlertPanel
                compact
                tone="warning"
                title={degraded.code}
                message={
                  <div className="space-y-1">
                    <div>{degraded.message}</div>
                    {degraded.user_action ? <div>{degraded.user_action}</div> : null}
                    {typeof degraded.retryable === 'boolean' ? (
                      <div>{degraded.retryable ? '可重试' : '需人工处理'}</div>
                    ) : null}
                  </div>
                }
                evidence_id={degraded.evidence_id}
              />
            </div>
          ) : null}
          <div className="min-w-0">
            <div className="text-[10px] font-medium uppercase tracking-wide text-zinc-400 dark:text-zinc-500">{detailLabel}</div>
            <div title={detailText} className="mt-0.5 truncate text-[9px] font-mono text-zinc-400 dark:text-zinc-500">
              {detailText}
            </div>
          </div>

          <div className="flex flex-wrap items-center gap-1.5 md:justify-end">
            <Chip tone={metricAccent} size="xs" className="gap-1 px-2 py-1 text-[10px]">
              <span className="uppercase tracking-wide opacity-70">PID</span>
              <span className="font-mono text-[11px]">{String(instance.pid || '-')}</span>
            </Chip>
            <Chip tone={metricAccent} size="xs" className="gap-1 px-2 py-1 text-[10px]">
              <span className="uppercase tracking-wide opacity-70">QGIS</span>
              <span className="font-mono text-[11px]">{String(instance.qgis_version || '-')}</span>
            </Chip>
            <Chip tone={metricAccent} size="xs" className="gap-1 px-2 py-1 text-[10px]">
              <span className="uppercase tracking-wide opacity-70">更新</span>
              <span className="font-mono text-[11px]">{instance.updated_at ? formatCompactDateTime(instance.updated_at) : '-'}</span>
            </Chip>
          </div>
        </div>
      </button>
    </div>
  )
}

export default function InstancesPage() {
  const { instances, activeInstanceId, qgisAvailable, loading, runningCount, fetchInstances, setActiveInstance } = useInstanceStore()
  const backendReady = useConnectionStore((s) => s.backendReady)
  const startupError = useConnectionStore((s) => s.startupError)
  const startupErrorAlert = useMemo(
    () => getStartupErrorAlert(startupError, backendReady),
    [backendReady, startupError],
  )
  const launcher = useQGISProjectLauncher()
  const [launchMenuOpen, setLaunchMenuOpen] = useState(false)
  const [newProjectDialogOpen, setNewProjectDialogOpen] = useState(false)
  const [closingInstanceId, setClosingInstanceId] = useState<string | null>(null)
  const [closeConfirmInstance, setCloseConfirmInstance] = useState<QGISInstance | null>(null)
  const [menuFocusKey, setMenuFocusKey] = useState(0)
  const launchTriggerRef = useRef<HTMLButtonElement | null>(null)

  useEffect(() => {
    void fetchInstances()
  }, [fetchInstances])

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

  const visibleInstances = useMemo(
    () => instances.filter((instance) => shouldDisplayInstance(instance)),
    [instances],
  )

  const activeInstance = useMemo(
    () => (activeInstanceId
      ? visibleInstances.find((instance) => instance.id === activeInstanceId) || null
      : null),
    [activeInstanceId, visibleInstances],
  )

  const reusableBlankCount = useMemo(
    () => visibleInstances.filter((instance) => isReusableBlankInstance(instance)).length,
    [visibleInstances],
  )

  const sortedInstances = useMemo(
    () => sortInstancesByCreatedAt(visibleInstances),
    [visibleInstances],
  )

  const handleSelectInstance = (instanceId: string) => {
    const normalizedInstanceId = instanceId.trim()
    if (!normalizedInstanceId) {
      return
    }
    const previousActiveInstanceId = activeInstanceId
    setActiveInstance(normalizedInstanceId)
    void wsClient.selectInstance(normalizedInstanceId)
      .catch((error) => {
        setActiveInstance(getInstanceSelectRollbackTarget(error, previousActiveInstanceId || null))
        toast.error(error instanceof Error ? error.message : '实例切换失败')
      })
  }

  const waitForInstanceRemoval = async (instanceId: string) => {
    const normalizedInstanceId = instanceId.trim()
    if (!normalizedInstanceId) {
      return
    }

    const deadline = Date.now() + INSTANCE_CLOSE_WAIT_TIMEOUT_MS
    while (Date.now() <= deadline) {
      await fetchInstances()
      const exists = useInstanceStore.getState().instances.some((instance) => instance.id === normalizedInstanceId)
      if (!exists) {
        return
      }
      await new Promise((resolve) => {
        window.setTimeout(resolve, INSTANCE_CLOSE_POLL_INTERVAL_MS)
      })
    }

    throw new InstanceCloseWaitTimeoutError('等待实例退出超时')
  }

  const performCloseInstance = async (instance: QGISInstance) => {
    const normalizedInstanceId = instance.id.trim()
    if (!normalizedInstanceId || !canCloseInstance(instance) || closingInstanceId) {
      return
    }

    const wasActive = useInstanceStore.getState().activeInstanceId === normalizedInstanceId
    setClosingInstanceId(normalizedInstanceId)
    setCloseConfirmInstance(null)

    try {
      await api.closeInstance(normalizedInstanceId)
      try {
        await waitForInstanceRemoval(normalizedInstanceId)
        const nextActiveInstanceId = useInstanceStore.getState().activeInstanceId
        if (wasActive && nextActiveInstanceId) {
          toast.success('实例已关闭，已自动切换到其它可用实例')
        } else if (wasActive) {
          toast.success('实例已关闭，当前已无可用实例')
        } else {
          toast.success('实例已关闭')
        }
      } catch (error) {
        if (error instanceof InstanceCloseWaitTimeoutError) {
          toast.info('关闭请求已发送，如 QGIS 弹出保存确认，请先在实例中完成后再查看列表')
          return
        }
        throw error
      }
    } catch (error) {
      toast.error(error instanceof Error ? error.message : '关闭实例失败')
    } finally {
      setClosingInstanceId((current) => (current === normalizedInstanceId ? null : current))
      void fetchInstances()
    }
  }

  const handleRequestCloseInstance = (instance: QGISInstance) => {
    if (!canCloseInstance(instance) || closingInstanceId) {
      return
    }
    if (getProjectPath(instance)) {
      setCloseConfirmInstance(instance)
      return
    }
    void performCloseInstance(instance)
  }

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
    <AppShell>
      <div className="h-full overflow-y-auto bg-zinc-50/30 dark:bg-zinc-950">
        <div className="mx-auto flex w-full max-w-[1360px] flex-col gap-3 px-3 py-2 lg:px-3 lg:py-3">
          <div className="flex flex-col gap-3 rounded border border-zinc-200 bg-white px-4 py-3 dark:border-zinc-800 dark:bg-zinc-900 xl:flex-row xl:items-start xl:justify-between">
            <div className="min-w-0 flex-1">
              <h1 className="text-[13px] font-semibold text-zinc-900 dark:text-zinc-100">GIS 实例管理</h1>
              <p className="mt-1 text-[11px] leading-5 text-zinc-500 dark:text-zinc-400">
                查看实例状态、切换当前活跃实例，并快速新建或打开工程。
              </p>
              <div className="mt-2 flex flex-wrap items-center gap-1.5">
                <Chip size="sm">共 {visibleInstances.length} 个实例</Chip>
                <Chip size="sm" tone="blue">运行中 {runningCount}</Chip>
                {reusableBlankCount > 0 && <Chip size="sm" tone="amber">空白可复用 {reusableBlankCount}</Chip>}
                {activeInstance && (
                  <Chip size="sm">
                    当前 {deriveProjectName(activeInstance.project_path) || '未打开工程'} · {getStatusLabel(activeInstance.status)}
                  </Chip>
                )}
                {!qgisAvailable && <Chip size="sm" tone="amber">当前无可用 GIS 实例</Chip>}
                {loading && <Chip size="sm" tone="blue">同步中…</Chip>}
              </div>
            </div>

            <div className="flex flex-wrap items-center gap-2">
              <div className="relative">
                <ActionButton
                  ref={launchTriggerRef}
                  variant="primary"
                  onClick={() => {
                    if (launchMenuOpen) {
                      closeLaunchMenu(true)
                    } else {
                      openLaunchMenu()
                    }
                  }}
                  onKeyDown={handleTriggerKeyDown}
                  aria-expanded={launchMenuOpen}
                  aria-haspopup="menu"
                  disabled={launcher.launching}
                  className="gap-2"
                >
                  <PlusIcon className="h-4 w-4" />
                  <span>{launcher.launching ? '处理中...' : '新建 / 打开工程'}</span>
                </ActionButton>
              </div>

              <ProjectLaunchPopover
                open={launchMenuOpen}
                anchorRef={launchTriggerRef}
                placement="bottom-end"
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

              <ActionButton
                variant="default"
                onClick={() => {
                  void fetchInstances()
                }}
                disabled={loading}
              >
                {loading ? '刷新中...' : '刷新实例'}
              </ActionButton>
            </div>
          </div>

      {startupErrorAlert ? (
        <AlertPanel
          tone="error"
          title={startupErrorAlert.title}
          message={startupErrorAlert.message}
        />
          ) : null}

          {loading && instances.length === 0 ? (
            <StateBlock
              title="正在同步实例列表"
              description="请稍候，正在从当前运行环境拉取 QGIS 实例信息。"
              loading
            />
          ) : visibleInstances.length === 0 ? (
            <StateBlock
              title="暂无 QGIS 实例"
              description="使用右上角“新建 / 打开工程”即可启动或接入新的实例。"
            />
          ) : (
            <div className="space-y-2">
              <InstanceSection
                title="所有实例"
                description="所有实例按创建时间从早到晚固定排序，状态变化或打开工程不会改变列表位置。"
                instances={sortedInstances}
                activeInstanceId={activeInstanceId}
                closingInstanceId={closingInstanceId}
                selectable={qgisAvailable}
                onSelect={handleSelectInstance}
                onClose={handleRequestCloseInstance}
              />
            </div>
          )}
        </div>
      </div>

      <NewProjectDialog
        open={newProjectDialogOpen}
        loading={launcher.launching}
        onClose={() => setNewProjectDialogOpen(false)}
        onPickDirectory={pickQGISProjectDirectory}
        onSubmit={handleCreateProject}
      />

      <ConfirmDialog
        open={closeConfirmInstance !== null}
        title="确认关闭 QGIS 实例"
        description={closeConfirmInstance
          ? `${deriveProjectName(closeConfirmInstance.project_path) || '当前工程'} 正在此实例中打开。关闭后，QGIS 可能会提示保存未提交的修改。${closeConfirmInstance.id === activeInstanceId ? ' 当前活跃实例关闭后会自动切换到其它可用实例。' : ''}`
          : ''}
        confirmText="确认关闭"
        cancelText="取消"
        danger
        loading={closeConfirmInstance !== null && closingInstanceId === closeConfirmInstance.id}
        onConfirm={() => {
          if (!closeConfirmInstance) {
            return
          }
          void performCloseInstance(closeConfirmInstance)
        }}
        onCancel={() => {
          if (!closingInstanceId) {
            setCloseConfirmInstance(null)
          }
        }}
      />
    </AppShell>
  )
}
