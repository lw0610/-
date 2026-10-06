'use client'

import { useEffect, useMemo, useState } from 'react'
import { InterruptRequestData, TodoItem, WorkflowBlock, WorkflowEntry } from '@/types/message'
import TaskListPanel from './TaskListPanel'
import AgentBlockView from './AgentBlockView'
import Chip from '@/components/common/Chip'
import StateBlock from '@/components/common/StateBlock'

interface Props {
  tasks: TodoItem[]
  pendingInterrupts: InterruptRequestData[]
  workflowBlocks: WorkflowBlock[]
  focusedWorkflowBlockId: string | null
  onFocusWorkflowBlock: (blockId: string | null) => void
}

function asRecord(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return {}
  }
  return value as Record<string, unknown>
}

function isEntryActive(entry: WorkflowEntry): boolean {
  if (entry.toolCard?.status === 'running') {
    return true
  }

  const payload = asRecord(entry.data)
  const status = String(payload.status || payload.state || payload.progress || '').toLowerCase()
  return status === 'running' || status === 'active' || status === 'in_progress' || status === 'processing'
}

function isWorkflowBlockActive(block: WorkflowBlock): boolean {
  if (block.status === 'running') {
    return true
  }
  const entries = Array.isArray(block.entries) ? block.entries : []
  return entries.some((entry) => isEntryActive(entry))
}

function parseInterruptPreview(interrupt: InterruptRequestData) {
  const checkpointId = String(interrupt.checkpoint_id || '').trim()
  const contexts = Array.isArray(interrupt.interrupt_contexts) ? interrupt.interrupt_contexts : []
  const root = contexts.find((ctx) => ctx?.is_root_cause) || contexts[0]
  let info: Record<string, unknown> = {}

  if (root?.info && typeof root.info === 'object' && !Array.isArray(root.info)) {
    info = root.info as Record<string, unknown>
  } else if (typeof root?.info === 'string') {
    try { const parsed = JSON.parse(root.info)
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      info = parsed as Record<string, unknown>
    } else {
      info = { description: root.info }
    } } catch (error) { void error; info = { description: root.info } }
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
        <h3 className="text-[9px] font-medium uppercase tracking-[0.3px] text-zinc-400 dark:text-zinc-500">{title}</h3>
        {typeof count === 'number' && <Chip>{count} 项</Chip>}
      </div>
      {action}
    </div>
  )
}

export default function RightInspector({
  tasks,
  pendingInterrupts,
  workflowBlocks,
  focusedWorkflowBlockId,
  onFocusWorkflowBlock,
}: Props) {
  const [expandAllBlocks, setExpandAllBlocks] = useState(false)
  const [manualCollapseOverrides, setManualCollapseOverrides] = useState<Record<string, boolean>>({})
  const safeBlocks = useMemo(
    () => (Array.isArray(workflowBlocks)
      ? workflowBlocks.filter((item): item is WorkflowBlock => Boolean(item) && typeof item === 'object' && !Array.isArray(item))
      : []),
    [workflowBlocks],
  )
  const safeTasks = useMemo(
    () => (Array.isArray(tasks)
      ? tasks.filter((item): item is TodoItem => Boolean(item) && typeof item === 'object' && !Array.isArray(item))
      : []),
    [tasks],
  )
  const safeInterrupts = useMemo(
    () => (Array.isArray(pendingInterrupts)
      ? pendingInterrupts.filter((item): item is InterruptRequestData => Boolean(item) && typeof item === 'object' && !Array.isArray(item))
      : []),
    [pendingInterrupts],
  )

  const sortedWorkflowBlocks = useMemo(
    () => safeBlocks.slice().sort((a, b) => {
      const aStartedAt = typeof a.startedAt === 'number' && Number.isFinite(a.startedAt) ? a.startedAt : 0
      const bStartedAt = typeof b.startedAt === 'number' && Number.isFinite(b.startedAt) ? b.startedAt : 0
      return aStartedAt - bStartedAt
    }),
    [safeBlocks],
  )

  const activeBlockIdSet = useMemo(
    () => new Set(sortedWorkflowBlocks.filter((block) => isWorkflowBlockActive(block)).map((block, index) => String(block.id || `workflow-block-${index}`))),
    [sortedWorkflowBlocks],
  )

  const activeBlockCount = activeBlockIdSet.size

  useEffect(() => {
    setManualCollapseOverrides({})
  }, [expandAllBlocks])

  return (
    <aside className="flex h-full w-[380px] min-h-0 flex-col overflow-hidden border-l border-zinc-200 bg-[#fafafa] dark:border-zinc-800 dark:bg-zinc-950/95">
      <div className="border-b border-zinc-200 px-3 py-2 dark:border-zinc-800">
        <div className="flex items-start justify-between gap-3">
          <div>
            <h2 className="text-[10px] font-semibold text-zinc-900 dark:text-zinc-100">详情与任务</h2>
            <div className="mt-1 text-[11px] leading-5 text-zinc-500 dark:text-zinc-400">
              统一查看任务推进、待确认项与智能体完整过程。
            </div>
          </div>
          <div className="flex flex-wrap items-center justify-end gap-1.5">
            <Chip>{safeTasks.length} 任务</Chip>
            {safeInterrupts.length > 0 && <Chip tone="amber">待确认 {safeInterrupts.length}</Chip>}
            <Chip tone="blue">流程 {sortedWorkflowBlocks.length}</Chip>
          </div>
        </div>
      </div>

      <div className="flex flex-1 flex-col overflow-hidden">
        <div data-testid="inspector-task-region" className="shrink-0 max-h-[40%] overflow-y-auto p-2 scrollbar-thin">
          <TaskListPanel tasks={safeTasks} />

          {safeInterrupts.length > 0 && (
            <div className="mt-2.5">
              <section className="space-y-2">
                <SectionHeader title="待确认（只读）" count={safeInterrupts.length} />
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
                            <div className="truncate text-[11px] font-medium text-zinc-800 dark:text-zinc-100">{preview.title}</div>
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
                          {preview.decisionCount > 0 && <Chip tone="amber">决策项 {preview.decisionCount}</Chip>}
                          {preview.optionCount > 0 && <Chip tone="blue">选项 {preview.optionCount}</Chip>}
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

        <div className="h-px bg-zinc-200 dark:bg-zinc-800 shrink-0" />

        <div data-testid="inspector-workflow-region" className="flex-1 min-h-0 overflow-y-auto p-2 scrollbar-thin">
          <section className="space-y-2">
            <SectionHeader
              title="智能体完整过程"
              count={sortedWorkflowBlocks.length}
              action={
                sortedWorkflowBlocks.length > 0 ? (
                  <button
                    onClick={() => setExpandAllBlocks((prev) => !prev)}
                    title={expandAllBlocks ? '恢复自动折叠' : '展开全部流程'}
                    className="inline-flex items-center rounded-sm border border-zinc-200 bg-white px-2 py-0.5 text-[9px] text-zinc-500 transition-colors hover:border-zinc-300 hover:bg-zinc-50 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-300 dark:hover:border-zinc-600 dark:hover:bg-zinc-800"
                  >
                    {expandAllBlocks ? '自动折叠' : '展开全部'}
                  </button>
                ) : null
              }
            />

            {sortedWorkflowBlocks.length === 0 ? (
              <StateBlock
                title="暂无过程数据"
                description={'主聊天区出现\u201c查看详情\u201d入口后，可在这里查看完整过程。'}
              />
            ) : (
              <>
                {activeBlockCount > 0 && (
                  <div className="flex flex-wrap items-center gap-1.5">
                    <Chip tone="blue">活跃流程 {activeBlockCount}</Chip>
                    <Chip>已聚合时间线视图</Chip>
                  </div>
                )}
                <div className="space-y-2">
                  {sortedWorkflowBlocks.map((block, index) => {
                    const blockId = String(block.id || `workflow-block-${index}`)
                    const defaultCollapsed = expandAllBlocks
                      ? false
                      : !(activeBlockIdSet.has(blockId) || focusedWorkflowBlockId === blockId)
                    const collapsed = typeof manualCollapseOverrides[blockId] === 'boolean'
                      ? manualCollapseOverrides[blockId]
                      : defaultCollapsed
                    return (
                      <AgentBlockView
                        key={blockId}
                        block={block}
                        focused={focusedWorkflowBlockId === blockId}
                        collapsed={collapsed}
                        onFocus={() => onFocusWorkflowBlock(focusedWorkflowBlockId === blockId ? null : blockId)}
                        onToggleCollapse={() => {
                          setManualCollapseOverrides((prev) => ({
                            ...prev,
                            [blockId]: !collapsed,
                          }))
                        }}
                      />
                    )
                  })}
                </div>
              </>
            )}
          </section>
        </div>
      </div>
    </aside>
  )
}
