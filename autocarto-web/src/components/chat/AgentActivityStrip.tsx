'use client'

import { WorkflowBlock } from '@/types/message'
import { formatTimestamp } from '@/lib/utils'
import { cn } from '@/lib/utils'

interface Props {
  blocks: WorkflowBlock[]
  activeBlockId: string | null
  focusedBlockId: string | null
  onOpenBlock: (blockId: string) => void
}

const statusLabel: Record<WorkflowBlock['status'], string> = {
  running: '进行中',
  completed: '已完成',
  error: '异常',
}

const statusClassName: Record<WorkflowBlock['status'], string> = {
  running: 'bg-blue-50 text-blue-700 border-blue-200 dark:bg-blue-900/20 dark:text-blue-300 dark:border-blue-800',
  completed: 'bg-emerald-50 text-emerald-700 border-emerald-200 dark:bg-emerald-900/20 dark:text-emerald-300 dark:border-emerald-800',
  error: 'bg-red-50 text-red-700 border-red-200 dark:bg-red-900/20 dark:text-red-300 dark:border-red-800',
}

export default function AgentActivityStrip({
  blocks,
  activeBlockId,
  focusedBlockId,
  onOpenBlock,
}: Props) {
  if (blocks.length === 0) {
    return null
  }

  const previewBlocks = [...blocks].slice(-3).reverse()

  return (
    <div className="border-b border-zinc-200 bg-zinc-50/70 px-3 py-2 dark:border-zinc-800 dark:bg-zinc-900/40">
      <div className="mb-2 flex items-center justify-between">
        <div className="text-xs font-medium text-zinc-600 dark:text-zinc-300">智能体过程入口</div>
        {activeBlockId && (
          <div className="text-[11px] text-zinc-500 dark:text-zinc-400">
            当前活跃：{blocks.find((item) => item.id === activeBlockId)?.agentDisplayName || '-'}
          </div>
        )}
      </div>

      <div className="space-y-2">
        {previewBlocks.map((block) => (
          <div
            key={block.id}
            className={cn(
              'flex items-center gap-3 rounded border bg-white px-3 py-2 dark:bg-zinc-900',
              focusedBlockId === block.id
                ? 'border-blue-300 dark:border-blue-700'
                : 'border-zinc-200 dark:border-zinc-700',
            )}
          >
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2">
                <div className="truncate text-xs font-medium text-zinc-700 dark:text-zinc-100">
                  {block.agentDisplayName}
                </div>
                <span
                  className={cn(
                    'inline-flex items-center rounded-sm border px-1.5 py-0.5 text-[10px]',
                    statusClassName[block.status],
                  )}
                >
                  {statusLabel[block.status]}
                </span>
              </div>
              <div className="truncate text-[11px] text-zinc-500 dark:text-zinc-400">{block.summary}</div>
              <div className="mt-0.5 text-[10px] text-zinc-400">{formatTimestamp(block.updatedAt)}</div>
            </div>

            <button
              onClick={() => onOpenBlock(block.id)}
              className="rounded-sm border border-zinc-200 px-2 py-1 text-[11px] text-zinc-600 transition-colors hover:bg-zinc-100 dark:border-zinc-700 dark:text-zinc-300 dark:hover:bg-zinc-800"
            >
              查看详情
            </button>
          </div>
        ))}
      </div>
    </div>
  )
}
