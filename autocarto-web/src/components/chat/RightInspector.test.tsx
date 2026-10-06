import React from 'react'
import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import RightInspector from '@/components/chat/RightInspector'
import type { TodoItem, WorkflowBlock } from '@/types/message'

vi.mock('@/components/chat/TaskListPanel', () => ({
  default: ({ tasks }: { tasks: TodoItem[] }) => <div>tasks:{tasks.length}</div>,
}))

vi.mock('@/components/chat/AgentBlockView', () => ({
  default: ({ block }: { block: WorkflowBlock }) => <div>{block.agentDisplayName}</div>,
}))

describe('RightInspector', () => {
  it('将任务区和流程区渲染为独立滚动容器', () => {
    render(
      <RightInspector
        tasks={[{ content: 'task', priority: 'high', status: 'pending' }]}
        pendingInterrupts={[]}
        workflowBlocks={[
          {
            id: 'wf-1',
            agent: 'planner',
            agentDisplayName: 'Planner',
            summary: '处理中',
            status: 'running',
            startedAt: 1,
            updatedAt: 2,
            entries: [],
          },
        ]}
        focusedWorkflowBlockId={null}
        onFocusWorkflowBlock={vi.fn()}
      />,
    )

    expect(screen.getByTestId('inspector-task-region')).toBeInTheDocument()
    expect(screen.getByTestId('inspector-workflow-region')).toBeInTheDocument()
    expect(screen.getByText('tasks:1')).toBeInTheDocument()
    expect(screen.getByText('Planner')).toBeInTheDocument()
  })
})
