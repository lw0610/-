import React from 'react'
import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import MessageBubble from '@/components/chat/MessageBubble'
import type { Message, WorkflowBlock } from '@/types/message'

vi.mock('@/components/chat/MarkdownContent', () => ({
  default: ({ content }: { content: string }) => <div>{content}</div>,
}))

vi.mock('@/components/chat/InterruptCard', () => ({
  default: () => <div>interrupt-card</div>,
}))

const message: Message = {
  id: 'msg-agent',
  type: 'agent',
  content: '',
  timestamp: 1000,
  workflowBlockId: 'wf-1',
}

const workflowBlock: WorkflowBlock = {
  id: 'wf-1',
  agent: 'planner',
  agentDisplayName: 'Planner',
  summary: '规划步骤',
  status: 'completed',
  startedAt: 1000,
  updatedAt: 2000,
  entries: [
    {
      id: 'entry-1',
      type: 'thinking',
      content: '先分析当前目标。',
      timestamp: 1500,
    },
  ],
}

describe('MessageBubble', () => {
  it('agent 消息支持整体折叠过程详情区域', () => {
    render(
      <MessageBubble
        message={message}
        workflowBlock={workflowBlock}
        workflowFocused={false}
        onOpenWorkflowBlock={vi.fn()}
      />,
    )

    expect(screen.getByText('过程详情 (1)')).toBeInTheDocument()
    expect(screen.getByText('先分析当前目标。')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '过程详情 (1)' }))

    expect(screen.queryByText('先分析当前目标。')).not.toBeInTheDocument()
  })
})
