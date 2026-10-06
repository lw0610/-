'use client'

import { useRef, useEffect, useMemo } from 'react'
import { ExecutionMode, InterruptRequestData, Message, WorkflowBlock } from '@/types/message'
import { cn } from '@/lib/utils'
import MessageBubble from './MessageBubble'
import type { ReviewReference, ReviewSummary } from '@/features/knowledge-review/model'

interface Props {
  messages: Message[]
  workflowBlocks: WorkflowBlock[]
  focusedWorkflowBlockId: string | null
  onOpenWorkflowBlock: (blockId: string) => void
  pendingInterrupts: InterruptRequestData[]
  interruptSubmitting: string
  interruptResolvedStates: Record<string, 'approved' | 'rejected'>
  onInterruptApprove: (checkpointId: string, payload?: unknown) => void
  onInterruptReject: (checkpointId: string) => void
  executionMode?: ExecutionMode
  sessionId?: string
  knowledgeReviewSummaries?: Record<string, ReviewSummary>
  onOpenKnowledgeReview?: (reference: ReviewReference) => void
  onConfirmKnowledgeReview?: (reference: ReviewReference) => void
}

function asRecord(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return {}
  }
  return value as Record<string, unknown>
}

function extractCheckpointId(message: Message): string {
  const data = asRecord(message.data)
  const snake = typeof data.checkpoint_id === 'string' ? data.checkpoint_id : ''
  const camel = typeof data.checkpointId === 'string' ? data.checkpointId : ''
  return (snake || camel).trim()
}

function buildWorkflowMessages(workflowBlocks: WorkflowBlock[], existingMessages: Message[]) {
  const linkedWorkflowIds = new Set(
    existingMessages
      .filter((msg) => msg.type === 'agent' && msg.workflowBlockId)
      .map((msg) => String(msg.workflowBlockId)),
  )

  return workflowBlocks
    .filter((block) => !linkedWorkflowIds.has(block.id))
    .map((block) => ({
      id: `wf-msg-${block.id}`,
      type: 'agent',
      content: block.summary,
      timestamp: block.startedAt,
      agent: block.agent,
      agentDisplayName: block.agentDisplayName,
      workflowBlockId: block.id,
      data: {
        status: block.status,
        updated_at: block.updatedAt,
      },
    } satisfies Message))
}

function buildPendingInterruptMessages(
  pendingInterrupts: InterruptRequestData[],
  existedCheckpointSet: Set<string>,
  fallbackTimestamp: number,
): Message[] {
  const seenCheckpoint = new Set<string>()

  return pendingInterrupts
    .map((interrupt) => {
      const checkpointId = String(interrupt.checkpoint_id || '').trim()
      if (!checkpointId || existedCheckpointSet.has(checkpointId) || seenCheckpoint.has(checkpointId)) {
        return null
      }
      seenCheckpoint.add(checkpointId)
      return { checkpointId, interrupt }
    })
    .filter((item): item is { checkpointId: string; interrupt: InterruptRequestData } => Boolean(item))
    .map((item, idx) => ({
      id: `pending-interrupt-${item.checkpointId}`,
      type: 'interrupt',
      content: '任务需要您的确认才能继续',
      timestamp: fallbackTimestamp + idx + 1,
      data: {
        checkpoint_id: item.checkpointId,
        interrupt_contexts: Array.isArray(item.interrupt.interrupt_contexts)
          ? item.interrupt.interrupt_contexts
          : [],
      },
    } satisfies Message))
}

export default function MessageList({
  messages,
  workflowBlocks,
  focusedWorkflowBlockId,
  onOpenWorkflowBlock,
  pendingInterrupts,
  interruptSubmitting,
  interruptResolvedStates,
  onInterruptApprove,
  onInterruptReject,
  executionMode = 'build',
  sessionId,
  knowledgeReviewSummaries,
  onOpenKnowledgeReview,
  onConfirmKnowledgeReview,
}: Props) {
  const bottomRef = useRef<HTMLDivElement>(null)
  const isBuildMode = executionMode === 'build'

  const modeTheme = useMemo(
    () =>
      isBuildMode
        ? {
          listClassName: 'bg-gradient-to-b from-emerald-50/70 via-surface-base to-surface-base dark:from-emerald-950/10',
          bannerClassName: 'border-emerald-300 bg-emerald-50 text-emerald-700 dark:border-emerald-900/60 dark:bg-emerald-900/20 dark:text-emerald-300',
          bannerLabel: '⚡ BUILD 模式 · 直接执行',
          emptyCardClassName: 'border-emerald-300/70 bg-surface-raised text-text-secondary dark:border-emerald-900/60',
          emptyHint: '当前对话会直接调用工具执行操作。',
        }
        : {
          listClassName: 'bg-gradient-to-b from-chip-amber-bg via-surface-base to-surface-base',
          bannerClassName: 'border-chip-amber-border bg-chip-amber-bg text-chip-amber-text',
          bannerLabel: '🧭 PLAN 模式 · 只读分析',
          emptyCardClassName: 'border-chip-amber-border bg-surface-raised text-text-secondary',
          emptyHint: '当前对话仅做方案分析，不会直接执行操作。',
        },
    [isBuildMode],
  )

  const workflowById = useMemo(
    () => new Map(workflowBlocks.map((block) => [block.id, block])),
    [workflowBlocks],
  )

  const interruptByCheckpoint = useMemo(
    () =>
      new Map(
        pendingInterrupts
          .map((interrupt) => [String(interrupt.checkpoint_id || ''), interrupt] as const)
          .filter((item) => Boolean(item[0])),
      ),
    [pendingInterrupts],
  )

  const visibleMessages = useMemo(() => {
    const baseMessages = messages.filter(
      (msg) => msg.type !== 'tool' && msg.type !== 'thinking',
    )
    const generatedWorkflowMessages = buildWorkflowMessages(workflowBlocks, baseMessages)
    const sorted = [...baseMessages, ...generatedWorkflowMessages].sort((a, b) => a.timestamp - b.timestamp)

    // 同一 checkpoint 的 interrupt 仅保留最新一条，避免重复卡片导致交互抖动。
    const latestInterruptIndex = new Map<string, number>()
    sorted.forEach((msg, idx) => {
      if (msg.type !== 'interrupt') {
        return
      }
      const checkpointId = extractCheckpointId(msg)
      if (checkpointId) {
        latestInterruptIndex.set(checkpointId, idx)
      }
    })

    const deduped = sorted.filter((msg, idx) => {
      if (msg.type !== 'interrupt') {
        return true
      }
      const checkpointId = extractCheckpointId(msg)
      if (!checkpointId) {
        return true
      }
      return latestInterruptIndex.get(checkpointId) === idx
    })

    const existedCheckpointSet = new Set(
      deduped
        .filter((msg) => msg.type === 'interrupt')
        .map((msg) => extractCheckpointId(msg))
        .filter(Boolean),
    )

    const fallbackTimestamp = deduped.reduce(
      (maxTimestamp, item) => Math.max(maxTimestamp, Number(item.timestamp) || 0),
      Date.now(),
    )

    const generatedInterruptMessages = buildPendingInterruptMessages(
      pendingInterrupts,
      existedCheckpointSet,
      fallbackTimestamp,
    )

    return [...deduped, ...generatedInterruptMessages].sort((a, b) => a.timestamp - b.timestamp)
  }, [messages, pendingInterrupts, workflowBlocks])

  const hasContent = visibleMessages.length > 0

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [visibleMessages])

  return (
    <div
      className={cn(
        'h-full overflow-y-auto p-3 scrollbar-thin transition-colors duration-200',
        modeTheme.listClassName,
      )}
      role="log"
      aria-live="polite"
      aria-label={isBuildMode ? '对话消息（BUILD 模式）' : '对话消息（PLAN 模式）'}
    >
      <div className="sticky top-0 z-10 -mx-3 mb-2 px-3 pb-1">
        <div
          className={cn(
            'inline-flex items-center rounded-sm border px-2 py-0.5 text-[10px] font-medium shadow-sm backdrop-blur',
            modeTheme.bannerClassName,
          )}
        >
          {modeTheme.bannerLabel}
        </div>
      </div>

      {!hasContent && (
        <div className="flex min-h-[220px] items-center justify-center">
          <div
            className={cn(
              'max-w-md rounded-sm border px-4 py-3 text-center text-[11px]',
              modeTheme.emptyCardClassName,
            )}
          >
            <p className="text-[10px] uppercase tracking-wide text-text-faint">{isBuildMode ? 'BUILD' : 'PLAN'}</p>
            <p className="mt-1 font-medium text-text-primary">开始对话吧</p>
            <p className="mt-1">{modeTheme.emptyHint}</p>
          </div>
        </div>
      )}

      {visibleMessages.map((msg) => {
        const checkpointId = msg.type === 'interrupt' ? extractCheckpointId(msg) : ''
        const fallbackData = asRecord(msg.data)
        const interruptData = checkpointId
          ? interruptByCheckpoint.get(checkpointId) || {
            checkpoint_id: checkpointId,
            interrupt_contexts: Array.isArray(fallbackData.interrupt_contexts)
              ? fallbackData.interrupt_contexts
              : [],
          }
          : undefined

        return (
          <MessageBubble
            key={msg.type === 'interrupt' && checkpointId ? `interrupt-${checkpointId}` : msg.id}
            message={msg}
            workflowBlock={msg.workflowBlockId ? workflowById.get(msg.workflowBlockId) : undefined}
            workflowFocused={msg.workflowBlockId === focusedWorkflowBlockId}
            onOpenWorkflowBlock={onOpenWorkflowBlock}
            interrupt={interruptData}
            interruptSubmitting={checkpointId ? interruptSubmitting === checkpointId : false}
            interruptResolvedState={checkpointId ? interruptResolvedStates[checkpointId] : undefined}
            onInterruptApprove={onInterruptApprove}
            onInterruptReject={onInterruptReject}
            sessionId={sessionId}
            knowledgeReviewSummaries={knowledgeReviewSummaries}
            onOpenKnowledgeReview={onOpenKnowledgeReview}
            onConfirmKnowledgeReview={onConfirmKnowledgeReview}
          />
        )
      })}
      <div ref={bottomRef} />
    </div>
  )
}
