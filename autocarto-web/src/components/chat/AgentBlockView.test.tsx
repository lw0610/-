import React from 'react'
import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import AgentBlockView from '@/components/chat/AgentBlockView'
import type { WorkflowBlock } from '@/types/message'

vi.mock('@/components/chat/ToolCallCard', () => ({
  default: ({ toolCard }: { toolCard: { tool: string; status: string } }) => (
    <div data-testid="tool-card">{toolCard.tool}:{toolCard.status}</div>
  ),
}))

vi.mock('@/components/chat/MarkdownContent', () => ({
  default: ({ content }: { content: string }) => <div>{content}</div>,
}))

function buildBlock(entryOverrides: Partial<WorkflowBlock['entries'][number]> = {}): WorkflowBlock {
  return {
    id: 'wf-test',
    agent: 'planner',
    agentDisplayName: 'Planner',
    summary: '分析流程',
    status: 'completed',
    startedAt: 100,
    updatedAt: 200,
    entries: [
      {
        id: 'entry-1',
        type: 'status',
        content: '同步任务列表',
        timestamp: 150,
        data: {
          status: 'failed',
          success: false,
        },
        ...entryOverrides,
      },
    ],
  }
}

describe('AgentBlockView', () => {
  it('纯状态类过程记录不会显示工具成功或失败徽标', () => {
    render(
      <AgentBlockView
        block={buildBlock()}
        focused={false}
        collapsed={false}
        onFocus={() => {}}
        onToggleCollapse={() => {}}
      />,
    )

    expect(screen.getByText('状态')).toBeInTheDocument()
    expect(screen.queryByText('失败')).not.toBeInTheDocument()
    expect(screen.queryByText('成功')).not.toBeInTheDocument()
    expect(screen.getByText('同步任务列表')).toBeInTheDocument()
  })

  it('过程记录会随运行态结束自动收起详情', () => {
    const { rerender } = render(
      <AgentBlockView
        block={buildBlock({
          data: { status: 'running' },
        })}
        focused={false}
        collapsed={false}
        onFocus={() => {}}
        onToggleCollapse={() => {}}
      />,
    )

    expect(screen.getByText('同步任务列表')).toBeInTheDocument()
    expect(document.querySelectorAll('svg.rotate-90')).toHaveLength(1)

    rerender(
      <AgentBlockView
        block={buildBlock({
          data: { status: 'completed' },
        })}
        focused={false}
        collapsed={false}
        onFocus={() => {}}
        onToggleCollapse={() => {}}
      />,
    )

    expect(screen.getByText('同步任务列表')).toBeInTheDocument()
    expect(document.querySelectorAll('svg.rotate-90')).toHaveLength(1)
    expect(document.querySelectorAll('svg:not(.rotate-90)')).toHaveLength(1)
  })

  it('点击右上角折叠按钮会触发独立折叠回调', () => {
    const onToggleCollapse = vi.fn()
    render(
      <AgentBlockView
        block={buildBlock()}
        focused={false}
        collapsed={false}
        onFocus={() => {}}
        onToggleCollapse={onToggleCollapse}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: '折叠 Planner' }))
    expect(onToggleCollapse).toHaveBeenCalledTimes(1)
  })
})
