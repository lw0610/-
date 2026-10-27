import React from 'react'
import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, expect, it, vi } from 'vitest'
import ToolCallCard from '@/components/chat/ToolCallCard'
import { useChatStore } from '@/stores/chatStore'
import { useSessionFilesStore } from '@/hooks/useSessionFiles'
import { useUIStore } from '@/stores/uiStore'
vi.mock('@/components/chat/MarkdownContent', () => ({ default: () => null }))
beforeEach(() => {
  useChatStore.setState({ sessionId: 'sid' })
  useSessionFilesStore
    .getState()
    .put('sid', [
      {
        id: 'file-1',
        path: '/tmp/map.pdf',
        name: 'map.pdf',
        kind: 'pdf',
        group: 'deliverable',
        members: [],
        size: 10,
        registered_at: '2026-10-25',
        modified_at: '2026-10-25',
        exists: true,
        origin: { type: 'tool', tool: 'export_map', call_id: 'call-1' },
      },
    ])
  useUIStore.setState({
    workspace: { open: false, tab: 'process', width: 360, previewWidth: 520 },
  })
})
it('使用真实工具调用编号定位已登记文件', () => {
  render(
    <ToolCallCard
      toolCard={{
        id: 'random-display-id',
        callId: 'call-1',
        tool: 'export_map',
        status: 'completed',
        startedAt: 1,
        result: '完成',
      }}
    />,
  )
  fireEvent.click(screen.getByRole('button', { name: '在文件区查看（1）' }))
  expect(useUIStore.getState().workspace).toMatchObject({
    open: true,
    tab: 'files',
  })
  expect(useUIStore.getState().highlightedFileIds).toEqual(['file-1'])
})
it('其他工具调用不显示文件入口', () => {
  render(
    <ToolCallCard
      toolCard={{
        id: 'random-display-id',
        callId: 'different',
        tool: 'export_map',
        status: 'completed',
        startedAt: 1,
        result: '完成',
      }}
    />,
  )
  expect(screen.queryByText(/在文件区查看/)).not.toBeInTheDocument()
})
