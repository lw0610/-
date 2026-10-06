import React from 'react'
import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import TaskListPanel from '@/components/chat/TaskListPanel'

describe('TaskListPanel', () => {
  it('会把 success / processing / cancelled 归一化到完成态、进行中和阻塞态', () => {
    render(
      <TaskListPanel
        tasks={[
          { content: '成功任务', status: 'success' },
          { content: '处理中任务', status: 'processing' },
          { content: '已取消任务', status: 'cancelled' },
        ]}
      />,
    )

    expect(screen.getByText((_, element) => element?.textContent === '1/3 已完成，当前保持紧凑追踪。')).toBeInTheDocument()
    expect(screen.getByText('完成 1')).toBeInTheDocument()
    expect(screen.getByText('进行中 1')).toBeInTheDocument()
    expect(screen.getByText('阻塞 1')).toBeInTheDocument()
  })

  it('会忽略状态值前后的空白字符', () => {
    render(
      <TaskListPanel
        tasks={[
          { content: '成功任务', status: ' success ' },
          { content: '处理中任务', status: ' processing ' },
          { content: '失败任务', status: ' blocked ' },
        ]}
      />,
    )

    expect(screen.getByText((_, element) => element?.textContent === '1/3 已完成，当前保持紧凑追踪。')).toBeInTheDocument()
    expect(screen.getByText('完成 1')).toBeInTheDocument()
    expect(screen.getByText('进行中 1')).toBeInTheDocument()
    expect(screen.getByText('阻塞 1')).toBeInTheDocument()
  })
})
