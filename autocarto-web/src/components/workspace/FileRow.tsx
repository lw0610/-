'use client'
/* eslint-disable @next/next/no-img-element -- 本地文件缩略图通过受限内容接口直接展示。 */
import {
  backendURL,
  contentPath,
  fileSize,
  kindLabels,
  type SessionFile,
} from './files'
export default function FileRow({
  item,
  sessionId,
  projectPath,
  sessionDir,
  isNew,
  highlighted,
  onPreview,
  onAction,
}: {
  item: SessionFile
  sessionId: string
  projectPath: string
  sessionDir: string
  isNew: boolean
  highlighted: boolean
  onPreview: () => void
  onAction: (action: 'open' | 'reveal' | 'copy') => void
}) {
  const location = item.path.startsWith(sessionDir + '/')
    ? item.path.slice(sessionDir.length + 1)
    : item.path
  return (
    <div
      className={`group rounded border p-2 text-xs ${highlighted ? 'border-blue-400 bg-blue-50 dark:bg-blue-950/40' : 'border-zinc-200 bg-white dark:border-zinc-800 dark:bg-zinc-900'}`}
    >
      <button
        onClick={onPreview}
        className="flex w-full items-start gap-2 text-left"
        title={item.path}
      >
        {item.kind === 'image' && item.group === 'deliverable' ? (
          <img
            alt=""
            loading="lazy"
            className="h-12 w-14 shrink-0 rounded object-contain bg-zinc-100"
            src={backendURL(contentPath(sessionId, projectPath, item))}
          />
        ) : (
          <span className="shrink-0 rounded bg-zinc-100 px-1 py-1 text-[10px] dark:bg-zinc-800">
            {kindLabels[item.kind] || '文件'}
          </span>
        )}
        <span className="min-w-0 flex-1">
          <strong className="flex items-center gap-1 break-all font-medium">
            {isNew && (
              <span
                title="新文件"
                className="h-1.5 w-1.5 shrink-0 rounded-full bg-blue-500"
              />
            )}
            {item.name}
          </strong>
          <span className="mt-1 block truncate text-[10px] text-zinc-500">
            {item.origin.label || item.origin.tool || '会话文件'}
            {item.step_id ? ` · ${item.step_id}` : ''}
          </span>
          <span className="block text-[10px] text-zinc-500">
            {fileSize(item.size)} ·{' '}
            {new Date(item.modified_at).toLocaleString('zh-CN')}
            {item.members?.length > 0
              ? ` · ${item.members.length + 1} 个文件`
              : ''}
          </span>
          <span className="block truncate text-[10px] text-zinc-400">
            {location}
          </span>
        </span>
      </button>
      <div className="mt-1 flex justify-end gap-2 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
        <button onClick={() => onAction('open')}>打开</button>
        <button onClick={() => onAction('reveal')}>显示位置</button>
        <button onClick={() => onAction('copy')}>复制路径</button>
      </div>
    </div>
  )
}
