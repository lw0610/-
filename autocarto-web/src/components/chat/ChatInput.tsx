'use client'

import { useState, useRef, useCallback, type ReactNode } from 'react'
import type { ExecutionMode } from '@/types/message'
import Chip from '@/components/common/Chip'
import { cn } from '@/lib/utils'

interface Props {
  onSend: (content: string) => void | boolean | Promise<void | boolean>
  flowControls?: ReactNode
  sendBlocked?: boolean
  onCancel?: () => void
  disabled?: boolean
  running?: boolean
  executionMode?: ExecutionMode
}

export default function ChatInput({
  onSend,
  onCancel,
  disabled,
  running,
  executionMode = 'build',
  flowControls,
  sendBlocked,
}: Props) {
  const [input, setInput] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const isBuildMode = executionMode === 'build'

  const placeholder = isBuildMode
    ? 'BUILD 模式 — 将直接执行操作 (Enter 发送, Shift+Enter 换行)'
    : 'PLAN 模式 — 仅分析不执行 (Enter 发送, Shift+Enter 换行)'

  const modeBadge = isBuildMode
    ? {
      label: '⚡ BUILD · 直接执行',
      tone: 'emerald' as const,
    }
    : {
      label: '🧭 PLAN · 只读分析',
      tone: 'amber' as const,
    }

  const handleSend = useCallback(async () => {
    const trimmed = input.trim()
    if (!trimmed || disabled || running || submitting || sendBlocked) return
    setSubmitting(true)
    try {
      const sent = await onSend(trimmed)
      if (sent === false) return
      setInput('')
      if (textareaRef.current) textareaRef.current.style.height = 'auto'
    } finally { setSubmitting(false) }
  }, [input, disabled, onSend, running, submitting, sendBlocked])

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      void handleSend()
    }
  }

  const handleInput = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
    setInput(e.target.value)
    const el = e.target
    el.style.height = 'auto'
    el.style.height = Math.min(el.scrollHeight, 200) + 'px'
  }

  return (
    <div
      className={cn(
        'border-t p-3 transition-colors duration-200',
        isBuildMode
          ? 'border-emerald-200 bg-gradient-to-b from-emerald-50 to-surface-overlay dark:border-emerald-900/40 dark:from-emerald-950/20'
          : 'border-chip-amber-border bg-gradient-to-b from-chip-amber-bg to-surface-overlay',
      )}
    >
      {flowControls}
      <div
        className={cn(
          'relative rounded border transition-all duration-150',
          isBuildMode
            ? 'border-emerald-300/70 bg-surface-raised focus-within:border-emerald-500 focus-within:ring-2 focus-within:ring-emerald-400/30 dark:border-emerald-900/60'
            : 'border-chip-amber-border bg-surface-raised focus-within:border-chip-amber-text focus-within:ring-2 focus-within:ring-chip-amber-border',
        )}
      >
        <textarea
          ref={textareaRef}
          value={input}
          onChange={handleInput}
          onKeyDown={handleKeyDown}
          placeholder={placeholder}
          disabled={disabled}
          rows={1}
          aria-label="消息输入"
          className="w-full resize-none border-none bg-transparent px-3 py-2.5 text-xs text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-0 disabled:opacity-50"
        />
        <div className="flex items-center justify-between px-2.5 pb-2">
          <div className="flex items-center gap-2">
            <Chip tone={modeBadge.tone}>{modeBadge.label}</Chip>
            <span className="text-[9px] text-text-faint">Enter 发送 · Shift+Enter 换行</span>
          </div>
          <button
            onClick={running ? onCancel : () => void handleSend()}
            disabled={running ? !onCancel : disabled || submitting || sendBlocked || !input.trim()}
            title={running ? '取消当前任务' : isBuildMode ? '发送并立即执行' : '发送分析请求'}
            className={cn(
              'flex h-7 w-7 items-center justify-center rounded text-white transition-colors disabled:cursor-not-allowed disabled:opacity-50',
              running
                ? 'bg-red-600 hover:bg-red-700'
                : isBuildMode
                  ? 'bg-emerald-600 shadow-sm shadow-emerald-500/25 hover:bg-emerald-700'
                  : 'bg-zinc-900 hover:bg-zinc-800 dark:bg-zinc-100 dark:text-zinc-900 dark:hover:bg-zinc-200',
            )}
          >
            <span className="sr-only">{running ? '取消任务' : '发送'}</span>
            {running ? (
              <svg viewBox="0 0 20 20" className="h-4 w-4" fill="currentColor" aria-hidden>
                <rect x="5" y="5" width="10" height="10" rx="1.5" />
              </svg>
            ) : (
              <svg viewBox="0 0 20 20" className="h-4 w-4" fill="currentColor" aria-hidden>
                <path d="M3.105 2.29a.75.75 0 0 0-.826.95l1.414 4.925A1.5 1.5 0 0 0 5.135 9.25H10.5a.75.75 0 0 1 0 1.5H5.135a1.5 1.5 0 0 0-1.442 1.086L2.28 16.76a.75.75 0 0 0 .826.95l15.11-7.196a.75.75 0 0 0 0-1.028L3.105 2.29Z" />
              </svg>
            )}
          </button>
        </div>
      </div>
    </div>
  )
}
