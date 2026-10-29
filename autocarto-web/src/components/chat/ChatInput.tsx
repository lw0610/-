'use client'

import { useState, useRef, useCallback, useEffect } from 'react'
import type { ChatInputMetadata, ExecutionMode } from '@/types/message'
import Chip from '@/components/common/Chip'
import { cn } from '@/lib/utils'
import { useMessageInputs } from './useMessageInputs'
import api from '@/lib/api'

interface Props {
  onSend: (content: string, metadata?: ChatInputMetadata) => void | boolean | Promise<void | boolean>
  sessionId?: string
  instanceId?: string
  awaitingAnswer?: boolean
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
  sessionId,
  instanceId,
  awaitingAnswer = false,
}: Props) {
  const [input, setInput] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const isBuildMode = executionMode === 'build'
  const inputs = useMessageInputs(sessionId, instanceId)
  const canReply = !running || awaitingAnswer

  const placeholder = '描述你要做的地图，可以添加文件、用 @ 引用图层、用 / 选择流程'
  useEffect(() => {
    const prefill = (event: Event) => {
      const text = String((event as CustomEvent<{ text?: string }>).detail?.text || '')
      if (text) { setInput(text); textareaRef.current?.focus() }
    }
    window.addEventListener('autocarto:compose-message', prefill)
    return () => window.removeEventListener('autocarto:compose-message', prefill)
  }, [])

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
    if (!trimmed || disabled || !canReply || submitting || inputs.loading) return
    setSubmitting(true)
    try {
      const metadata = inputs.metadata()
      if (metadata.attachments?.length) await api.post('/api/chat/attachments/check', { paths: metadata.attachments.map(item => item.path) })
      if (!inputs.isCurrent()) return
      const sent = await (Object.keys(metadata).length ? onSend(trimmed, metadata) : onSend(trimmed))
      if (sent === false || !inputs.isCurrent()) return
      setInput('')
      inputs.clear()
      if (textareaRef.current) textareaRef.current.style.height = 'auto'
    } catch (reason) { inputs.setError(reason instanceof Error ? reason.message : '发送失败') }
    finally { setSubmitting(false) }
  }, [input, disabled, onSend, canReply, submitting, inputs])

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault()
      void handleSend()
    }
  }

  const handleInput = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
    setInput(e.target.value)
    const trigger = e.target.value.match(/(?:^|\s)([@/])([^\s]*)$/)
    if (trigger && !inputs.menu) void inputs.openMenu(trigger[1] === '@' ? 'layers' : 'guides')
    const el = e.target
    el.style.height = 'auto'
    el.style.height = Math.min(el.scrollHeight, 200) + 'px'
  }

  return (
    <div
      onDragOver={event => event.preventDefault()}
      onDrop={event => {
        event.preventDefault()
        const paths = Array.from(event.dataTransfer.files).map(file => (file as File & { path?: string }).path || '').filter(Boolean)
        if (paths.length) void inputs.acceptPaths(paths)
        else inputs.setError('浏览器无法读取本机附件路径，请使用桌面端的添加按钮。')
      }}
      className={cn(
        'border-t p-3 transition-colors duration-200',
        isBuildMode
          ? 'border-emerald-200 bg-gradient-to-b from-emerald-50 to-surface-overlay dark:border-emerald-900/40 dark:from-emerald-950/20'
          : 'border-chip-amber-border bg-gradient-to-b from-chip-amber-bg to-surface-overlay',
      )}
    >
      <div className="mb-2 flex flex-wrap gap-1.5" aria-label="消息引用">
        {inputs.attachments.map(item => <Chip key={item.path}>{item.kind === 'folder' ? '文件夹' : '文件'}：{item.path.split(/[/\\]/).at(-1)} <button type="button" aria-label={`移除附件 ${item.path}`} onClick={() => inputs.removeAttachment(item.path)}>×</button></Chip>)}
        {inputs.layers.map(item => <Chip key={item.id}>@{item.name} <button type="button" aria-label={`移除图层 ${item.name}`} onClick={() => inputs.removeLayer(item.id)}>×</button></Chip>)}
        {inputs.guide && <Chip>/{inputs.guide.name} <button type="button" aria-label="移除流程" onClick={inputs.removeGuide}>×</button></Chip>}
      </div>
      {inputs.menu && <div className="mb-2 max-h-48 overflow-auto rounded border bg-surface-raised p-2" role="listbox" aria-label={inputs.menu === 'guides' ? '流程' : '工程图层'}>
        {(inputs.menu === 'guides' ? inputs.guides : inputs.availableLayers).filter(item => {
          const word = input.match(/(?:^|\s)[@/]([^\s]*)$/)?.[1] || ''
          return !word || item.name.toLowerCase().includes(word.toLowerCase())
        }).map(item => <button key={item.id} role="option" aria-selected={inputs.menu === 'guides' ? inputs.guide?.id === item.id : inputs.layers.some(layer => layer.id === item.id)} type="button" className="block w-full rounded px-2 py-1 text-left text-xs hover:bg-blue-50" onClick={() => {
          if (inputs.menu === 'guides') inputs.selectGuide(item as typeof inputs.guides[number]); else inputs.selectLayer(item)
          setInput(current => current.replace(/(?:^|\s)[@/][^\s]*$/, ' ').trimStart())
          textareaRef.current?.focus()
        }}>{item.name}{'description' in item && <span className="ml-2 text-text-muted">{String(item.description || '')}</span>}</button>)}
        <button type="button" className="mt-1 text-xs text-text-muted" onClick={() => inputs.setMenu('')}>关闭选项</button>
      </div>}
      {inputs.loading && <p role="status" className="mb-1 text-xs">正在读取…</p>}
      {inputs.error && <p role="alert" className="mb-1 text-xs text-red-700">{inputs.error}</p>}
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
          <div className="flex flex-wrap items-center gap-2">
            <button type="button" disabled={disabled || inputs.loading} className="text-xs" onClick={() => void inputs.browse(false)}>添加文件</button>
            <button type="button" disabled={disabled || inputs.loading} className="text-xs" onClick={() => void inputs.browse(true)}>添加文件夹</button>
            <button type="button" disabled={disabled} className="text-xs" onClick={() => void inputs.openMenu('layers')}>@图层</button>
            <button type="button" disabled={disabled} className="text-xs" onClick={() => void inputs.openMenu('guides')}>流程</button>
            <Chip tone={modeBadge.tone}>{modeBadge.label}</Chip>
            <span className="text-[9px] text-text-faint">Enter 发送 · Shift+Enter 换行</span>
          </div>
          <button
            onClick={!canReply ? onCancel : () => void handleSend()}
            disabled={!canReply ? !onCancel : disabled || submitting || inputs.loading || !input.trim()}
            title={!canReply ? '取消当前任务' : isBuildMode ? '发送并立即执行' : '发送分析请求'}
            className={cn(
              'flex h-7 w-7 items-center justify-center rounded text-white transition-colors disabled:cursor-not-allowed disabled:opacity-50',
              !canReply
                ? 'bg-red-600 hover:bg-red-700'
                : isBuildMode
                  ? 'bg-emerald-600 shadow-sm shadow-emerald-500/25 hover:bg-emerald-700'
                  : 'bg-zinc-900 hover:bg-zinc-800 dark:bg-zinc-100 dark:text-zinc-900 dark:hover:bg-zinc-200',
            )}
          >
            <span className="sr-only">{!canReply ? '取消任务' : '发送'}</span>
            {!canReply ? (
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
