import type { ExecutionMode, WSMessage } from '@/types/message'
import { useInstanceStore } from '@/stores/instanceStore'
import { InstanceEvent } from '@/types/instance'
import { executionModeToRunIntent } from '@/lib/runIntent'
import { generateId } from './utils'

type MessageHandler = (msg: WSMessage) => void
type ConnectionHandler = (connected: boolean) => void
type InstanceSelectError = Error & { activeId?: string }

function createInstanceSelectError(message: string, activeId?: string): InstanceSelectError {
  const error = new Error(message) as InstanceSelectError
  const normalizedActiveId = typeof activeId === 'string' ? activeId.trim() : ''
  if (normalizedActiveId) {
    error.activeId = normalizedActiveId
  }
  return error
}

export function getInstanceSelectRollbackTarget(error: unknown, fallbackActiveId: string | null) {
  if (error && typeof error === 'object') {
    const activeId =
      typeof (error as { activeId?: unknown }).activeId === 'string'
        ? (error as { activeId?: string }).activeId?.trim() || ''
        : ''
    if (activeId) {
      return activeId
    }
  }
  return fallbackActiveId
}

const MAX_RECONNECT_DELAY = 30000
const INITIAL_RECONNECT_DELAY = 1000
const FAST_RECONNECT_ATTEMPTS = 5
const RECONNECT_JITTER_RATIO = 0.2
const WEBSOCKET_ROUTE = '/ws'
const CANCEL_IDEMPOTENCY_WINDOW_MS = 15000

function normalizeWebSocketRoute(url: string) {
  const trimmed = url.trim()
  if (!trimmed) {
    return ''
  }

  try {
    const parsed = new URL(trimmed)
    parsed.pathname = WEBSOCKET_ROUTE
    parsed.search = ''
    parsed.hash = ''
    return parsed.toString().replace(/\/$/, '')
  } catch {
    return trimmed
  }
}

export class WebSocketClient {
  private ws: WebSocket | null = null
  private url: string
  private reconnectDelay = INITIAL_RECONNECT_DELAY
  private reconnectAttempts = 0
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null
  private messageHandlers: Set<MessageHandler> = new Set()
  private connectionHandlers: Set<ConnectionHandler> = new Set()
  private shouldReconnect = true
  private sessionId: string | null = null
  private cancelIdempotencyKeys: Map<string, { key: string; expiresAt: number }> = new Map()

  constructor(url?: string) {
    this.url = typeof url === 'string' ? normalizeWebSocketRoute(url) : ''
  }

  connect(sessionId?: string) {
    this.shouldReconnect = true
    this.reconnectAttempts = 0
    this.reconnectDelay = INITIAL_RECONNECT_DELAY
    this.sessionId = sessionId || null
    this.doConnect()
  }

  private doConnect() {
    if (this.ws?.readyState === WebSocket.OPEN || this.ws?.readyState === WebSocket.CONNECTING) {
      return
    }

    const url = this.sessionId ? `${this.url}?session_id=${this.sessionId}` : this.url
    this.ws = new WebSocket(url)

    this.ws.onopen = () => {
      this.reconnectAttempts = 0
      this.reconnectDelay = INITIAL_RECONNECT_DELAY
      this.notifyConnection(true)
    }

    this.ws.onmessage = (event) => {
      try {
        const msg: WSMessage = JSON.parse(event.data)
        if (msg.type === 'cancelled' && typeof msg.session_id === 'string') {
          this.cancelIdempotencyKeys.delete(msg.session_id.trim())
        }

        // 自动分发实例变化事件到 store
        if (msg.type === 'instance_changed' && msg.data) {
          const activeId =
            typeof msg.data.active_id === 'string'
              ? msg.data.active_id.trim()
              : ''

          if (activeId) {
            useInstanceStore.getState().setActiveInstance(activeId)
          }

          if (typeof msg.data.event_type === 'string' && msg.data.instance) {
            const instanceEvent: InstanceEvent = {
              type: msg.data.event_type as InstanceEvent['type'],
              instance: msg.data.instance as InstanceEvent['instance'],
            }
            useInstanceStore.getState().handleInstanceEvent(instanceEvent)
          }
        }

        this.messageHandlers.forEach((h) => h(msg))
      } catch (error) {
        void error
      }
    }

    this.ws.onclose = () => {
      console.warn('[WS CLOSED]', url)
      this.notifyConnection(false)
      this.scheduleReconnect()
    }

    this.ws.onerror = () => {
      console.warn('[WS ERROR]', url)
      this.ws?.close()
    }
  }

  private scheduleReconnect() {
    if (!this.shouldReconnect) {
      return
    }

    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer)
    }

    this.reconnectAttempts += 1

    const fastRetry = this.reconnectAttempts <= FAST_RECONNECT_ATTEMPTS
    const baseDelay = fastRetry ? INITIAL_RECONNECT_DELAY : this.reconnectDelay
    const jitter = Math.round(baseDelay * RECONNECT_JITTER_RATIO * Math.random())
    const delay = Math.min(baseDelay + jitter, MAX_RECONNECT_DELAY)

    this.reconnectTimer = setTimeout(() => {
      this.doConnect()
    }, delay)

    if (!fastRetry) {
      this.reconnectDelay = Math.min(this.reconnectDelay * 2, MAX_RECONNECT_DELAY)
    }
  }

  disconnect() {
    this.shouldReconnect = false
    this.reconnectAttempts = 0
    this.reconnectDelay = INITIAL_RECONNECT_DELAY

    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }

    this.ws?.close()
    this.ws = null
    this.cancelIdempotencyKeys.clear()
  }

  send(msg: Record<string, unknown>) {
    if (this.ws?.readyState !== WebSocket.OPEN) {
      return false
    }

    this.ws.send(JSON.stringify(msg))
    return true
  }

  waitUntilConnected(timeoutMs = 1500) {
    if (this.connected) {
      return Promise.resolve(true)
    }

    const normalizedUrl = this.url.trim()

    if (!normalizedUrl) {
      return Promise.resolve(false)
    }

    if (this.ws?.readyState !== WebSocket.CONNECTING) {
      this.doConnect()
    }

    return new Promise<boolean>((resolve) => {
      let settled = false
      let timer: ReturnType<typeof setTimeout> | null = null

      const finish = (connected: boolean) => {
        if (settled) {
          return
        }

        settled = true
        dispose()

        if (timer) {
          clearTimeout(timer)
          timer = null
        }

        resolve(connected)
      }

      const dispose = this.onConnection((connected) => {
        if (connected) {
          finish(true)
        }
      })

      timer = setTimeout(() => {
        finish(this.connected)
      }, timeoutMs)
    })
  }

  sendChat(content: string, sessionId?: string, data?: Record<string, unknown>) {
    const normalizedSessionId = (sessionId || this.sessionId || '').trim()
    const sent = this.send({
      type: 'chat',
      content,
      session_id: normalizedSessionId || null,
      data,
    })
    if (sent && normalizedSessionId) {
      this.cancelIdempotencyKeys.delete(normalizedSessionId)
    }
    return sent
  }

  sendModelSwitch(providerName: string) {
    return this.send({
      type: 'model_quick_switch',
      data: {
        provider_name: providerName,
        request_id: `model-${generateId()}`,
      },
    })
  }

  sendExecutionModeSwitch(mode: ExecutionMode, sessionId?: string, projectPath?: string) {
    const nextSessionId = (sessionId || this.sessionId || '').trim()

    const data: Record<string, unknown> = {
      mode,
      run_intent: executionModeToRunIntent(mode),
      request_id: `mode-${generateId()}`,
    }

    const payload: Record<string, unknown> = {
      type: 'execution_mode_switch',
      data,
    }

    if (nextSessionId) {
      payload.session_id = nextSessionId
      data.session_id = nextSessionId
    }

    if (typeof projectPath === 'string' && projectPath.trim()) {
      data.project_path = projectPath.trim()
    }

    return this.send(payload)
  }

  sendAgentQuickSwitch(configName: string) {
    return this.send({
      type: 'agent_quick_switch',
      data: {
        config_name: configName,
        request_id: `agent-${generateId()}`,
      },
    })
  }

  sendInstanceSelect(instanceId: string, requestId?: string) {
    return this.send({
      type: 'instance_select',
      data: {
        instance_id: instanceId,
        request_id: requestId || `instance-${generateId()}`,
      },
    })
  }

  selectInstance(instanceId: string, timeoutMs = 5000) {
    const normalizedInstanceId = instanceId.trim()

    if (!normalizedInstanceId) {
      return Promise.reject(new Error('缺少实例 ID'))
    }

    const requestId = `instance-${generateId()}`

    return new Promise<string>((resolve, reject) => {
      const cleanup = (() => {
        let done = false
        let dispose = () => {}
        let timer: ReturnType<typeof setTimeout> | null = null

        return {
          setDisposer(value: () => void) {
            dispose = value
          },
          setTimer(value: ReturnType<typeof setTimeout>) {
            timer = value
          },
          finish() {
            if (done) {
              return
            }

            done = true
            dispose()

            if (timer) {
              clearTimeout(timer)
              timer = null
            }
          },
        }
      })()

      const dispose = this.onMessage((msg) => {
        const data = msg.data || {}
        const incomingRequestId =
          typeof data.request_id === 'string' ? data.request_id.trim() : ''

        if (incomingRequestId !== requestId) {
          return
        }

        if (msg.type === 'instance_changed') {
          cleanup.finish()

          const success = data.success !== false
          const activeId =
            typeof data.active_id === 'string' ? data.active_id.trim() : ''

          if (!success) {
            reject(
              createInstanceSelectError(
                typeof data.error === 'string' && data.error.trim()
                  ? data.error.trim()
                  : '实例切换失败',
                activeId,
              ),
            )
            return
          }

          if (!activeId) {
            reject(createInstanceSelectError('实例切换确认消息缺少 active_id'))
            return
          }

          useInstanceStore.getState().setActiveInstance(activeId)
          resolve(activeId)
          return
        }

        if (msg.type === 'error') {
          cleanup.finish()
          reject(createInstanceSelectError(msg.content || '实例切换失败'))
        }
      })

      cleanup.setDisposer(dispose)

      const timer = setTimeout(() => {
        cleanup.finish()
        reject(new Error('等待实例切换确认超时，请稍后重试'))
      }, timeoutMs)

      cleanup.setTimer(timer)

      if (!this.sendInstanceSelect(normalizedInstanceId, requestId)) {
        cleanup.finish()
        reject(new Error('实例切换请求发送失败，请检查连接状态'))
      }
    })
  }

  sendInstanceList() {
    return this.send({ type: 'instance_list' })
  }

  sendSessionAttach(sessionId: string, lastSeenSeq?: number) {
    const normalizedSessionId = sessionId.trim()

    if (!normalizedSessionId) {
      return false
    }

    const data: Record<string, unknown> = {
      session_id: normalizedSessionId,
    }

    if (
      typeof lastSeenSeq === 'number' &&
      Number.isFinite(lastSeenSeq) &&
      lastSeenSeq > 0
    ) {
      data.last_seen_seq = Math.floor(lastSeenSeq)
    }

    return this.send({
      type: 'session_attach',
      session_id: normalizedSessionId,
      data,
    })
  }

  sendSessionDetach(sessionId: string) {
    const normalizedSessionId = sessionId.trim()

    if (!normalizedSessionId) {
      return false
    }

    return this.send({
      type: 'session_detach',
      session_id: normalizedSessionId,
      data: {
        session_id: normalizedSessionId,
      },
    })
  }

  sendCancel(sessionId?: string) {
    const normalizedSessionId = (sessionId || this.sessionId || '').trim()
    if (!normalizedSessionId) {
      return false
    }

    const now = Date.now()
    const cached = this.cancelIdempotencyKeys.get(normalizedSessionId)
    const idempotencyKey =
      cached && cached.expiresAt > now
        ? cached.key
        : `cancel-${normalizedSessionId}-${generateId()}`

    const sent = this.send({
      type: 'cancel',
      session_id: normalizedSessionId,
      idempotency_key: idempotencyKey,
      data: {
        session_id: normalizedSessionId,
        idempotency_key: idempotencyKey,
      },
    })
    if (sent) {
      this.cancelIdempotencyKeys.set(normalizedSessionId, {
        key: idempotencyKey,
        expiresAt: now + CANCEL_IDEMPOTENCY_WINDOW_MS,
      })
    }
    return sent
  }

  sendInterruptResponse(params: {
    checkpointId: string
    approved: boolean
    userData?: unknown
    sessionId?: string
  }) {
    return this.send({
      type: 'interrupt_response',
      session_id: params.sessionId || this.sessionId,
      data: {
        checkpoint_id: params.checkpointId,
        approved: params.approved,
        user_data: params.userData,
      },
    })
  }

  sendContextStats(messages: Array<Record<string, unknown>>, sessionId?: string) {
    const requestId = `ctx-stats-${generateId()}`

    const sent = this.send({
      type: 'context.stats',
      session_id: sessionId || this.sessionId,
      data: {
        request_id: requestId,
        messages,
      },
    })

    return sent ? requestId : ''
  }

  sendContextCompress(messages: Array<Record<string, unknown>>, sessionId?: string) {
    const requestId = `ctx-compress-${generateId()}`

    const sent = this.send({
      type: 'context.compress',
      session_id: sessionId || this.sessionId,
      data: {
        request_id: requestId,
        messages,
      },
    })

    return sent ? requestId : ''
  }

  onMessage(handler: MessageHandler) {
    this.messageHandlers.add(handler)
    return () => this.messageHandlers.delete(handler)
  }

  onConnection(handler: ConnectionHandler) {
    this.connectionHandlers.add(handler)
    return () => this.connectionHandlers.delete(handler)
  }

  private notifyConnection(connected: boolean) {
    this.connectionHandlers.forEach((h) => h(connected))
  }

  get connected() {
    return this.ws?.readyState === WebSocket.OPEN
  }

  setUrl(url: string) {
    this.url = normalizeWebSocketRoute(url)
  }
}

export const wsClient = new WebSocketClient()
export default wsClient
