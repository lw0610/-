'use client'
import { useEffect, useMemo, useState } from 'react'
import type { WorkflowBlock } from '@/types/message'
import AgentBlockView from '@/components/chat/AgentBlockView'
import Chip from '@/components/common/Chip'
import StateBlock from '@/components/common/StateBlock'
function isWorkflowBlockActive(block: WorkflowBlock): boolean {
  // A stopped attempt can retain its last running tool event when no result
  // arrived. The attempt's terminal state must still stop the activity badge.
  return block.status === 'running'
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

export default function ProcessTab({
  workflowBlocks,
  focusedWorkflowBlockId,
  onFocusWorkflowBlock,
}: {
  workflowBlocks: WorkflowBlock[]
  focusedWorkflowBlockId: string | null
  onFocusWorkflowBlock: (id: string | null) => void
}) {
  const [expandAllBlocks, setExpandAllBlocks] = useState(false)
  const [manualCollapseOverrides, setManualCollapseOverrides] = useState<
    Record<string, boolean>
  >({})
  const safeBlocks = useMemo(
    () =>
      Array.isArray(workflowBlocks)
        ? workflowBlocks.filter(
            (item): item is WorkflowBlock =>
              Boolean(item) && typeof item === 'object' && !Array.isArray(item),
          )
        : [],
    [workflowBlocks],
  )
  const sortedWorkflowBlocks = useMemo(
    () =>
      safeBlocks.slice().sort((a, b) => {
        const aStartedAt =
          typeof a.startedAt === 'number' && Number.isFinite(a.startedAt)
            ? a.startedAt
            : 0
        const bStartedAt =
          typeof b.startedAt === 'number' && Number.isFinite(b.startedAt)
            ? b.startedAt
            : 0
        return aStartedAt - bStartedAt
      }),
    [safeBlocks],
  )

  const activeBlockIdSet = useMemo(
    () =>
      new Set(
        sortedWorkflowBlocks
          .filter((block) => isWorkflowBlockActive(block))
          .map((block, index) => String(block.id || `workflow-block-${index}`)),
      ),
    [sortedWorkflowBlocks],
  )

  const activeBlockCount = activeBlockIdSet.size

  useEffect(() => {
    setManualCollapseOverrides({})
  }, [expandAllBlocks])

  return (
    <div
      data-testid="workspace-process-region"
      className="h-full overflow-y-auto p-3 scrollbar-thin"
    >
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
            description={
              '主聊天区出现\u201c查看详情\u201d入口后，可在这里查看完整过程。'
            }
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
                  : !(
                      activeBlockIdSet.has(blockId) ||
                      focusedWorkflowBlockId === blockId
                    )
                const collapsed =
                  typeof manualCollapseOverrides[blockId] === 'boolean'
                    ? manualCollapseOverrides[blockId]
                    : defaultCollapsed
                return (
                  <AgentBlockView
                    key={blockId}
                    block={block}
                    focused={focusedWorkflowBlockId === blockId}
                    collapsed={collapsed}
                    onFocus={() =>
                      onFocusWorkflowBlock(
                        focusedWorkflowBlockId === blockId ? null : blockId,
                      )
                    }
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
  )
}
