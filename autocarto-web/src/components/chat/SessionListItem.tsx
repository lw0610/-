'use client'

import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { ArchiveIcon, TrashIcon } from '@/components/common/AppIcon'
import { SessionRecord } from '@/lib/api'

interface Props {
  session: SessionRecord
  active: boolean
  running?: boolean
  compact?: boolean
  disabled?: boolean
  busy?: boolean
  onSelect: (sessionId: string) => void
  onToggleArchived: (session: SessionRecord) => void
  onDelete: (session: SessionRecord) => void
  onRename: (session: SessionRecord, nextName: string) => void
  onClearHistory: (session: SessionRecord) => void
  onCreateSnapshot: (session: SessionRecord) => void
  onExport: (session: SessionRecord) => void
}

interface PopoverPosition {
  top: number
  left: number
}

function formatRelativeTime(value?: string) {
  if (!value) {
    return '-'
  }
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) {
    return value
  }

  const diff = Date.now() - parsed.getTime()
  if (diff < 60 * 1000) {
    return '刚刚'
  }
  if (diff < 60 * 60 * 1000) {
    return `${Math.floor(diff / (60 * 1000))} 分钟前`
  }
  if (diff < 24 * 60 * 60 * 1000) {
    return `${Math.floor(diff / (60 * 60 * 1000))} 小时前`
  }
  return parsed.toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function getTitleSourceMeta(value: unknown) {
  const normalized = String(value || '').trim().toLowerCase()
  if (normalized === 'llm') {
    return {
      label: 'LLM',
      title: '标题由大模型生成',
      className: 'border-blue-200 bg-blue-50 text-blue-700 dark:border-blue-900/60 dark:bg-blue-950/30 dark:text-blue-300',
    }
  }
  if (normalized === 'fallback') {
    return {
      label: '规则',
      title: '标题由规则降级生成',
      className: 'border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-300',
    }
  }
  if (normalized === 'manual') {
    return {
      label: '手动',
      title: '标题已由用户手动修改',
      className: 'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900/60 dark:bg-emerald-950/30 dark:text-emerald-300',
    }
  }
  if (normalized === 'default') {
    return {
      label: '默认',
      title: '标题仍为默认会话名称',
      className: 'border-zinc-200 bg-zinc-50 text-zinc-500 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-400',
    }
  }
  return null
}

function getPopoverPosition(triggerEl: HTMLElement | null, width: number, height: number): PopoverPosition {
  if (!triggerEl || typeof window === 'undefined') {
    return { top: 0, left: 0 }
  }

  const rect = triggerEl.getBoundingClientRect()
  const margin = 8
  const viewportWidth = window.innerWidth
  const viewportHeight = window.innerHeight

  let left = rect.right - width
  if (left < margin) {
    left = margin
  }
  if (left + width > viewportWidth - margin) {
    left = Math.max(margin, viewportWidth - margin - width)
  }

  let top = rect.bottom + 6
  if (top + height > viewportHeight - margin) {
    const aboveTop = rect.top - 6 - height
    if (aboveTop >= margin) {
      top = aboveTop
    } else {
      top = Math.max(margin, viewportHeight - margin - height)
    }
  }

  return { top, left }
}

export default function SessionListItem({
  session,
  active,
  running = false,
  compact = false,
  disabled,
  busy,
  onSelect,
  onToggleArchived,
  onDelete,
  onRename,
  onClearHistory,
  onCreateSnapshot,
  onExport,
}: Props) {
  const containerRef = useRef<HTMLDivElement | null>(null)
  const menuTriggerRef = useRef<HTMLButtonElement | null>(null)
  const deleteTriggerRef = useRef<HTMLButtonElement | null>(null)
  const menuPopoverRef = useRef<HTMLDivElement | null>(null)
  const deletePopoverRef = useRef<HTMLDivElement | null>(null)
  const renamePopoverRef = useRef<HTMLDivElement | null>(null)

  const [renameOpen, setRenameOpen] = useState(false)
  const [renameValue, setRenameValue] = useState(session.name || '')
  const [menuOpen, setMenuOpen] = useState(false)
  const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false)
  const [menuPosition, setMenuPosition] = useState<PopoverPosition>({ top: 0, left: 0 })
  const [deletePosition, setDeletePosition] = useState<PopoverPosition>({ top: 0, left: 0 })
  const [renamePosition, setRenamePosition] = useState<PopoverPosition>({ top: 0, left: 0 })

  useEffect(() => {
    setRenameValue(session.name || '')
  }, [session.name])

  useEffect(() => {
    const handleClick = (event: MouseEvent) => {
      const target = event.target as Node | null
      if (!target) {
        return
      }
      if (
        containerRef.current?.contains(target)
        || menuPopoverRef.current?.contains(target)
        || deletePopoverRef.current?.contains(target)
        || renamePopoverRef.current?.contains(target)
      ) {
        return
      }

      if (menuOpen || deleteConfirmOpen || renameOpen) {
        setMenuOpen(false)
        setDeleteConfirmOpen(false)
        setRenameOpen(false)
      }
    }
    window.addEventListener('mousedown', handleClick)
    return () => window.removeEventListener('mousedown', handleClick)
  }, [deleteConfirmOpen, menuOpen, renameOpen])

  useEffect(() => {
    if (!menuOpen) {
      return
    }

    const update = () => {
      setMenuPosition(getPopoverPosition(menuTriggerRef.current, 160, 150))
    }

    update()
    window.addEventListener('resize', update)
    window.addEventListener('scroll', update, true)
    return () => {
      window.removeEventListener('resize', update)
      window.removeEventListener('scroll', update, true)
    }
  }, [menuOpen])

  useEffect(() => {
    if (!deleteConfirmOpen) {
      return
    }

    const update = () => {
      setDeletePosition(getPopoverPosition(deleteTriggerRef.current, 224, 92))
    }

    update()
    window.addEventListener('resize', update)
    window.addEventListener('scroll', update, true)
    return () => {
      window.removeEventListener('resize', update)
      window.removeEventListener('scroll', update, true)
    }
  }, [deleteConfirmOpen])

  useEffect(() => {
    if (!renameOpen) {
      return
    }

    const update = () => {
      setRenamePosition(getPopoverPosition(menuTriggerRef.current, 256, 116))
    }

    update()
    window.addEventListener('resize', update)
    window.addEventListener('scroll', update, true)
    return () => {
      window.removeEventListener('resize', update)
      window.removeEventListener('scroll', update, true)
    }
  }, [renameOpen])

  const preview = String(session.last_message_preview || '').trim() || '暂无消息'
  const bodyButtonClass = compact ? 'w-full rounded-sm px-2.5 py-2 pr-24 text-left focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500/50' : 'w-full rounded-sm px-3 py-2.5 pr-28 text-left focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500/50'
  const titleClass = 'truncate text-[11px] font-medium leading-4 text-zinc-800 dark:text-zinc-100'
  const previewClass = 'mt-1 truncate text-[9px] leading-4 text-zinc-400 dark:text-zinc-500'
  const timeClass = 'mt-1 text-[9px] text-zinc-400 dark:text-zinc-500'
  const titleSourceMeta = getTitleSourceMeta(session.title_source)
  const toolbarClass = compact
    ? 'absolute right-1.5 top-1.5 flex items-center gap-0.5 rounded border border-zinc-200/80 bg-white/95 px-1 py-0.5 dark:border-zinc-700 dark:bg-zinc-900/95'
    : 'absolute right-2 top-2 flex items-center gap-1 rounded border border-zinc-200/80 bg-white/95 px-1 py-0.5 dark:border-zinc-700 dark:bg-zinc-900/95'
  const actionButtonClass = compact
    ? 'inline-flex h-5 w-5 items-center justify-center rounded-sm border border-transparent text-zinc-500 transition-colors hover:border-zinc-200 hover:bg-zinc-50 dark:text-zinc-300 dark:hover:border-zinc-700 dark:hover:bg-zinc-800'
    : 'inline-flex h-6 min-w-6 items-center justify-center rounded-sm border border-transparent text-zinc-500 transition-colors hover:border-zinc-200 hover:bg-zinc-50 dark:text-zinc-300 dark:hover:border-zinc-700 dark:hover:bg-zinc-800'

  const submitRename = () => {
    const next = renameValue.trim()
    if (!next || next === session.name) {
      setRenameOpen(false)
      return
    }
    onRename(session, next)
    setRenameOpen(false)
  }

  const canUseDOM = typeof document !== 'undefined'

  const menuPopover = canUseDOM && menuOpen
    ? createPortal(
      <div
        ref={menuPopoverRef}
        className="fixed z-50 w-44 rounded border border-zinc-200/80 bg-white/95 p-1.5 shadow-[0_2px_8px_rgba(0,0,0,0.08)] shadow-zinc-900/10 dark:border-zinc-800 dark:bg-zinc-900/95 dark:shadow-black/30"
        style={{ top: menuPosition.top, left: menuPosition.left }}
        role="menu"
      >
        <button
          className="block w-full rounded px-2.5 py-2 text-left text-xs text-zinc-700 transition-colors hover:bg-zinc-100 dark:text-zinc-200 dark:hover:bg-zinc-800"
          role="menuitem"
          onClick={() => {
            setRenameOpen(true)
            setMenuOpen(false)
            setDeleteConfirmOpen(false)
          }}
          disabled={disabled}
        >
          重命名
        </button>
        <button
          className="block w-full rounded px-2.5 py-2 text-left text-xs text-zinc-700 transition-colors hover:bg-zinc-100 dark:text-zinc-200 dark:hover:bg-zinc-800"
          role="menuitem"
          onClick={() => {
            onClearHistory(session)
            setMenuOpen(false)
          }}
          disabled={disabled}
        >
          清空历史
        </button>
        <button
          className="block w-full rounded px-2.5 py-2 text-left text-xs text-zinc-700 transition-colors hover:bg-zinc-100 dark:text-zinc-200 dark:hover:bg-zinc-800"
          role="menuitem"
          onClick={() => {
            onCreateSnapshot(session)
            setMenuOpen(false)
          }}
          disabled={disabled}
        >
          创建快照
        </button>
        <button
          className="block w-full rounded px-2.5 py-2 text-left text-xs text-zinc-700 transition-colors hover:bg-zinc-100 dark:text-zinc-200 dark:hover:bg-zinc-800"
          role="menuitem"
          onClick={() => {
            onExport(session)
            setMenuOpen(false)
          }}
          disabled={disabled}
        >
          导出
        </button>
      </div>,
      document.body,
    )
    : null

  const deletePopover = canUseDOM && deleteConfirmOpen
    ? createPortal(
      <div
        ref={deletePopoverRef}
        className="fixed z-50 w-56 rounded border border-red-200/80 bg-white/95 p-2.5 text-xs shadow-[0_2px_8px_rgba(0,0,0,0.08)] shadow-zinc-900/10 dark:border-red-900/60 dark:bg-zinc-900/95 dark:shadow-black/30"
        style={{ top: deletePosition.top, left: deletePosition.left }}
      >
        <div className="mb-2 text-red-600 dark:text-red-400">确认删除该会话？</div>
        <p className="mb-2 max-w-64 text-xs text-zinc-500">会永久删除聊天记录和会话内下载、生成的文件。已另存到工程或其他目录的文件不会删除。此操作不可撤销。</p>
        <div className="flex items-center justify-end gap-1">
          <button
            className="rounded border border-zinc-200 px-2.5 py-1 text-xs text-zinc-600 transition-colors hover:bg-zinc-100 dark:border-zinc-700 dark:text-zinc-200 dark:hover:bg-zinc-800"
            onClick={() => {
              setDeleteConfirmOpen(false)
            }}
          >
            取消
          </button>
          <button
            className="rounded border border-red-200 px-2.5 py-1 text-xs text-red-700 transition-colors hover:bg-red-50 dark:border-red-900/60 dark:text-red-300 dark:hover:bg-red-950/30"
            onClick={() => {
              onDelete(session)
              setDeleteConfirmOpen(false)
            }}
            disabled={disabled}
          >
            删除
          </button>
        </div>
      </div>,
      document.body,
    )
    : null

  const renamePopover = canUseDOM && renameOpen
    ? createPortal(
      <div
        ref={renamePopoverRef}
        className="fixed z-50 w-64 rounded border border-zinc-200/80 bg-white/95 p-2.5 shadow-[0_2px_8px_rgba(0,0,0,0.08)] shadow-zinc-900/10 dark:border-zinc-800 dark:bg-zinc-900/95 dark:shadow-black/30"
        style={{ top: renamePosition.top, left: renamePosition.left }}
      >
        <input
          className="mb-2 w-full rounded border border-zinc-200 bg-zinc-50/70 px-2.5 py-2 text-xs text-zinc-700 outline-none transition-colors focus:border-blue-300 focus:bg-white dark:border-zinc-700 dark:bg-zinc-950 dark:text-zinc-200 dark:focus:border-blue-700"
          aria-label="会话重命名输入"
          value={renameValue}
          onChange={(e) => setRenameValue(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault()
              submitRename()
            }
            if (e.key === 'Escape') {
              e.preventDefault()
              setRenameOpen(false)
            }
          }}
          autoFocus
        />
        <div className="flex items-center justify-end gap-1">
          <button
            className="rounded border border-zinc-200 px-2.5 py-1 text-xs text-zinc-600 transition-colors hover:bg-zinc-100 dark:border-zinc-700 dark:text-zinc-200 dark:hover:bg-zinc-800"
            onClick={() => {
              setRenameOpen(false)
            }}
          >
            取消
          </button>
          <button
            className="rounded border border-blue-200 px-2.5 py-1 text-xs text-blue-700 transition-colors hover:bg-blue-50 dark:border-blue-900/60 dark:text-blue-300 dark:hover:bg-blue-950/30"
            onClick={() => {
              submitRename()
            }}
            disabled={disabled}
          >
            保存
          </button>
        </div>
      </div>,
      document.body,
    )
    : null

  return (
    <>
      <div
        ref={containerRef}
        className={`group relative rounded-sm border transition-[border-color,background-color,box-shadow] ${
          active
            ? 'border-l-2 border-blue-500 bg-zinc-100 text-blue-700 dark:border-blue-500 dark:bg-zinc-800 dark:text-blue-300'
            : 'border-zinc-200/70 bg-white/70 hover:border-zinc-300 hover:bg-white dark:border-zinc-800 dark:bg-zinc-900/40 dark:hover:border-zinc-700 dark:hover:bg-zinc-900/80'
        }`}
      >
        <button
          className={bodyButtonClass}
          onClick={() => onSelect(session.session_id)}
          disabled={disabled}
          aria-label={`切换会话 ${session.name || session.session_id}`}
        >
          <div className="flex items-center gap-1">
            <span className={titleClass}>{session.name || session.session_id}</span>
            {titleSourceMeta && (
              <span
                className={`inline-flex rounded-sm border px-1.5 py-0.5 text-[9px] font-medium leading-none ${titleSourceMeta.className}`}
                title={titleSourceMeta.title}
              >
                {titleSourceMeta.label}
              </span>
            )}
            {running && <span className="inline-block h-1.5 w-1.5 animate-pulse rounded-full bg-blue-500" title="后台运行中" />}
            {session.archived && <ArchiveIcon className="h-3 w-3" />}
          </div>
          <div className={previewClass}>{preview}</div>
          <div className={timeClass}>{formatRelativeTime(session.updated_at)}</div>
        </button>

        <div
          className={`${toolbarClass} ${
            active ? 'opacity-100' : 'pointer-events-none opacity-0 group-hover:pointer-events-auto group-hover:opacity-100 group-focus-within:pointer-events-auto group-focus-within:opacity-100'
          }`}
        >
          {busy && (
            <span
              className="inline-block h-2.5 w-2.5 animate-spin rounded-full border border-zinc-400 border-t-transparent dark:border-zinc-500"
              aria-label="会话操作进行中"
            />
          )}
          <button
            className={actionButtonClass}
            title={session.archived ? '取消归档' : '归档'}
            aria-label={session.archived ? '取消归档会话' : '归档会话'}
            onClick={(e) => {
              e.stopPropagation()
              onToggleArchived(session)
            }}
            disabled={disabled}
          >
            <ArchiveIcon className="h-3 w-3" />
          </button>
          <button
            ref={deleteTriggerRef}
            className={compact
              ? 'inline-flex h-5 w-5 items-center justify-center rounded-sm border border-transparent text-red-600 transition-colors hover:border-red-200 hover:bg-red-50 dark:text-red-300 dark:hover:border-red-900/60 dark:hover:bg-red-950/30'
              : 'inline-flex h-6 min-w-6 items-center justify-center rounded-sm border border-transparent text-red-600 transition-colors hover:border-red-200 hover:bg-red-50 dark:text-red-300 dark:hover:border-red-900/60 dark:hover:bg-red-950/30'}
            title="删除会话"
            aria-label="删除会话"
            onClick={(e) => {
              e.stopPropagation()
              setDeleteConfirmOpen((prev) => !prev)
              setMenuOpen(false)
              setRenameOpen(false)
            }}
            disabled={disabled}
          >
            <TrashIcon className="h-3 w-3" />
          </button>
          <button
            ref={menuTriggerRef}
            className={actionButtonClass}
            title="更多"
            aria-label="更多会话操作"
            onClick={(e) => {
              e.stopPropagation()
              setMenuOpen((prev) => !prev)
              setDeleteConfirmOpen(false)
            }}
            disabled={disabled}
          >
            ...
          </button>
        </div>
      </div>

      {menuPopover}
      {deletePopover}
      {renamePopover}
    </>
  )
}
