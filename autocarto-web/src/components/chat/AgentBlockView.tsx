'use client'

import { useEffect, useMemo, useState } from 'react'
import { normalizeWorkflowBlockStatus, workflowToolStatusForDisplay } from '@/lib/chat-message-utils'
import { WorkflowBlock, WorkflowEntry } from '@/types/message'
import { cn, formatTimestamp } from '@/lib/utils'
import ToolCallCard from './ToolCallCard'
import MarkdownContent from './MarkdownContent'

import CollapseIndicator from '@/components/common/CollapseIndicator'

interface Props {
  block: WorkflowBlock
  focused: boolean
  collapsed: boolean
  onFocus: () => void
  onToggleCollapse: () => void
}

const statusLabel: Record<WorkflowBlock['status'], string> = {
  running: '进行中',
  waiting_user: '等待确认',
  completed: '已完成',
  error: '异常',
}

const statusClassName: Record<WorkflowBlock['status'], string> = {
  running: 'border-blue-300 bg-blue-50 text-blue-700 dark:border-blue-800 dark:bg-blue-900/20 dark:text-blue-300',
  waiting_user: 'border-amber-300 bg-amber-50 text-amber-700 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-300',
  completed: 'border-zinc-200 bg-white text-zinc-600 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-300',
  error: 'border-red-300 bg-red-50 text-red-700 dark:border-red-800 dark:bg-red-900/20 dark:text-red-300',
}

const entryTypeLabel: Record<WorkflowEntry['type'], string> = {
  thinking: '思考',
  tool: '工具',
  status: '状态',
  decision: '决策',
  note: '备注',
}

const entryTypeIcon: Record<WorkflowEntry['type'], string> = {
  thinking: '💭',
  tool: '🔧',
  status: '•',
  decision: '📋',
  note: '•',
}

function resolveThinkingPresentation(entry: WorkflowEntry) {
  const payload = asRecord(entry.data)
  const kind = String(payload.thinking_kind || '').trim().toLowerCase()
  switch (kind) {
    case 'reasoning':
      return { label: '推理', icon: '🧠' }
    case 'analysis':
      return { label: '分析', icon: '💭' }
    default:
      return { label: entryTypeLabel.thinking, icon: entryTypeIcon.thinking }
  }
}

function asRecord(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return {}
  }
  return value as Record<string, unknown>
}

function isValidTimestamp(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value) && value > 0
}

function formatTimestampOrDash(value: unknown): string {
  return isValidTimestamp(value) ? formatTimestamp(value) : '-'
}

function normalizeEntryType(raw: unknown): WorkflowEntry['type'] {
  if (raw === 'thinking' || raw === 'tool' || raw === 'status' || raw === 'decision' || raw === 'note') {
    return raw
  }
  return 'status'
}

function isEntryRunning(entry: WorkflowEntry): boolean {
  if (entry.toolCard) {
    return workflowToolStatusForDisplay(entry.toolCard) === 'running'
  }
  if (entry.phase === 'tool_result') {
    return false
  }

  const payload = asRecord(entry.data)
  const status = String(payload.status || payload.state || payload.progress || '').toLowerCase()
  if (entry.phase === 'knowledge_review') return status === 'compiling'
  return status === 'running' || status === 'active' || status === 'in_progress' || status === 'processing'
}

export default function AgentBlockView({ block, collapsed, onFocus, onToggleCollapse }: Props) {
  const [expandedEntryIds, setExpandedEntryIds] = useState<Set<string>>(new Set())
  const safeEntries = useMemo(
    () => (Array.isArray(block.entries)
      ? block.entries.filter((item): item is WorkflowEntry => Boolean(item) && typeof item === 'object' && !Array.isArray(item))
      : []),
    [block.entries],
  )
  const displayName = String(block.agentDisplayName || block.agent || '智能体')
  const summary = String(block.summary || '处理中...')
  const blockStatus = normalizeWorkflowBlockStatus(block.status)
  const startedAtText = formatTimestampOrDash(block.startedAt)
  const updatedAtText = formatTimestampOrDash(block.updatedAt)

  useEffect(() => {
    const validIds = new Set(
      safeEntries.map((entry, index) => String(entry.id || `workflow-entry-${index}`)),
    )
    setExpandedEntryIds((prev) => {
      const next = new Set<string>()
      prev.forEach((id) => {
        if (validIds.has(id)) {
          next.add(id)
        }
      })
      if (next.size === prev.size) {
        let same = true
        next.forEach((id) => {
          if (!prev.has(id)) {
            same = false
          }
        })
        if (same) {
          return prev
        }
      }
      return next
    })
  }, [safeEntries])

  const toggleEntryExpand = (entryId: string) => {
    setExpandedEntryIds((prev) => {
      const next = new Set(prev)
      if (next.has(entryId)) {
        next.delete(entryId)
      } else {
        next.add(entryId)
      }
      return next
    })
  }

  return (
    <section
      className={cn(
        'rounded border bg-white p-3 dark:bg-zinc-900/90',
        blockStatus === 'error'
          ? 'border-red-500 dark:border-red-500'
          : blockStatus === 'running'
            ? 'border-blue-500 dark:border-blue-500'
            : 'border-zinc-200 dark:border-zinc-800',
      )}
    >
      <div className="mb-2 flex items-start justify-between gap-2">
        <button
          onClick={onFocus}
          className="min-w-0 flex-1 text-left"
        >
          <div className={cn('break-words text-[10px] font-medium', blockStatus === 'completed' ? 'text-zinc-600 dark:text-zinc-300' : 'text-zinc-900 dark:text-zinc-100')}>
            {displayName}
          </div>
          <div className="break-words text-[9px] text-zinc-500 dark:text-zinc-400">{summary}</div>
          <div className="mt-1 text-[8px] font-mono text-zinc-400 dark:text-zinc-500">
            启动 {startedAtText} · 更新 {updatedAtText}
          </div>
        </button>
        <div className="flex shrink-0 items-center gap-1">
          <span className={cn('rounded-sm border px-1.5 py-0.5 text-[8px]', statusClassName[blockStatus])}>
            {statusLabel[blockStatus]}
          </span>
          <button
            type="button"
            onClick={(event) => {
              event.stopPropagation()
              onToggleCollapse()
            }}
            className="rounded p-0.5 hover:bg-zinc-100 dark:hover:bg-zinc-800"
            aria-label={collapsed ? `展开 ${displayName}` : `折叠 ${displayName}`}
          >
            <CollapseIndicator expanded={!collapsed} className="text-zinc-400" />
          </button>
        </div>
      </div>

      {collapsed ? (
        <div className="rounded-sm bg-zinc-50 px-2.5 py-2 text-[10px] text-zinc-500 dark:bg-zinc-950/60 dark:text-zinc-400">
          已折叠 · {safeEntries.length} 条过程记录
        </div>
      ) : (
        <div className="space-y-2">
          {safeEntries.map((entry, index) => {
            const entryId = String(entry.id || `workflow-entry-${index}`)
            const entryType = normalizeEntryType(entry.type)
            const toolCard = entry.toolCard
            const toolStatus = workflowToolStatusForDisplay(toolCard)
            const toolName = String(toolCard?.tool || '未知工具')
            const content = String(entry.content || '').trim()
            if (!toolCard && !content) {
              return null
            }
            const running = isEntryRunning(entry)
            const reviewStatus = entry.phase === 'knowledge_review' ? String(entry.data?.status || '') : ''
            const expanded = running || expandedEntryIds.has(entryId)
            const thinkingPresentation = entryType === 'thinking' ? resolveThinkingPresentation(entry) : null
            const entryTypeText = toolCard
              ? `工具 · ${toolName}`
              : thinkingPresentation?.label || entryTypeLabel[entryType]
            const timestampText = formatTimestampOrDash(entry.timestamp)
            const stateLabel = toolCard
              ? toolStatus === 'running'
                ? '进行中'
                : toolStatus === 'failed'
                  ? '失败'
                  : toolStatus === 'finished' ? '已结束' : '已完成'
              : reviewStatus === 'awaiting_review' ? '等待确认'
                : reviewStatus === 'compile_failed' ? '失败'
                : running
                ? '进行中'
                : '已完成'
            const entryStateClassName = toolCard
              ? toolStatus === 'running'
                ? 'border-blue-300 bg-blue-50 text-blue-700 dark:border-blue-800 dark:bg-blue-900/20 dark:text-blue-300'
                : toolStatus === 'failed'
                  ? 'border-red-300 bg-red-50 text-red-700 dark:border-red-800 dark:bg-red-900/20 dark:text-red-300'
                  : toolStatus === 'finished' ? 'border-zinc-200 bg-white text-zinc-600 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-300'
                  : 'border-emerald-300 bg-emerald-50 text-emerald-700 dark:border-emerald-800 dark:bg-emerald-900/20 dark:text-emerald-300'
              : reviewStatus === 'awaiting_review' ? statusClassName.waiting_user
                : reviewStatus === 'compile_failed' ? statusClassName.error
                : running
                ? 'border-blue-300 bg-blue-50 text-blue-700 dark:border-blue-800 dark:bg-blue-900/20 dark:text-blue-300'
                : 'border-zinc-200 bg-white text-zinc-600 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-300'

            return (
              <div key={entryId} className="border-l border-zinc-200 pl-2.5 dark:border-zinc-800">
                <button
                  onClick={() => {
                    if (running) {
                      return
                    }
                    toggleEntryExpand(entryId)
                  }}
                  className="flex w-full items-start justify-between gap-2 text-left"
                >
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-1.5 break-words text-[10px] text-zinc-600 dark:text-zinc-300">
                      <span>{toolCard ? '🔧' : thinkingPresentation?.icon || entryTypeIcon[entryType]}</span>
                      <span className="font-medium text-zinc-900 dark:text-zinc-100">{entryTypeText}</span>
                      <span className="font-mono text-[8px] text-zinc-400 dark:text-zinc-500">{timestampText}</span>
                    </div>
                    {!expanded && (
                      <div className="mt-1 text-[9px] text-zinc-500 dark:text-zinc-400">
                        {toolCard ? '已自动收起，点击可展开查看详情。' : content}
                      </div>
                    )}
                  </div>
                  <div className="flex shrink-0 items-center gap-1">
                    <span className={cn('rounded-sm border px-1.5 py-0.5 text-[8px]', entryStateClassName)}>
                      {stateLabel}
                    </span>
                    {running ? (
                      <span className="w-2.5" />
                    ) : (
                      <CollapseIndicator expanded={expanded} size="sm" className="text-zinc-400" />
                    )}
                  </div>
                </button>

                {expanded ? (
                  toolCard ? (
                    <div className="mt-2">
                      <ToolCallCard toolCard={toolCard} />
                    </div>
                  ) : (
                    <MarkdownContent content={content} className="mt-2 text-[10px] text-zinc-700 dark:text-zinc-200" />
                  )
                ) : null}
              </div>
            )
          })}
        </div>
      )}
    </section>
  )
}
