'use client'

import { useCallback, useEffect, useMemo, useState } from 'react'

import { formatInterruptDecisionMessage, sanitizeInterruptPayload } from '@/lib/interrupt-utils'
import { executionModeToRunIntent } from '@/lib/runIntent'
import { wsClient } from '@/lib/websocket'
import { generateId } from '@/lib/utils'
import { useChatStore } from '@/stores/chatStore'
import { useConnectionStore } from '@/stores/connectionStore'
import { useInstanceStore } from '@/stores/instanceStore'
import { useSessionStore } from '@/stores/sessionStore'
import type { FlowSubmission, FlowRunState, ResourceResumeChoice } from '@/features/flow-run/types'

interface UseChatMessagingParams {
  ensureSession: () => Promise<string | null>
}

export function useChatMessaging({ ensureSession }: UseChatMessagingParams) {
  const {
    sessionId: chatSessionId,
    messages,
    isThinking,
    isRunning,
    executionMode,
    thinkingEnabled,
    tasks,
    pendingInterrupts,
    workflowBlocks,
    addMessage,
    setSessionId,
    setThinking,
    setRunning,
    resolveInterrupt,
  } = useChatStore()

  const connected = useConnectionStore((s) => s.connected)

  const activeSessionId = useSessionStore((s) => s.activeSessionId)

  const instances = useInstanceStore((s) => s.instances)
  const activeInstanceId = useInstanceStore((s) => s.activeInstanceId)
  const projectConnected = useInstanceStore((s) => s.projectConnected)

  const activeInstance = useMemo(
    () => instances.find((item) => item.id === activeInstanceId) || null,
    [activeInstanceId, instances],
  )
  const projectPath = activeInstance?.project_path || ''

  const [interruptSubmitting, setInterruptSubmitting] = useState('')
  const [resolvedInterruptStates, setResolvedInterruptStates] = useState<
    Record<string, 'approved' | 'rejected'>
  >({})

  const waitForRealtimeConnection = useCallback(async (timeoutMs = 1500) => {
    if (wsClient.connected) {
      return true
    }
    return wsClient.waitUntilConnected(timeoutMs)
  }, [])

  const resolveInterruptCheckpointId = useCallback((rawCheckpointId: string) => {
    const normalizedCheckpointId = String(rawCheckpointId || '').trim()
    if (normalizedCheckpointId) {
      return normalizedCheckpointId
    }
    const fallbackCheckpointId = String(pendingInterrupts[0]?.checkpoint_id || '').trim()
    return fallbackCheckpointId
  }, [pendingInterrupts])

  const handleSend = useCallback(async (content: string, flow?: FlowSubmission) => {
    if (!connected) {
      addMessage({
        type: 'error',
        content: '当前后端连接不可用，无法发送消息。请等待连接恢复后重试。',
      })
      return false
    }

    if (!projectConnected || !projectPath.trim()) {
      addMessage({
        type: 'error',
        content: '当前后端在线，但未连接可用的 QGIS 项目。请先打开或重新连接 QGIS 项目后再发送新消息。',
      })
      return false
    }

    const realtimeConnected = await waitForRealtimeConnection()
    if (!realtimeConnected) {
      addMessage({
        type: 'error',
        content: '当前实时连接正在恢复，消息尚未发送。请等待连接稳定后重试。',
      })
      return false
    }

    const sessionId = await ensureSession()
    if (!sessionId) {
      return false
    }

    setSessionId(sessionId)
    addMessage({ type: 'user', content })
    setRunning(true)
    setThinking(true)

    const taskRequestId = flow ? `task-${generateId()}` : ''
    const sendPayload = {
      execution_mode: executionMode,
      run_intent: executionModeToRunIntent(executionMode),
      thinking_enabled: thinkingEnabled,
      project_path: projectPath,
      instance_id: activeInstanceId,
      ...(flow ? { flow, request_id: taskRequestId, idempotency_key: taskRequestId } : {}),
    }

    const send = () => flow ? wsClient.sendTask(content, sessionId, sendPayload) : wsClient.sendChat(content, sessionId, sendPayload)
    let sent = send()
    if (!sent && await waitForRealtimeConnection()) {
      sent = send()
    }

    if (!sent) {
      setThinking(false)
      setRunning(false)
      addMessage({
        type: 'error',
        content: '当前连接不可用，消息未发送。请稍后重试。',
      })
    }
    return sent
  }, [
    activeInstanceId,
    addMessage,
    connected,
    ensureSession,
    executionMode,
    projectConnected,
    projectPath,
    setRunning,
    setSessionId,
    setThinking,
    thinkingEnabled,
    waitForRealtimeConnection,
  ])

  const handleCancel = useCallback(() => {
    const sent = wsClient.sendCancel(activeSessionId || chatSessionId || undefined)
    if (sent) {
      setThinking(false)
    } else {
      addMessage({
        type: 'error',
        content: '取消请求发送失败，连接可能已断开。请等待重连后重试。',
      })
    }
  }, [activeSessionId, addMessage, chatSessionId, setThinking])

  const handleResumeFlow = useCallback((run: FlowRunState, choice?: ResourceResumeChoice) => handleSend(`${choice === 'restart' ? '重新开始' : '继续'}流程「${run.plan?.name || run.name || run.flow_id}」`, {
    id: run.flow_id, resume_run_id: run.flow_run_id,
    ...(choice ? { resume_resource_choice: choice, resource_fingerprint: run.resource_change?.fingerprint } : {}),
  }), [handleSend])

  const handleInterruptApprove = useCallback((checkpointId: string, payload?: unknown) => {
    const finalCheckpointId = resolveInterruptCheckpointId(checkpointId)
    if (!finalCheckpointId) {
      addMessage({
        type: 'error',
        content: '无法确认中断：缺少 checkpoint_id。',
      })
      return
    }

    if (interruptSubmitting === finalCheckpointId || resolvedInterruptStates[finalCheckpointId]) {
      return
    }

    setInterruptSubmitting(finalCheckpointId)
    const sent = wsClient.sendInterruptResponse({
      checkpointId: finalCheckpointId,
      approved: true,
      userData: sanitizeInterruptPayload(payload),
      sessionId: activeSessionId || chatSessionId || undefined,
    })
    if (!sent) {
      setInterruptSubmitting('')
      addMessage({
        type: 'error',
        content: '中断确认发送失败，请检查连接后重试。',
      })
      return
    }

    setResolvedInterruptStates((prev) => ({
      ...prev,
      [finalCheckpointId]: 'approved',
    }))
    resolveInterrupt(finalCheckpointId)
    setThinking(true)
    setInterruptSubmitting('')
    const flowInterrupt = pendingInterrupts.find(item => item.checkpoint_id === finalCheckpointId && item.flow_run_id)
    const displayPayload = flowInterrupt && payload && typeof payload === 'object' && !Array.isArray(payload)
      ? { ...payload, ...('option' in payload ? { option: ({ retry: '重试本步', skip: '跳过本步', stop: '终止流程', confirm: '确认', reject: '不确认' } as Record<string, string>)[String(payload.option)] || payload.option } : {}) }
      : payload
    addMessage({
      type: 'decision',
      content: formatInterruptDecisionMessage(finalCheckpointId, displayPayload),
      ...(flowInterrupt ? { data: { flow_run_id: flowInterrupt.flow_run_id, step_id: flowInterrupt.step_id } } : {}),
    })
  }, [
    activeSessionId,
    addMessage,
    chatSessionId,
    interruptSubmitting,
    pendingInterrupts,
    resolveInterrupt,
    resolveInterruptCheckpointId,
    resolvedInterruptStates,
    setThinking,
  ])

  const handleInterruptReject = useCallback((checkpointId: string) => {
    const finalCheckpointId = resolveInterruptCheckpointId(checkpointId)
    if (!finalCheckpointId) {
      addMessage({
        type: 'error',
        content: '无法拒绝中断：缺少 checkpoint_id。',
      })
      return
    }

    if (interruptSubmitting === finalCheckpointId || resolvedInterruptStates[finalCheckpointId]) {
      return
    }

    setInterruptSubmitting(finalCheckpointId)
    const sent = wsClient.sendInterruptResponse({
      checkpointId: finalCheckpointId,
      approved: false,
      sessionId: activeSessionId || chatSessionId || undefined,
    })
    if (!sent) {
      setInterruptSubmitting('')
      addMessage({
        type: 'error',
        content: '中断拒绝发送失败，请检查连接后重试。',
      })
      return
    }

    setResolvedInterruptStates((prev) => ({
      ...prev,
      [finalCheckpointId]: 'rejected',
    }))
    resolveInterrupt(finalCheckpointId)
    setInterruptSubmitting('')
    addMessage({
      type: 'decision',
      content: `决策结果（${finalCheckpointId}）\n- 已拒绝该决策请求。`,
    })
  }, [
    activeSessionId,
    addMessage,
    chatSessionId,
    interruptSubmitting,
    resolveInterrupt,
    resolveInterruptCheckpointId,
    resolvedInterruptStates,
  ])

  useEffect(() => {
    setResolvedInterruptStates({})
  }, [activeSessionId])

  // The server may reissue the same checkpoint after a resource-change guard.
  // Its original confirmation remains actionable until ADK actually resumes it.
  useEffect(() => {
    setResolvedInterruptStates(previous => {
      const reissued = pendingInterrupts.filter(item => item.flow_run_id).map(item => String(item.checkpoint_id || '')).filter(id => id && previous[id])
      if (!reissued.length) return previous
      const next = { ...previous }
      for (const id of reissued) delete next[id]
      return next
    })
  }, [pendingInterrupts])

  const activeInterrupt = pendingInterrupts.length > 0 ? pendingInterrupts[0] : null
  const activeInterruptCheckpointId = String(activeInterrupt?.checkpoint_id || '')

  return {
    activeInterrupt,
    activeInterruptCheckpointId,
    connected,
    handleCancel,
    handleInterruptApprove,
    handleInterruptReject,
    handleSend,
    handleResumeFlow,
    interruptSubmitting,
    isRunning,
    isThinking,
    messages,
    pendingInterrupts,
    projectConnected,
    resolvedInterruptStates,
    tasks,
    workflowBlocks,
  }
}
