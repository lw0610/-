import React from 'react'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import InterruptCard from '@/components/chat/InterruptCard'
import type { InterruptRequestData } from '@/types/message'

function buildInterrupt(info: Record<string, unknown>): InterruptRequestData {
  return {
    checkpoint_id: 'cp-1',
    interrupt_contexts: [
      {
        id: 'root',
        is_root_cause: true,
        info,
      },
    ],
  }
}

describe('InterruptCard', () => {
  it('会根据 confirm 默认值预选确认结果并直接提交', async () => {
    const onApprove = vi.fn()

    render(
      <InterruptCard
        interrupt={buildInterrupt({
          type: 'plan_confirmation',
          title: '确认执行',
          select_mode: 'confirm',
          default: 'yes',
          required: true,
        })}
        onApprove={onApprove}
        onReject={vi.fn()}
      />,
    )

    expect(screen.getByLabelText('是')).toBeChecked()
    await userEvent.click(screen.getByRole('button', { name: '确认并继续' }))

    expect(onApprove).toHaveBeenCalledWith({ confirmed: true })
  })

  it('会根据决策默认值初始化当前问题并允许继续下一项', async () => {
    render(
      <InterruptCard
        interrupt={buildInterrupt({
          type: 'decision',
          title: '需要决策',
          decisions: [
            {
              id: 'decision_1',
              title: '第一题',
              select_mode: 'single',
              options: [
                { label: '方案 A', value: 'option-a' },
                { label: '方案 B', value: 'option-b' },
              ],
              default: 'option-a',
              required: true,
            },
            {
              id: 'decision_2',
              title: '第二题',
              select_mode: 'text',
            },
          ],
        })}
        onApprove={vi.fn()}
        onReject={vi.fn()}
      />,
    )

    expect(screen.getByLabelText('方案 A')).toBeChecked()
    expect(screen.getByText('已回答')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: '下一项' }))

    expect(screen.getByText('2. 第二题')).toBeInTheDocument()
  })

  it('会提交多决策答案并合并补充说明', async () => {
    const onApprove = vi.fn()

    render(
      <InterruptCard
        interrupt={buildInterrupt({
          type: 'decision',
          title: '需要决策',
          decisions: [
            {
              id: 'decision_1',
              title: '第一题',
              select_mode: 'single',
              options: [
                { label: '方案 A', value: 'option-a' },
                { label: '方案 B', value: 'option-b' },
              ],
              default: 'option-a',
              required: true,
            },
            {
              id: 'decision_2',
              title: '第二题',
              select_mode: 'text',
              required: true,
            },
          ],
        })}
        onApprove={onApprove}
        onReject={vi.fn()}
      />,
    )

    await userEvent.click(screen.getByRole('button', { name: '下一项' }))
    await userEvent.type(screen.getByPlaceholderText('请输入决策内容'), '最终说明')
    await userEvent.type(screen.getByPlaceholderText('可选：补充确认信息'), '补充上下文')
    await userEvent.click(screen.getByRole('button', { name: '确认并继续' }))

    expect(onApprove).toHaveBeenCalledWith({
      decisions: {
        decision_1: { selected: ['option-a'] },
        decision_2: { text: '最终说明' },
      },
      text: '补充上下文',
    })
  })
})
