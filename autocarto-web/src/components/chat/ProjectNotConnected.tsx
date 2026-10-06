'use client'

import { useMemo } from 'react'
import { useConnectionStore } from '@/stores/connectionStore'
import { useInstanceStore } from '@/stores/instanceStore'

interface PlaceholderState {
  title: string
  description: string
  badgeText: string
  tone: 'red' | 'amber' | 'blue' | 'zinc'
}

function getTrimmedProjectPath(value: string | undefined) {
  if (!value) {
    return ''
  }
  return value.trim()
}

function StatusBadge({
  children,
  tone,
}: {
  children: React.ReactNode
  tone: PlaceholderState['tone']
}) {
  const toneClassName: Record<PlaceholderState['tone'], string> = {
    red: 'border-red-200 bg-red-50 text-red-700 dark:border-red-900/60 dark:bg-red-950/40 dark:text-red-300',
    amber: 'border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-900/60 dark:bg-amber-950/40 dark:text-amber-300',
    blue: 'border-blue-200 bg-blue-50 text-blue-700 dark:border-blue-900/60 dark:bg-blue-950/40 dark:text-blue-300',
    zinc: 'border-zinc-200 bg-white text-zinc-700 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-300',
  }

  return <span className={`inline-flex items-center rounded-sm border px-2 py-0.5 text-[11px] ${toneClassName[tone]}`}>{children}</span>
}

function SummaryItem({ label, value, className = '' }: { label: string; value: string | number; className?: string }) {
  return (
    <div className={`rounded border border-zinc-200/70 bg-zinc-50/70 px-3 py-2 dark:border-zinc-800 dark:bg-zinc-950/50 ${className}`}>
      <div className="text-[10px] uppercase tracking-wide text-zinc-500 dark:text-zinc-400">{label}</div>
      <div className="mt-1 break-words text-[11px] font-medium text-zinc-800 dark:text-zinc-100">{value}</div>
    </div>
  )
}

export default function ProjectNotConnected() {
  const connected = useConnectionStore((s) => s.connected)
  const instances = useInstanceStore((s) => s.instances)
  const activeInstanceId = useInstanceStore((s) => s.activeInstanceId)
  const stale = useInstanceStore((s) => s.stale)

  const activeInstance = useMemo(
    () => instances.find((item) => item.id === activeInstanceId) || null,
    [instances, activeInstanceId],
  )

  const runningCount = useMemo(
    () => instances.filter((item) => item.status === 'running').length,
    [instances],
  )

  const state = useMemo<PlaceholderState>(() => {
    if (!connected) {
      return {
        title: '后端连接中断',
        description: '当前无法访问服务，请先恢复后端连接，再连接 QGIS 项目。',
        badgeText: '后端未连接',
        tone: 'red',
      }
    }

    if (stale) {
      return {
        title: '运行时同步中',
        description: '正在等待运行时实例状态同步恢复，当前不会将 QGIS 误判为未连接。',
        badgeText: '同步中',
        tone: 'amber',
      }
    }

    if (instances.length === 0) {
      return {
        title: 'QGIS 未连接',
        description: '请先启动 QGIS 并启用 AutoCarto 插件，等待实例注册后再继续。',
        badgeText: '暂无实例',
        tone: 'amber',
      }
    }

    if (runningCount === 0) {
      return {
        title: 'QGIS 未运行',
        description: '检测到实例记录，但当前没有运行中的 QGIS。请恢复运行后再继续。',
        badgeText: '无运行实例',
        tone: 'amber',
      }
    }

    if (!activeInstance) {
      return {
        title: '请选择实例',
        description: '请在左侧实例栏选择一个运行中的实例，然后打开 QGIS 项目。',
        badgeText: '未选择实例',
        tone: 'blue',
      }
    }

    if (activeInstance.status !== 'running') {
      return {
        title: '当前实例不可用',
        description: '所选实例不在运行中，请切换到运行实例后继续。',
        badgeText: '实例未运行',
        tone: 'amber',
      }
    }

    if (!getTrimmedProjectPath(activeInstance.project_path)) {
      return {
        title: '尚未打开 QGIS 项目',
        description: '请先在当前实例中打开项目文件，连接成功后会自动启用会话与聊天。',
        badgeText: '未绑定项目',
        tone: 'blue',
      }
    }

    return {
      title: '等待项目状态同步',
      description: '已检测到项目信息，正在同步状态，请稍候。',
      badgeText: '同步中',
      tone: 'zinc',
    }
  }, [activeInstance, connected, instances.length, runningCount, stale])

  const activeProjectPath = getTrimmedProjectPath(activeInstance?.project_path)

  return (
    <div className="flex h-full items-center justify-center bg-zinc-50/40 px-6 py-8 dark:bg-zinc-950">
      <div className="w-full max-w-3xl rounded border border-zinc-200/80 bg-white/95 p-3 shadow-[0_2px_8px_rgba(0,0,0,0.08)] dark:border-zinc-800 dark:bg-zinc-900/90 dark:shadow-black/20">
        <div className="flex flex-col gap-2 lg:flex-row lg:items-start lg:justify-between">
          <div className="flex items-start gap-3">
            <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded border border-zinc-200 bg-zinc-50 text-zinc-400 dark:border-zinc-700 dark:bg-zinc-950 dark:text-zinc-500">
              <svg viewBox="0 0 24 24" className="h-5 w-5" fill="none" stroke="currentColor" strokeWidth="1.5" aria-hidden>
                <path d="M12 9v3.75m9-.75a9 9 0 1 1-18 0 9 9 0 0 1 18 0Zm-9 3.75h.008v.008H12v-.008Z" strokeLinecap="round" strokeLinejoin="round" />
              </svg>
            </div>
            <div>
              <div className="flex flex-wrap items-center gap-2">
                <h2 className="text-[13px] font-semibold text-zinc-900 dark:text-zinc-100">{state.title}</h2>
                <StatusBadge tone={state.tone}>{state.badgeText}</StatusBadge>
              </div>
              <p className="mt-2 max-w-2xl text-xs leading-6 text-zinc-600 dark:text-zinc-300">{state.description}</p>
            </div>
          </div>
        </div>

        <div className="mt-5 grid gap-3 lg:grid-cols-[minmax(0,1.15fr)_minmax(260px,0.85fr)]">
          <div className="rounded border border-zinc-200 bg-zinc-50 p-3 dark:border-zinc-800 dark:bg-zinc-950">
            <div className="flex items-center justify-between gap-2">
              <div className="text-xs font-medium text-zinc-800 dark:text-zinc-100">连接检查</div>
              <StatusBadge tone={connected ? 'blue' : 'red'}>{connected ? '服务在线' : '服务离线'}</StatusBadge>
            </div>
            <div className="mt-3 grid grid-cols-1 gap-2 sm:grid-cols-2">
              <SummaryItem label="后端连接" value={connected ? '已连接' : '未连接'} />
              <SummaryItem label="实例总数" value={instances.length} />
              <SummaryItem label="运行实例" value={runningCount} />
              <SummaryItem label="当前实例" value={activeInstance ? activeInstance.id : '未选择'} />
              <SummaryItem label="当前项目" value={activeProjectPath || '未打开项目'} className="sm:col-span-2" />
            </div>
          </div>

          <div className="rounded border border-blue-200 bg-blue-50 p-3 dark:border-blue-900/60 dark:bg-blue-950/25">
            <div className="flex items-center justify-between gap-2">
              <div className="text-xs font-medium text-blue-900 dark:text-blue-100">操作指引</div>
              <StatusBadge tone="blue">3 步完成</StatusBadge>
            </div>
            <div className="mt-3 space-y-2 text-xs leading-6 text-blue-900 dark:text-blue-100">
              <div>1. 启动 QGIS 并确保 AutoCarto 插件处于连接状态。</div>
              <div>2. 在左侧实例栏选择一个运行中的实例。</div>
              <div>3. 在 QGIS 中打开项目文件（.qgs / .qgz）。</div>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
