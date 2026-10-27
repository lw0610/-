'use client'
import { useMemo, useState } from 'react'
import type { useSessionFiles } from '@/hooks/useSessionFiles'
import { useUIStore } from '@/stores/uiStore'
import FileRow from './FileRow'
import { groupLabels, localFileAction, type SessionFile } from './files'
export default function FilesTab({
  files,
  sessionId,
  projectPath,
  onPreview,
}: {
  files: ReturnType<typeof useSessionFiles>
  sessionId: string
  projectPath: string
  onPreview: (item: SessionFile) => void
}) {
  const [query, setQuery] = useState(''),
    [group, setGroup] = useState(''),
    [sort, setSort] = useState('latest'),
    [feedback, setFeedback] = useState('')
  const highlighted = useUIStore((state) => state.highlightedFileIds)
  const filtered = useMemo(
    () =>
      files.items
        .filter(
          (item) =>
            (!group || item.group === group) &&
            (!query ||
              `${item.name} ${item.origin.label || ''} ${item.path}`
                .toLowerCase()
                .includes(query.toLowerCase())),
        )
        .sort((a, b) =>
          sort === 'name'
            ? a.name.localeCompare(b.name, 'zh-CN')
            : sort === 'type'
              ? a.kind.localeCompare(b.kind)
              : new Date(b.modified_at).getTime() -
                new Date(a.modified_at).getTime(),
        ),
    [files.items, group, query, sort],
  )
  const act = async (path: string, action: 'open' | 'reveal' | 'copy') => {
    try {
      setFeedback(await localFileAction(path, action))
    } catch (error) {
      setFeedback(error instanceof Error ? error.message : String(error))
    }
  }
  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="space-y-2 border-b border-zinc-200 p-3 dark:border-zinc-800">
        <div className="flex items-center justify-between">
          <h2 className="text-sm font-semibold">
            本会话文件{' '}
            <span className="text-xs font-normal text-zinc-500">
              {files.items.length}
            </span>
          </h2>
          <div className="flex gap-2 text-xs">
            <button
              disabled={files.loading}
              onClick={() => void files.refresh()}
            >
              {files.loading ? '刷新中…' : '刷新'}
            </button>
            <button
              disabled={!files.sessionDir}
              onClick={() => void act(files.sessionDir, 'open')}
            >
              打开文件夹
            </button>
          </div>
        </div>
        <input
          aria-label="搜索会话文件"
          placeholder="搜索名称、来源或路径"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          className="w-full rounded border border-zinc-300 bg-transparent px-2 py-1.5 text-xs dark:border-zinc-700"
        />
        <div className="flex items-center gap-2 text-xs">
          <select
            aria-label="文件类型筛选"
            value={group}
            onChange={(event) => setGroup(event.target.value)}
            className="min-w-0 rounded border bg-transparent p-1"
          >
            <option value="">全部类型</option>
            {Object.entries(groupLabels)
              .filter(([key]) => key !== 'other' || files.all)
              .map(([key, label]) => (
                <option key={key} value={key}>
                  {label}
                </option>
              ))}
          </select>
          <select
            aria-label="文件排序"
            value={sort}
            onChange={(event) => setSort(event.target.value)}
            className="rounded border bg-transparent p-1"
          >
            <option value="latest">最新</option>
            <option value="name">名称</option>
            <option value="type">类型</option>
          </select>
          <label className="ml-auto flex items-center gap-1 whitespace-nowrap">
            <input
              type="checkbox"
              checked={files.all}
              onChange={(event) => {
                files.setAll(event.target.checked)
                if (!event.target.checked && group === 'other') setGroup('')
              }}
            />
            显示全部
          </label>
        </div>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto space-y-3 p-3">
        {files.error && (
          <p role="alert" className="text-xs text-red-600">
            {files.error}
          </p>
        )}
        {feedback && (
          <p role="status" className="break-all text-xs text-blue-700">
            {feedback}
          </p>
        )}
        {!files.error && !files.loading && files.items.length === 0 && (
          <p className="py-8 text-center text-xs leading-6 text-zinc-500">
            本会话还没有产出文件。导出地图、下载数据或运行流程后会出现在这里。
          </p>
        )}
        {files.items.length > 0 && filtered.length === 0 && (
          <p className="text-xs text-zinc-500">没有匹配的文件。</p>
        )}
        {Object.entries(groupLabels).map(([key, label]) => {
          const items = filtered.filter((item) => item.group === key)
          return (
            items.length > 0 && (
              <details key={key} open={group === key || Boolean(query) || key !== 'intermediate'}>
                <summary className="mb-2 cursor-pointer text-xs font-medium">
                  {label} · {items.length}
                </summary>
                <div className="space-y-2">
                  {items.map((item) => (
                    <FileRow
                      key={item.id}
                      item={item}
                      sessionId={sessionId}
                      projectPath={projectPath}
                      sessionDir={files.sessionDir}
                      isNew={
                        new Date(item.registered_at).getTime() > files.viewedAt
                      }
                      highlighted={highlighted.includes(item.id)}
                      onPreview={() => onPreview(item)}
                      onAction={(action) => void act(item.path, action)}
                    />
                  ))}
                </div>
              </details>
            )
          )
        })}
      </div>
    </div>
  )
}
