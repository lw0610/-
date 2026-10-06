'use client'

import { useMemo, useState } from 'react'
import { InterruptRequestData, Message, WorkflowBlock } from '@/types/message'
import CollapseIndicator from '@/components/common/CollapseIndicator'
import { cn, formatTimestamp } from '@/lib/utils'
import MarkdownContent from './MarkdownContent'
import InterruptCard from './InterruptCard'
import KnowledgeReviewCard from '@/features/knowledge-review/KnowledgeReviewCard'
import { parseReviewReference, reviewKey, type ReviewReference, type ReviewSummary } from '@/features/knowledge-review/model'

interface Props {
  message: Message
  workflowBlock?: WorkflowBlock
  workflowFocused?: boolean
  onOpenWorkflowBlock?: (blockId: string) => void
  interrupt?: InterruptRequestData
  interruptSubmitting?: boolean
  interruptResolvedState?: 'approved' | 'rejected'
  onInterruptApprove?: (checkpointId: string, payload?: unknown) => void
  onInterruptReject?: (checkpointId: string) => void
  sessionId?: string
  knowledgeReviewSummaries?: Record<string, ReviewSummary>
  onOpenKnowledgeReview?: (reference: ReviewReference) => void
  onConfirmKnowledgeReview?: (reference: ReviewReference) => void
}

const workflowStatusLabel: Record<WorkflowBlock['status'], string> = {
  running: '进行中',
  completed: '已完成',
  error: '异常',
}

const workflowStatusClassName: Record<WorkflowBlock['status'], string> = {
  running: 'border-blue-300 bg-blue-50 text-blue-700 dark:border-blue-800 dark:bg-blue-900/20 dark:text-blue-300',
  completed:
    'border-emerald-300 bg-emerald-50 text-emerald-700 dark:border-emerald-800 dark:bg-emerald-900/20 dark:text-emerald-300',
  error: 'border-red-300 bg-red-50 text-red-700 dark:border-red-800 dark:bg-red-900/20 dark:text-red-300',
}

function asRecord(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return {}
  }
  return value as Record<string, unknown>
}

function buildErrorSuggestion(data: unknown) {
  const payload = asRecord(data)
  const code = String(payload.code || '').trim().toUpperCase()
  if (code === 'PLAN_AGENT_UNAVAILABLE') {
    return 'Plan 智能体当前不可用。请先检查 Compute 服务和工具连接状态，再重试。'
  }
  if (code === 'PLAN_PROTOCOL_VIOLATION') {
    return 'Plan 模式当前不可用。请先检查激活模型是否支持 Plan 执行协议、是否已配置 API Key，以及运行态是否已完成重建。'
  }
  if (code === 'AGENT_UNAVAILABLE') {
    return '当前模式智能体未就绪。请检查智能体配置和工具可用性后重试。'
  }
  return ''
}

function workflowEntryPriority(entry: WorkflowBlock['entries'][number]) {
  switch (entry.type) {
    case 'thinking':
    case 'decision':
      return 0
    case 'tool':
      return 1
    default:
      return 2
  }
}

function isWorkflowEntryRunning(entry: WorkflowBlock['entries'][number]) {
  const status = String(entry.toolCard?.status || entry.data?.status || entry.data?.state || '').toLowerCase()
  return status === 'running' || status === 'active' || status === 'in_progress' || status === 'processing'
}

function summarizeWorkflowEntry(entry: WorkflowBlock['entries'][number]) {
  if (entry.toolCard?.tool) {
    return `工具 · ${entry.toolCard.tool}`
  }
  if (entry.type === 'thinking') {
    return '思考'
  }
  if (entry.type === 'decision') {
    return '决策'
  }
  if (entry.type === 'status') {
    return '状态'
  }
  return '过程'
}

function workflowEntryPreview(entry: WorkflowBlock['entries'][number]) {
  return String(entry.content || entry.toolCard?.result || '').trim()
}

export default function MessageBubble({
  message,
  workflowBlock,
  workflowFocused,
  onOpenWorkflowBlock,
  interrupt,
  interruptSubmitting,
  interruptResolvedState,
  onInterruptApprove,
  onInterruptReject,
  sessionId,
  knowledgeReviewSummaries,
  onOpenKnowledgeReview,
  onConfirmKnowledgeReview,
}: Props) {
  const isUser = message.type === 'user'
  const isError = message.type === 'error'
  const isInterrupt = message.type === 'interrupt'
  const isStatus = message.type === 'status'
  const isAgent = message.type === 'agent'
  const isPhaseTransition = message.type === 'phase_transition'
  const errorSuggestion = isError ? buildErrorSuggestion(message.data) : ''
  const reviewReference = parseReviewReference(interrupt, sessionId)
  const [expandedWorkflowEntries, setExpandedWorkflowEntries] = useState<Record<string, boolean>>({})
  const [workflowCollapsed, setWorkflowCollapsed] = useState(false)
  const inlineWorkflowEntries = useMemo(() => {
    if (!workflowBlock) {
      return []
    }
    const entries = Array.isArray(workflowBlock.entries)
      ? workflowBlock.entries.filter((entry) => entry && (entry.content || entry.toolCard))
      : []
    return entries
      .slice()
      .sort((left, right) => {
        const priorityDiff = workflowEntryPriority(left) - workflowEntryPriority(right)
        if (priorityDiff !== 0) {
          return priorityDiff
        }
        return (Number(right.timestamp) || 0) - (Number(left.timestamp) || 0)
      })
      .slice(0, 5)
      .sort((left, right) => (Number(left.timestamp) || 0) - (Number(right.timestamp) || 0))
  }, [workflowBlock])

  if (reviewReference) {
    return <KnowledgeReviewCard reference={reviewReference} summary={knowledgeReviewSummaries?.[reviewKey(reviewReference)]} onOpen={onOpenKnowledgeReview} onConfirm={onConfirmKnowledgeReview} />
  }

  if (isStatus) {
    return (
      <div className="mb-3 flex justify-center">
        <div className="max-w-[90%] rounded-sm border border-zinc-200 bg-zinc-50 px-3 py-1 text-[11px] text-zinc-500 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-300">
          {message.content}
        </div>
      </div>
    )
  }

  if (isAgent && workflowBlock) {
    return (
      <div className="mb-3 flex justify-center">
        <div
          className={cn(
            'w-full max-w-[90%] rounded border bg-zinc-50/80 px-3 py-2 dark:bg-zinc-900/40',
            workflowFocused
              ? 'border-blue-500 dark:border-blue-500'
              : 'border-zinc-200 dark:border-zinc-700',
          )}
        >
          <div className="flex items-start justify-between gap-2">
            <div className="min-w-0">
              <div className="truncate text-[11px] font-medium text-zinc-700 dark:text-zinc-200">
                {workflowBlock.agentDisplayName}
              </div>
              <div className="truncate text-xs text-zinc-600 dark:text-zinc-300">{workflowBlock.summary}</div>
            </div>
            <span
              className={cn(
                'shrink-0 rounded-sm border px-1.5 py-0.5 text-[10px]',
                workflowStatusClassName[workflowBlock.status],
              )}
            >
              {workflowStatusLabel[workflowBlock.status]}
            </span>
          </div>
          <div className="mt-1 flex items-center justify-between gap-2 text-[10px] text-zinc-500 dark:text-zinc-400">
            <span>{formatTimestamp(workflowBlock.updatedAt)}</span>
            <button
              className="rounded border border-zinc-200 px-1.5 py-0.5 text-[10px] hover:bg-zinc-100 dark:border-zinc-700 dark:hover:bg-zinc-800"
              onClick={() => onOpenWorkflowBlock?.(workflowBlock.id)}
              aria-label={`查看 ${workflowBlock.agentDisplayName} 详情`}
            >
              查看详情
            </button>
          </div>
          {inlineWorkflowEntries.length > 0 && (
            <div className="mt-2 border-t border-zinc-200 pt-2 dark:border-zinc-700">
              <button
                type="button"
                onClick={() => setWorkflowCollapsed((prev) => !prev)}
                className="mb-1 flex w-full items-center justify-between text-[10px] text-zinc-500 dark:text-zinc-400"
              >
                <span>过程详情 ({inlineWorkflowEntries.length})</span>
                <CollapseIndicator expanded={!workflowCollapsed} size="sm" className="text-zinc-400" />
              </button>
              {!workflowCollapsed && (
                <div className="space-y-1.5">
                  {inlineWorkflowEntries.map((entry) => {
                    const entryKey = entry.id || `${entry.type}-${entry.timestamp}`
                    const running = isWorkflowEntryRunning(entry)
                    const expanded = running || Boolean(expandedWorkflowEntries[entryKey])
                    const preview = workflowEntryPreview(entry)
                    return (
                      <button
                        key={entryKey}
                        type="button"
                        className="w-full rounded border border-zinc-200 bg-white/80 px-2 py-1.5 text-left text-[10px] dark:border-zinc-700 dark:bg-zinc-900/70"
                        onClick={() => {
                          if (running) {
                            return
                          }
                          setExpandedWorkflowEntries((prev) => ({
                            ...prev,
                            [entryKey]: !prev[entryKey],
                          }))
                        }}
                      >
                        <div className="flex items-start justify-between gap-2">
                          <div className="min-w-0 flex-1">
                            <div className="truncate font-medium text-zinc-700 dark:text-zinc-200">{summarizeWorkflowEntry(entry)}</div>
                            <div className="truncate text-zinc-500 dark:text-zinc-400">{preview}</div>
                          </div>
                          <CollapseIndicator expanded={expanded} size="sm" className="mt-0.5 shrink-0 text-zinc-400" />
                        </div>
                        {expanded && preview && (
                          <div className="mt-1 whitespace-pre-wrap break-words text-[10px] text-zinc-600 dark:text-zinc-300">
                            {preview}
                          </div>
                        )}
                      </button>
                    )
                  })}
                  {workflowBlock.entries.length > inlineWorkflowEntries.length && (
                    <div className="text-[10px] text-zinc-500 dark:text-zinc-400">
                      还有 {workflowBlock.entries.length - inlineWorkflowEntries.length} 条，查看详情
                    </div>
                  )}
                </div>
              )}
            </div>
          )}
        </div>
      </div>
    )
  }

  if (isInterrupt && interrupt) {
    const checkpointId = String(interrupt.checkpoint_id || '')
    return (
      <div className="mb-3 flex justify-start">
        <div className="w-full max-w-[92%]">
          <InterruptCard
            interrupt={interrupt}
            submitting={interruptSubmitting}
            resolvedState={interruptResolvedState}
            onApprove={(payload) => onInterruptApprove?.(checkpointId, payload)}
            onReject={() => onInterruptReject?.(checkpointId)}
          />
        </div>
      </div>
    )
  }

  if (isPhaseTransition) {
    return (
      <div className="mb-3 flex justify-center">
        <div className="w-full max-w-[92%] rounded border border-blue-200 bg-blue-50/90 px-3 py-2 dark:border-blue-800 dark:bg-blue-900/20">
          <div className="text-xs font-medium text-blue-700 dark:text-blue-300">计划完成，开始执行...</div>
          {message.content && (
            <div className="mt-1 whitespace-pre-wrap break-words text-[11px] text-blue-700/90 dark:text-blue-200/90">
              {message.content}
            </div>
          )}
        </div>
      </div>
    )
  }

  return (
    <div className={`group mb-2 flex animate-fade-in-up ${isUser ? 'justify-end' : 'justify-start'}`}>
      {isUser ? (
        <div className="max-w-[75%]">
          <div className="mb-0.5 text-right text-[9px] font-semibold uppercase tracking-[0.3px] text-zinc-400">YOU</div>
          <div className="rounded border border-zinc-200 bg-white px-2.5 py-2 text-xs text-zinc-800 leading-relaxed dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-200">
            <div className="whitespace-pre-wrap break-words">{message.content}</div>
          </div>
        </div>
      ) : (
        <div className="max-w-[85%]">
          <div className="mb-0.5 text-[9px] font-semibold uppercase tracking-[0.3px] text-blue-500">
            {message.agentDisplayName ? message.agentDisplayName.toUpperCase() : 'AUTOCARTO'}
          </div>
          <div className="border-l-2 border-blue-500 pl-2.5">
            <MarkdownContent
              content={message.content}
              className={isError ? 'text-xs text-red-700 leading-[1.6] dark:text-red-300' : 'text-xs text-zinc-700 leading-[1.6] dark:text-zinc-200'}
            />
            {isError && errorSuggestion && (
              <div className="mt-2 rounded border border-amber-300 bg-amber-50 px-2 py-1 text-xs text-amber-700 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-300">
                {errorSuggestion}
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  )
}
