import React from 'react'
import { render, screen } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'
import MessageList from '@/components/chat/MessageList'
import type { Message } from '@/types/message'

vi.mock('@/components/chat/MessageBubble', () => ({
  default: ({ message }: { message: Message }) => (
    <div data-testid="message-bubble">{message.content}</div>
  ),
}))

const baseProps = {
  workflowBlocks: [],
  focusedWorkflowBlockId: null,
  onOpenWorkflowBlock: vi.fn(),
  pendingInterrupts: [],
  interruptSubmitting: '',
  interruptResolvedStates: {},
  onInterruptApprove: vi.fn(),
  onInterruptReject: vi.fn(),
}

describe('MessageList', () => {
  it('无消息时显示空状态文字"开始对话吧"', () => {
    render(<MessageList messages={[]} {...baseProps} />)
    expect(screen.getByText('开始对话吧')).toBeInTheDocument()
  })

  it('滚动容器有 role="log" 属性', () => {
    render(<MessageList messages={[]} {...baseProps} />)
    expect(screen.getByRole('log')).toBeInTheDocument()
  })

  it('滚动容器有 aria-live="polite" 属性', () => {
    render(<MessageList messages={[]} {...baseProps} />)
    expect(screen.getByRole('log')).toHaveAttribute('aria-live', 'polite')
  })

  it('有消息时渲染消息气泡', () => {
    const messages: Message[] = [
      { id: '1', type: 'user', content: '你好', timestamp: 1000 },
      { id: '2', type: 'assistant', content: '你好！', timestamp: 2000 },
    ]
    render(<MessageList messages={messages} {...baseProps} />)
    expect(screen.getAllByTestId('message-bubble')).toHaveLength(2)
    expect(screen.getByText('你好')).toBeInTheDocument()
    expect(screen.getByText('你好！')).toBeInTheDocument()
  })

  it('interrupt 消息会渲染决策卡气泡', () => {
    const messages: Message[] = [
      {
        id: 'interrupt-1',
        type: 'interrupt',
        content: '任务需要您的确认才能继续',
        timestamp: 3000,
        data: { checkpoint_id: 'cp-1', interrupt_contexts: [] },
      },
    ]

    render(<MessageList messages={messages} {...baseProps} />)

    expect(screen.getAllByTestId('message-bubble')).toHaveLength(1)
    expect(screen.getByText('任务需要您的确认才能继续')).toBeInTheDocument()
  })

  it('pendingInterrupts 会补齐缺失的 interrupt 卡片消息', () => {
    render(
      <MessageList
        messages={[]}
        {...baseProps}
        pendingInterrupts={[{ checkpoint_id: 'cp-pending', interrupt_contexts: [] }]}
      />,
    )

    expect(screen.queryByText('开始对话吧')).not.toBeInTheDocument()
    expect(screen.getByText('任务需要您的确认才能继续')).toBeInTheDocument()
  })

  it('同一 checkpoint 的 interrupt 只保留最新一条', () => {
    const messages: Message[] = [
      {
        id: 'interrupt-1',
        type: 'interrupt',
        content: '旧的确认卡',
        timestamp: 1000,
        data: { checkpoint_id: 'cp-1', interrupt_contexts: [] },
      },
      {
        id: 'interrupt-2',
        type: 'interrupt',
        content: '新的确认卡',
        timestamp: 2000,
        data: { checkpoint_id: 'cp-1', interrupt_contexts: [] },
      },
    ]

    render(<MessageList messages={messages} {...baseProps} />)

    expect(screen.queryByText('旧的确认卡')).not.toBeInTheDocument()
    expect(screen.getByText('新的确认卡')).toBeInTheDocument()
    expect(screen.getAllByTestId('message-bubble')).toHaveLength(1)
  })

  it('已有同 checkpoint interrupt 时，不再追加 pendingInterrupt 卡片', () => {
    const messages: Message[] = [
      {
        id: 'interrupt-1',
        type: 'interrupt',
        content: '任务需要您的确认才能继续',
        timestamp: 1000,
        data: { checkpoint_id: 'cp-1', interrupt_contexts: [] },
      },
    ]

    render(
      <MessageList
        messages={messages}
        {...baseProps}
        pendingInterrupts={[{ checkpoint_id: 'cp-1', interrupt_contexts: [] }]}
      />,
    )

    expect(screen.getAllByTestId('message-bubble')).toHaveLength(1)
  })
})
