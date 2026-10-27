'use client'

import { useMemo, useState } from 'react'
import MarkdownContent from './MarkdownContent'
import { normalizeWorkflowToolStatus } from '@/lib/chat-message-utils'
import { WorkflowToolCard } from '@/types/message'
import { formatTimestamp } from '@/lib/utils'
import { cn } from '@/lib/utils'
import { useSessionFilesStore } from '@/hooks/useSessionFiles'
import { useChatStore } from '@/stores/chatStore'
import { useUIStore } from '@/stores/uiStore'

interface Props {
  toolCard: WorkflowToolCard
}

const statusLabel: Record<WorkflowToolCard['status'], string> = {
  running: '调用中',
  completed: '已完成',
  failed: '失败',
}

const statusClassName: Record<WorkflowToolCard['status'], string> = {
  running: 'border-blue-300 bg-blue-50 text-blue-700 dark:border-blue-800 dark:bg-blue-900/20 dark:text-blue-300',
  completed:
    'border-emerald-300 bg-emerald-50 text-emerald-700 dark:border-emerald-800 dark:bg-emerald-900/20 dark:text-emerald-300',
  failed: 'border-red-300 bg-red-50 text-red-700 dark:border-red-800 dark:bg-red-900/20 dark:text-red-300',
}

interface ParsedResultView {
  isStructured: boolean
  effectiveSuccess?: boolean
  statusSource?: string
  errorText?: string
  infoText?: string
  messageText?: string
  outputText?: string
  extraPayloadText?: string
  rawText: string
}

function isValidTimestamp(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value) && value > 0
}

function toDisplayText(value: unknown): string {
  if (value === null || value === undefined) {
    return ''
  }
  if (typeof value === 'string') {
    return value
  }
  if (typeof value === 'number' || typeof value === 'boolean') {
    return String(value)
  }
  try { return JSON.stringify(value, null, 2) } catch (error) { void error; return String(value) }
}

function asRecord(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return {}
  }
  return value as Record<string, unknown>
}

function parseResultForView(toolCard: WorkflowToolCard): ParsedResultView {
  const rawText = toDisplayText(toolCard.result).trim()
  if (!rawText) {
    return {
      isStructured: false,
      rawText: '',
    }
  }

  try { const parsed = JSON.parse(rawText)
  const payload = asRecord(parsed)
  if (!Object.keys(payload).length) {
    return {
      isStructured: false,
      rawText,
    }
  }
  
  const payloadSuccess = typeof payload.success === 'boolean' ? payload.success : undefined
  const effectiveSuccess = typeof payloadSuccess === 'boolean'
    ? payloadSuccess
    : typeof toolCard.success === 'boolean'
      ? toolCard.success
      : undefined
  
  const statusSource = toDisplayText(payload.status_source || toolCard.statusSource)
  const errorText = toDisplayText(payload.error || payload.err || payload.reason)
  const infoText = toDisplayText(payload.info)
  const messageText = toDisplayText(payload.message)
  const outputText = toDisplayText(payload.output || payload.result)
  
  const extraPayload = { ...payload }
  delete extraPayload.success
  delete extraPayload.status_source
  delete extraPayload.error
  delete extraPayload.err
  delete extraPayload.reason
  delete extraPayload.info
  delete extraPayload.message
  delete extraPayload.output
  delete extraPayload.result
  const extraPayloadText = Object.keys(extraPayload).length ? toDisplayText(extraPayload) : ''
  
  return {
    isStructured: true,
    effectiveSuccess,
    statusSource,
    errorText,
    infoText,
    messageText,
    outputText,
    extraPayloadText,
    rawText,
  } } catch (error) { void error; return {
    isStructured: false,
    rawText,
  } }
}

export default function ToolCallCard({ toolCard }: Props) {
  const sessionId = useChatStore(state => state.sessionId) || ''
  const files = useSessionFilesStore(state => state.items[sessionId])
  const registeredFiles = (files || []).filter(item => Boolean(toolCard.callId) && item.origin.call_id === toolCard.callId || item.origin.tool === toolCard.tool && Boolean(toolCard.result?.includes(item.path)))

  const [argsOpen, setArgsOpen] = useState(false)
  const [resultOpen, setResultOpen] = useState(true)
  const [extraOpen, setExtraOpen] = useState(false)
  const toolStatus = normalizeWorkflowToolStatus(toolCard.status)
  const toolName = String(toolCard.tool || '未知工具')
  const safeArgs = toolCard.args && typeof toolCard.args === 'object' ? toolCard.args : {}
  const startedAtText = isValidTimestamp(toolCard.startedAt) ? formatTimestamp(toolCard.startedAt) : '-'

  const elapsedText = useMemo(() => {
    if (!isValidTimestamp(toolCard.startedAt)) {
      return '-'
    }
    if (!isValidTimestamp(toolCard.finishedAt)) {
      if (toolStatus === 'running') {
        return '执行中'
      }
      return '-'
    }
    const elapsed = Math.max(0, toolCard.finishedAt - toolCard.startedAt)
    if (elapsed < 1000) {
      return `${elapsed}ms`
    }
    return `${(elapsed / 1000).toFixed(2)}s`
  }, [toolCard.finishedAt, toolCard.startedAt, toolStatus])

  const parsedResult = useMemo(
    () => parseResultForView(toolCard),
    [toolCard],
  )

  const hasResult = parsedResult.rawText.length > 0

  const resultBadge = useMemo(() => {
    if (toolStatus === 'running') {
      return {
        label: '执行中',
        className: 'border-blue-300 bg-blue-50 text-blue-700 dark:border-blue-800 dark:bg-blue-900/20 dark:text-blue-300',
      }
    }

    const isFailed = toolStatus === 'failed' || parsedResult.effectiveSuccess === false
    if (isFailed) {
      return {
        label: '失败',
        className: 'border-red-300 bg-red-50 text-red-700 dark:border-red-800 dark:bg-red-900/20 dark:text-red-300',
      }
    }

    const isSuccess = toolStatus === 'completed' || parsedResult.effectiveSuccess === true
    if (isSuccess) {
      return {
        label: '成功',
        className: 'border-emerald-300 bg-emerald-50 text-emerald-700 dark:border-emerald-800 dark:bg-emerald-900/20 dark:text-emerald-300',
      }
    }

    return {
      label: '未知',
      className: 'border-zinc-300 bg-zinc-50 text-zinc-700 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-300',
    }
  }, [parsedResult.effectiveSuccess, toolStatus])

  return (
    <div className="rounded border border-zinc-200 bg-white p-3 text-[11px] dark:border-zinc-800 dark:bg-zinc-900/90">
      <div className="mb-2 flex items-center justify-between gap-2">
        <div className="min-w-0">
          <div className="truncate font-mono text-[10px] font-medium text-zinc-900 dark:text-zinc-100">工具：{toolName}</div>
          <div className="text-[9px] text-zinc-500 dark:text-zinc-400">
            开始 {startedAtText} · 耗时 {elapsedText}
          </div>
        </div>
        <span className={cn('rounded-sm border px-1.5 py-0.5 text-[9px]', statusClassName[toolStatus])}>
          {statusLabel[toolStatus]}
        </span>
      </div>

      {registeredFiles.length > 0 && <button className="mb-2 text-xs text-blue-600" onClick={() => useUIStore.getState().openWorkspace('files', registeredFiles.map(item => item.id))}>在文件区查看（{registeredFiles.length}）</button>}
      <div className="space-y-2">
        <div className="rounded-sm border border-zinc-200 bg-zinc-100 p-2 dark:border-zinc-800 dark:bg-zinc-950/60">
          <div className="mb-1 flex items-center justify-between">
            <div className="text-[10px] font-medium text-zinc-600 dark:text-zinc-300">输入参数</div>
            <button
              onClick={() => setArgsOpen((prev) => !prev)}
              className="rounded-sm border border-transparent px-2 py-0.5 text-[9px] text-zinc-500 transition-colors hover:border-zinc-200 hover:bg-white dark:text-zinc-300 dark:hover:border-zinc-700 dark:hover:bg-zinc-900"
            >
              {argsOpen ? '收起' : '展开'}
            </button>
          </div>
          {argsOpen && (
            <pre className="overflow-x-auto whitespace-pre-wrap break-words rounded-sm bg-white p-2 font-mono text-[10px] text-zinc-700 dark:bg-zinc-900 dark:text-zinc-200">
              {JSON.stringify(safeArgs, null, 2)}
            </pre>
          )}
        </div>

        <div className="rounded-sm border border-zinc-200 bg-zinc-100 p-2 dark:border-zinc-800 dark:bg-zinc-950/60">
          <div className="mb-1 flex items-center justify-between">
            <div className="text-[10px] font-medium text-zinc-600 dark:text-zinc-300">执行结果</div>
            <button
              onClick={() => setResultOpen((prev) => !prev)}
              className="rounded-sm border border-transparent px-2 py-0.5 text-[9px] text-zinc-500 transition-colors hover:border-zinc-200 hover:bg-white dark:text-zinc-300 dark:hover:border-zinc-700 dark:hover:bg-zinc-900"
            >
              {resultOpen ? '收起' : '展开'}
            </button>
          </div>
          {resultOpen && (
            <div className="space-y-2 rounded-sm bg-white p-2 dark:bg-zinc-900">
              {!hasResult && (
                <div className="text-[10px] text-zinc-500 dark:text-zinc-400">暂无结果</div>
              )}

              {hasResult && (
                <>
                  <div className="flex flex-wrap items-center gap-2 text-[9px]">
                    <span className={cn('rounded-sm border px-1.5 py-0.5', resultBadge.className)}>
                      {resultBadge.label}
                    </span>
                    {parsedResult.statusSource && (
                      <span className="text-zinc-500 dark:text-zinc-400">状态来源：{parsedResult.statusSource}</span>
                    )}
                  </div>

                  {parsedResult.isStructured ? (
                    <>
                      {parsedResult.errorText && (
                        <div className="rounded-sm border border-red-200 bg-red-50/80 px-2 py-1.5 text-[10px] text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300">
                          {parsedResult.errorText}
                        </div>
                      )}

                      {parsedResult.messageText && (
                        <div className="rounded-sm border border-zinc-200 bg-zinc-50 px-2 py-1.5 text-[10px] text-zinc-700 dark:border-zinc-700 dark:bg-zinc-900/70 dark:text-zinc-200">
                          {parsedResult.messageText}
                        </div>
                      )}

                      {parsedResult.infoText && (
                        <div className="rounded-sm border border-zinc-200 bg-zinc-50 px-2 py-1.5 text-[10px] text-zinc-600 dark:border-zinc-700 dark:bg-zinc-900/70 dark:text-zinc-300">
                          {parsedResult.infoText}
                        </div>
                      )}

                      {parsedResult.outputText && (
                        <pre className="overflow-x-auto whitespace-pre-wrap break-words rounded-sm bg-zinc-100 p-2 font-mono text-[10px] text-zinc-700 dark:bg-zinc-950 dark:text-zinc-200">
                          {parsedResult.outputText}
                        </pre>
                      )}

                      {parsedResult.extraPayloadText && (
                        <div className="rounded-sm border border-zinc-200 bg-zinc-50 p-2 dark:border-zinc-700 dark:bg-zinc-950">
                          <div className="mb-1 flex items-center justify-between">
                            <div className="text-[10px] font-medium text-zinc-600 dark:text-zinc-300">附加字段</div>
                            <button
                              onClick={() => setExtraOpen((prev) => !prev)}
                              className="rounded-sm border border-transparent px-2 py-0.5 text-[9px] text-zinc-500 transition-colors hover:border-zinc-200 hover:bg-white dark:text-zinc-300 dark:hover:border-zinc-700 dark:hover:bg-zinc-900"
                            >
                              {extraOpen ? '收起' : '展开'}
                            </button>
                          </div>
                          {extraOpen && (
                            <pre className="overflow-x-auto whitespace-pre-wrap break-words rounded-sm bg-white p-2 font-mono text-[10px] text-zinc-700 dark:bg-zinc-900 dark:text-zinc-200">
                              {parsedResult.extraPayloadText}
                            </pre>
                          )}
                        </div>
                      )}
                    </>
                  ) : (
                    <MarkdownContent content={parsedResult.rawText || '暂无结果'} className="text-xs text-zinc-700 dark:text-zinc-200" />
                  )}
                </>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
