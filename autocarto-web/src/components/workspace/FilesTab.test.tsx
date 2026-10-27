import React from 'react'
import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, expect, it, vi } from 'vitest'
import FilesTab from './FilesTab'
import { countNewFiles } from '@/hooks/useSessionFiles'
import type { SessionFile } from './files'
beforeEach(() => vi.restoreAllMocks())
const item: SessionFile = {
  id: 'one',
  path: '/tmp/output/roads.shp',
  name: 'roads.shp',
  kind: 'vector',
  group: 'data',
  members: ['roads.dbf'],
  size: 10,
  registered_at: '2026-10-25T10:00:00Z',
  modified_at: '2026-10-25T10:00:00Z',
  exists: true,
  origin: { type: 'tool', label: 'OSM 下载' },
}
it('支持搜索和分组筛选，同一数据集显示一行', () => {
  const onPreview = vi.fn()
  render(
    <FilesTab
      sessionId="sid"
      projectPath="/tmp/a.qgz"
      onPreview={onPreview}
      files={{
        items: [item],
        all: false,
        setAll: vi.fn(),
        loading: false,
        error: '',
        sessionDir: '/tmp',
        refresh: vi.fn(),
        newCount: 1,
        viewedAt: 0,
      }}
    />,
  )
  expect(screen.getByText('roads.shp')).toBeVisible()
  expect(screen.getByText(/2 个文件/)).toBeVisible()
  fireEvent.click(screen.getByText('roads.shp'))
  expect(onPreview).toHaveBeenCalledWith(item)
  fireEvent.change(screen.getByLabelText('文件类型筛选'), {
    target: { value: 'deliverable' },
  })
  expect(screen.queryByText('roads.shp')).not.toBeInTheDocument()
  fireEvent.change(screen.getByLabelText('文件类型筛选'), {
    target: { value: '' },
  })
  fireEvent.change(screen.getByLabelText('搜索会话文件'), {
    target: { value: '不存在' },
  })
  expect(screen.getByText('没有匹配的文件。')).toBeVisible()
})
it('新文件按会话查看时间计算，缩略图不计数', () => {
  expect(
    countNewFiles(
      [item, { ...item, id: 'preview', origin: { type: 'preview' } }],
      0,
    ),
  ).toBe(1)
  expect(countNewFiles([item], Date.parse('2026-10-25T11:00:00Z'))).toBe(0)
})

const fileState = (items: SessionFile[], all = false) => ({
  items, all, setAll: vi.fn(), loading: false, error: '', sessionDir: '/tmp',
  refresh: vi.fn(async () => {}), newCount: 0, viewedAt: 0,
})

it('成果和下载数据直接显示，可以分别筛选', () => {
  const source = { ...item, id: 'source', name: 'raw.geojson', group: 'source' }
  const result = { ...item, id: 'result', name: 'result.geojson', group: 'deliverable' }
  render(<FilesTab sessionId="sid" projectPath="/tmp/a.qgz" onPreview={vi.fn()} files={fileState([source, result])} />)
  expect(screen.getByText('result.geojson')).toBeVisible()
  expect(screen.getByText('raw.geojson')).toBeVisible()
  fireEvent.change(screen.getByLabelText('文件类型筛选'), { target: { value: 'source' } })
  expect(screen.getByText('raw.geojson')).toBeVisible()
  expect(screen.queryByText('result.geojson')).not.toBeInTheDocument()
})

it('文件列表不提供历史归档入口', () => {
  render(<FilesTab sessionId="sid" projectPath="/tmp/a.qgz" onPreview={vi.fn()} files={fileState([item], true)} />)
  expect(screen.queryByRole('button', { name: '移到历史' })).not.toBeInTheDocument()
  expect(screen.queryByRole('button', { name: '恢复到当前文件' })).not.toBeInTheDocument()
  expect(screen.queryByRole('option', { name: '历史文件' })).not.toBeInTheDocument()
})
