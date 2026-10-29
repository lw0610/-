'use client'

import { useMemo, useState } from 'react'
import { TodoItem } from '@/types/message'
import { cn } from '@/lib/utils'
import Chip from '@/components/common/Chip'

interface Props {
  tasks: TodoItem[]
  className?: string
}

const statusLabel: Record<string, string> = {
  todo: '待开始',
  in_progress: '进行中',
  done: '已完成',
  blocked: '阻塞',
  waiting_user: '等待确认',
  interrupted: '已中断',
  skipped: '已跳过',
  failed: '失败',
  cancelled: '已终止',
}

const statusBadgeClassName: Record<string, string> = {
  todo: 'border-zinc-200 bg-white text-zinc-500 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-300',
  in_progress: 'border-blue-200 bg-blue-50 text-blue-700 dark:border-blue-900/60 dark:bg-blue-900/20 dark:text-blue-300',
  done: 'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900/60 dark:bg-emerald-900/20 dark:text-emerald-300',
  blocked: 'border-red-200 bg-red-50 text-red-700 dark:border-red-900/60 dark:bg-red-900/20 dark:text-red-300',
}

const taskRowClassName: Record<string, string> = {
  todo: 'border-l-zinc-300 bg-white text-zinc-600 dark:bg-zinc-900/70 dark:text-zinc-300',
  in_progress: 'border-l-blue-500 bg-[#f0f9ff] text-zinc-900 dark:bg-blue-950/30 dark:text-zinc-100',
  done: 'border-l-emerald-500 bg-white text-zinc-400 dark:bg-zinc-900/70 dark:text-zinc-500',
  blocked: 'border-l-red-500 bg-white text-zinc-900 dark:bg-zinc-900/70 dark:text-zinc-100',
}

function normalizeStatus(raw: unknown) {
  const status = String(raw || 'todo').trim().toLowerCase()
  if (['waiting_user', 'interrupted', 'skipped', 'failed', 'cancelled'].includes(status)) return status
  if (status === 'done' || status === 'completed' || status === 'success' || status === 'succeeded') {
    return 'done'
  }
  if (status === 'in_progress' || status === 'running' || status === 'processing' || status === 'active') {
    return 'in_progress'
  }
  if (status === 'blocked' || status === 'failed' || status === 'error' || status === 'cancelled' || status === 'canceled' || status === 'aborted' || status === 'skipped') {
    return 'blocked'
  }
  return 'todo'
}

export default function TaskListPanel({ tasks, className }: Props) {
  const [collapsed, setCollapsed] = useState(false)
  const safeTasks = useMemo(
    () => (Array.isArray(tasks)
      ? tasks.filter((item): item is TodoItem => Boolean(item) && typeof item === 'object' && !Array.isArray(item))
      : []),
    [tasks],
  )

  const summary = useMemo(() => {
    const total = safeTasks.length
    let done = 0
    let running = 0
    let blocked = 0
    safeTasks.forEach((task) => {
      const status = normalizeStatus(task.status)
      if (status === 'done' || status === 'skipped') done += 1
      if (status === 'in_progress') running += 1
      if (['blocked', 'failed', 'interrupted'].includes(status)) blocked += 1
    })
    const progress = total > 0 ? Math.round((done / total) * 100) : 0
    return { total, done, running, blocked, progress }
  }, [safeTasks])

  if (safeTasks.length === 0) {
    return (
      <div className={cn('border border-zinc-200 bg-white px-3 py-2 dark:border-zinc-800 dark:bg-zinc-900', className)}>
        <div className="flex items-start justify-between gap-3">
          <div>
            <div className="flex items-center gap-2">
              <div className="text-[10px] font-semibold text-zinc-900 dark:text-zinc-100">任务进度</div>
              <div className="font-mono text-[9px] text-zinc-400 dark:text-zinc-500">0/0</div>
            </div>
            <div className="mt-1 text-[11px] leading-5 text-zinc-500 dark:text-zinc-400">
              当前还没有生成任务清单，待智能体开始规划后会在这里显示。
            </div>
          </div>
          <Chip>0 项</Chip>
        </div>
      </div>
    )
  }

  return (
    <div className={cn('border border-zinc-200 bg-white p-3 dark:border-zinc-800 dark:bg-zinc-900', className)}>
      <div className="flex items-start justify-between gap-3">
        <div>
          <div className="flex items-center gap-2">
            <div className="text-[10px] font-semibold text-zinc-900 dark:text-zinc-100">任务进度</div>
            <div className="font-mono text-[9px] text-zinc-400 dark:text-zinc-500">{summary.done}/{summary.total}</div>
          </div>
          <div className="mt-1 text-[11px] leading-5 text-zinc-500 dark:text-zinc-400">
            {summary.done}/{summary.total} 已完成，当前保持紧凑追踪。
          </div>
        </div>

        <div className="flex flex-wrap items-center justify-end gap-1.5">
          <Chip tone="emerald">完成 {summary.done}</Chip>
          {summary.running > 0 && <Chip tone="blue">进行中 {summary.running}</Chip>}
          {summary.blocked > 0 && <Chip tone="red">阻塞 {summary.blocked}</Chip>}
          <button
            onClick={() => setCollapsed((prev) => !prev)}
            className="inline-flex items-center rounded-sm border border-zinc-200 bg-white px-2 py-0.5 text-[8px] text-zinc-500 transition-colors hover:border-zinc-300 hover:bg-zinc-50 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-300 dark:hover:border-zinc-600 dark:hover:bg-zinc-800"
          >
            {collapsed ? '展开' : '收起'}
          </button>
        </div>
      </div>

      <div className="mt-2 flex items-center gap-2">
        <div className="h-0.5 min-w-0 flex-1 bg-zinc-100 dark:bg-zinc-800">
          <div
            className="h-0.5 bg-emerald-500 transition-all"
            style={{ width: `${summary.progress}%` }}
          />
        </div>
        <span className="shrink-0 text-[10px] text-zinc-500 dark:text-zinc-400">{summary.progress}%</span>
      </div>

      {!collapsed && (
        <div className="mt-2 max-h-56 space-y-1.5 overflow-y-auto pr-0.5 scrollbar-thin">
          {safeTasks.map((task, idx) => {
            const status = normalizeStatus(task.status)
            const label = statusLabel[status] || status
            const tone = ['blocked', 'failed', 'interrupted'].includes(status) ? 'blocked' : status === 'waiting_user' ? 'in_progress' : status === 'skipped' ? 'done' : status
            const statusCls = statusBadgeClassName[tone] || statusBadgeClassName.todo
            const rowCls = taskRowClassName[tone] || taskRowClassName.todo
            const taskContent = String(task.content || `任务 ${idx + 1}`)
            const taskPriority = String(task.priority || '').trim()
            return (
              <div
                key={`${idx}-${taskContent}`}
                className={cn('border-l-2 px-2.5 py-2', rowCls)}
              >
                <div className="flex items-start gap-2">
                  {status === 'in_progress' ? (
                    <span className="mt-1.5 h-1.5 w-1.5 shrink-0 rounded-full bg-blue-500 animate-pulse" />
                  ) : (
                    <span className="mt-1.5 h-1.5 w-1.5 shrink-0 rounded-full bg-transparent" />
                  )}
                  <div className="min-w-0 flex-1">
                    <div className="flex items-start gap-2">
                      <span className={cn('mt-0.5 shrink-0 rounded-sm border px-1.5 py-0.5 text-[8px]', statusCls)}>
                        {label}
                      </span>
                      <div
                        className={cn(
                          'min-w-0 flex-1 break-words text-[11px] leading-5',
                          status === 'done' && 'line-through text-zinc-400 dark:text-zinc-500',
                          status === 'in_progress' && 'font-medium text-zinc-900 dark:text-zinc-100',
                          status === 'blocked' && 'text-zinc-900 dark:text-zinc-100',
                          status === 'todo' && 'text-zinc-600 dark:text-zinc-300',
                        )}
                      >
                        {taskContent}
                      </div>
                      {taskPriority && (
                        <span className="shrink-0 rounded-sm border border-zinc-200 bg-white px-1.5 py-0.5 text-[8px] text-zinc-500 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-400">
                          P{taskPriority}
                        </span>
                      )}
                    </div>

                  </div>
                </div>
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
