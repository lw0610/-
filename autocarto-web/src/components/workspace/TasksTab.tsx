'use client'
import type { InterruptRequestData, TodoItem } from '@/types/message'
import TaskListPanel from '@/components/chat/TaskListPanel'
import Chip from '@/components/common/Chip'
import AcquisitionProgress from '@/features/plan/AcquisitionProgress'
import type { FlowRunState } from '@/features/plan/types'
import { terminalFlow } from '@/features/plan/model'
function parseInterruptPreview(interrupt: InterruptRequestData) {
  const checkpointId = String(interrupt.checkpoint_id || '').trim()
  const contexts = Array.isArray(interrupt.interrupt_contexts)
    ? interrupt.interrupt_contexts
    : []
  const root = contexts.find((ctx) => ctx?.is_root_cause) || contexts[0]
  let info: Record<string, unknown> = {}

  if (
    root?.info &&
    typeof root.info === 'object' &&
    !Array.isArray(root.info)
  ) {
    info = root.info as Record<string, unknown>
  } else if (typeof root?.info === 'string') {
    try {
      const parsed = JSON.parse(root.info)
      if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
        info = parsed as Record<string, unknown>
      } else {
        info = { description: root.info }
      }
    } catch (error) {
      void error
      info = { description: root.info }
    }
  }

  const title = String(info.title || '需要确认').trim() || '需要确认'
  const description = String(info.description || '').trim()
  const type = String(info.type || '').trim()
  const selectMode = String(info.select_mode || '').trim()
  const decisions = Array.isArray(info.decisions) ? info.decisions : []
  const options = Array.isArray(info.options) ? info.options : []

  return {
    checkpointId,
    title,
    description,
    type,
    selectMode,
    decisionCount: decisions.length,
    optionCount: options.length,
  }
}

function SectionHeader({
  title,
  count,
  action,
}: {
  title: string
  count?: number
  action?: React.ReactNode
}) {
  return (
    <div className="flex items-center justify-between gap-2">
      <div className="flex items-center gap-2">
        <h3 className="text-[9px] font-medium uppercase tracking-[0.3px] text-zinc-400 dark:text-zinc-500">
          {title}
        </h3>
        {typeof count === 'number' && <Chip>{count} 项</Chip>}
      </div>
      {action}
    </div>
  )
}

export default function TasksTab({
  tasks,
  pendingInterrupts,
  run,
  embedded = false,
}: {
  tasks: TodoItem[]
  pendingInterrupts: InterruptRequestData[]
  run?: FlowRunState
  embedded?: boolean
}) {
  const safeTasks = Array.isArray(tasks) ? tasks.filter(Boolean) : []
  const safeInterrupts = Array.isArray(pendingInterrupts)
    ? pendingInterrupts.filter(Boolean)
    : []
  return (
    <div
      data-testid="workspace-task-region"
      className={embedded ? 'p-3' : 'h-full overflow-y-auto p-3 scrollbar-thin'}
    >
      <TaskListPanel tasks={safeTasks} />
      {run?.acquisition && <AcquisitionProgress state={run.acquisition} finished={terminalFlow(run.status)} />}

      {safeInterrupts.length > 0 && (
        <div className="mt-2.5">
          <section className="space-y-2">
            <SectionHeader
              title="待确认（只读）"
              count={safeInterrupts.length}
            />
            <div className="space-y-2">
              {safeInterrupts.map((interrupt, index) => {
                const preview = parseInterruptPreview(interrupt)
                return (
                  <div
                    key={preview.checkpointId || `interrupt-preview-${index}`}
                    className="rounded border border-zinc-200 bg-white p-3 dark:border-zinc-800 dark:bg-zinc-900/80"
                  >
                    <div className="flex items-start justify-between gap-2">
                      <div className="min-w-0">
                        <div className="truncate text-[11px] font-medium text-zinc-800 dark:text-zinc-100">
                          {preview.title}
                        </div>
                        {preview.description && (
                          <div className="mt-1 whitespace-pre-wrap break-words text-[11px] leading-5 text-zinc-600 dark:text-zinc-300">
                            {preview.description}
                          </div>
                        )}
                      </div>
                      <Chip>{preview.checkpointId || 'checkpoint'}</Chip>
                    </div>

                    <div className="mt-2 flex flex-wrap items-center gap-1.5 text-[10px] text-zinc-500 dark:text-zinc-400">
                      {preview.type && <Chip>{preview.type}</Chip>}
                      {preview.selectMode && <Chip>{preview.selectMode}</Chip>}
                      {preview.decisionCount > 0 && (
                        <Chip tone="amber">决策项 {preview.decisionCount}</Chip>
                      )}
                      {preview.optionCount > 0 && (
                        <Chip tone="blue">选项 {preview.optionCount}</Chip>
                      )}
                    </div>

                    <div className="mt-2 rounded-sm border border-amber-200 bg-amber-50/70 px-2.5 py-2 text-[11px] leading-5 text-amber-800 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-200">
                      请在主聊天区完成选择；这里保留只读详情，便于对照上下文。
                    </div>
                  </div>
                )
              })}
            </div>
          </section>
        </div>
      )}
    </div>
  )
}
