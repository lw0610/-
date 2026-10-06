'use client'

import { useCallback, useEffect, useMemo, useRef } from 'react'
import type { MutableRefObject } from 'react'

import api, { SessionRecord } from '@/lib/api'
import {
  estimateContextStats,
  toContextMessages,
  toMessagesFromCompressedContext,
  toSessionMessage,
} from '@/lib/chat-message-utils'
import { getGlobalPersistenceController } from '@/hooks/useGlobalPersistence'
import type { ApplySessionRecord, SessionRuntimeSettings } from '@/hooks/useChatSession'
import { executionModeToRunIntent } from '@/lib/runIntent'
import { wsClient } from '@/lib/websocket'
import { useChatStore } from '@/stores/chatStore'
import { useInstanceStore } from '@/stores/instanceStore'
import { useSessionStore } from '@/stores/sessionStore'
import type { ContextResponseData, ExecutionMode } from '@/types/message'

interface UseChatContextParams {
  ensureSession: () => Promise<string | null>
  createSnapshotForSession: (
    sessionId: string,
    reason?: 'manual' | 'auto_compress' | 'clear',
  ) => Promise<string>
  sessionSettings: SessionRuntimeSettings
  applySessionRecord: ApplySessionRecord
  sessionRef: MutableRefObject<SessionRecord | null>
  planUnavailableReason: string
}

export function useChatContext({
  ensureSession,
  createSnapshotForSession,
  sessionSettings,
  applySessionRecord,
  sessionRef,
  planUnavailableReason,
}: UseChatContextParams) {
  const {
    sessionId: chatSessionId,
    messages,
    workflowBlocks,
    executionMode,
    thinkingEnabled,
    contextStats,
    lastContextResponse,
    addMessage,
    setContextStats,
    setExecutionMode,
    setThinkingEnabled,
    setLastContextResponse,
    setMessages,
  } = useChatStore()

  const activeSessionId = useSessionStore((s) => s.activeSessionId)

  const instances = useInstanceStore((s) => s.instances)
  const activeInstanceId = useInstanceStore((s) => s.activeInstanceId)

  const activeInstance = useMemo(
    () => instances.find((item) => item.id === activeInstanceId) || null,
    [activeInstanceId, instances],
  )
  const projectPath = activeInstance?.project_path || ''

  const contextRequestRef = useRef<string>('')

  const handleRefreshContextStats = useCallback(() => {
    const contextMessages = toContextMessages(messages)
    const requestId = wsClient.sendContextStats(
      contextMessages,
      activeSessionId || chatSessionId || undefined,
    )
    if (!requestId) {
      addMessage({
        type: 'error',
        content: '上下文统计请求发送失败，请检查连接后重试。',
      })
      return
    }
    contextRequestRef.current = requestId
  }, [activeSessionId, addMessage, chatSessionId, messages])

  const handleCompressContext = useCallback(async () => {
    const contextMessages = toContextMessages(messages)
    if (contextMessages.length <= 1) {
      addMessage({
        type: 'status',
        content: '上下文消息过少，无需压缩。',
      })
      return
    }

    if (sessionSettings.autoSnapshotBeforeCompress) {
      try { const sessionId = activeSessionId || await ensureSession()
      if (!sessionId) {
        return
      }
      await createSnapshotForSession(sessionId, 'auto_compress')
      addMessage({
        type: 'status',
        content: '压缩前快照已自动创建。',
      }) } catch (error) { void error; addMessage({
        type: 'error',
        content: '自动创建快照失败，已取消上下文压缩。',
      })
      return }
    }

    const requestId = wsClient.sendContextCompress(
      contextMessages,
      activeSessionId || chatSessionId || undefined,
    )
    if (!requestId) {
      addMessage({
        type: 'error',
        content: '上下文压缩请求发送失败，请检查连接后重试。',
      })
      return
    }

    contextRequestRef.current = requestId
    addMessage({
      type: 'status',
      content: '正在请求后端压缩上下文...',
    })
  }, [
    activeSessionId,
    addMessage,
    chatSessionId,
    createSnapshotForSession,
    ensureSession,
    messages,
    sessionSettings.autoSnapshotBeforeCompress,
  ])

  const handleExecutionModeChange = useCallback(async (mode: ExecutionMode) => {
    if (mode === 'plan' && planUnavailableReason) {
      addMessage({
        type: 'error',
        content: `Plan 模式当前不可用：${planUnavailableReason}`,
      })
      return
    }

    setExecutionMode(mode)
    const sent = wsClient.sendExecutionModeSwitch(
      mode,
      activeSessionId || chatSessionId || undefined,
      projectPath,
    )
    if (!sent) {
      addMessage({
        type: 'error',
        content: '执行模式实时同步失败，已尝试仅保存会话。',
      })
    }
	if (!activeSessionId) {
	  return
	}
	if (!projectPath.trim()) {
	  addMessage({
		type: 'error',
		content: '当前未绑定项目路径，无法持久化执行模式。',
	  })
	  return
	}

	try { const updated = await api.patchSession(
  activeSessionId,
  {
    execution_mode: mode,
    run_intent: executionModeToRunIntent(mode),
  },
  projectPath,
)
applySessionRecord(updated) } catch (error) { void error; addMessage({
  type: 'error',
  content: '执行模式更新失败，请稍后重试。',
}) }
  }, [
    activeSessionId,
    addMessage,
    applySessionRecord,
    chatSessionId,
    planUnavailableReason,
    projectPath,
    setExecutionMode,
  ])

  const handleThinkingToggle = useCallback((enabled: boolean) => {
    setThinkingEnabled(enabled)
  }, [setThinkingEnabled])

  useEffect(() => {
    setContextStats(estimateContextStats(messages, sessionRef.current?.context_stats as Record<string, unknown> | undefined))
  }, [messages, sessionRef, setContextStats])

  useEffect(() => {
    const data = lastContextResponse
    if (!data) {
      return
    }

    const requestId = String(data.request_id || '')
    if (contextRequestRef.current && requestId && requestId !== contextRequestRef.current) {
      return
    }

    const apply = async (payload: ContextResponseData) => {
      if (payload.success === false) {
        setLastContextResponse(null)
        contextRequestRef.current = ''
        return
      }

      if (Array.isArray(payload.compressed_context)) {
        const compressedMessages = toMessagesFromCompressedContext(payload.compressed_context)
		  if (compressedMessages.length > 0) {
			setMessages(compressedMessages)
			if (!projectPath.trim()) {
			  addMessage({
				type: 'error',
				content: '当前未绑定项目路径，无法保存压缩后的会话。',
			  })
			  return
			}

			const sessionId = await ensureSession()
          if (!sessionId) {
            return
          }

          const session = sessionRef.current || await api.getSession(sessionId, projectPath)
          const history = Array.isArray(session.compression_history)
            ? [...session.compression_history]
            : []
          const beforeStats = estimateContextStats(messages)
          const afterStats = estimateContextStats(compressedMessages)

          history.push({
            timestamp: new Date().toISOString(),
            before_tokens: beforeStats.estimatedTokens,
            after_tokens: afterStats.estimatedTokens,
            strategy: 'summary',
            summary_preview: String(payload.summary || '').slice(0, 200),
          })

          const updated: SessionRecord = {
            ...session,
            messages: compressedMessages.map(toSessionMessage),
            execution_mode: executionMode,
            run_intent: executionModeToRunIntent(executionMode),
            context_stats: {
              total_tokens: afterStats.estimatedTokens,
              compressed_count: Number(payload.compressed_count || compressedMessages.length),
            },
            compression_history: history,
            updated_at: new Date().toISOString(),
          }

          await api.saveSession(sessionId, updated, projectPath)
          applySessionRecord(updated)
          getGlobalPersistenceController().markSessionLoaded(sessionId, {
            messages: compressedMessages,
            persistedMessages: compressedMessages,
            workflowBlocks,
            executionMode,
            sessionRecord: updated,
          })

          addMessage({
            type: 'context',
            content: '上下文已压缩，历史消息已更新。',
          })
        }
      }

      setLastContextResponse(null)
      contextRequestRef.current = ''
    }

    apply(data).catch(() => {
      setLastContextResponse(null)
      contextRequestRef.current = ''
    })
  }, [
    addMessage,
    applySessionRecord,
    ensureSession,
    executionMode,
    lastContextResponse,
    messages,
    projectPath,
    sessionRef,
    setLastContextResponse,
    setMessages,
    workflowBlocks,
  ])

  return {
    contextStats,
    executionMode,
    handleCompressContext,
    handleExecutionModeChange,
    handleRefreshContextStats,
    handleThinkingToggle,
    thinkingEnabled,
  }
}
