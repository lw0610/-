import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { WebSocketClient } from './websocket'

class FakeWebSocket {
  static CONNECTING = 0
  static OPEN = 1
  static lastUrl = '__unset__'
  static latest: FakeWebSocket | null = null

  readyState = FakeWebSocket.CONNECTING
  onopen: ((event: Event) => void) | null = null
  onmessage: ((event: MessageEvent) => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null
  sentMessages: unknown[] = []

  constructor(url: string) {
    FakeWebSocket.lastUrl = url
    FakeWebSocket.latest = this
  }

  close() {}
  send(payload: string) {
    this.sentMessages.push(JSON.parse(payload))
  }

  emitMessage(payload: unknown) {
    this.onmessage?.(
      new MessageEvent('message', {
        data: JSON.stringify(payload),
      }),
    )
  }
}

describe('WebSocketClient authority-only behavior', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    vi.resetModules()
    FakeWebSocket.lastUrl = '__unset__'
    FakeWebSocket.latest = null
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('does not default websocket connections to legacy port 3000', () => {
    vi.stubGlobal('WebSocket', FakeWebSocket)

    const client = new WebSocketClient()
    client.connect()

    expect(FakeWebSocket.lastUrl).toBe('')
  })

  it('respects explicit runtime websocket urls without rewriting them', () => {
    vi.stubGlobal('WebSocket', FakeWebSocket)

    const client = new WebSocketClient('ws://127.0.0.1:50321/ws')
    client.connect('sid-1')

    expect(FakeWebSocket.lastUrl).toBe('ws://127.0.0.1:50321/ws?session_id=sid-1')
  })

  it('forwards raw websocket error frames to listeners', () => {
    vi.stubGlobal('WebSocket', FakeWebSocket)

    const client = new WebSocketClient('ws://127.0.0.1:50321/ws')
    const handler = vi.fn()
    client.onMessage(handler)

    client.connect('sid-1')
    FakeWebSocket.latest?.emitMessage({
      type: 'error',
      content: '',
      session_id: 'sid-1',
      data: {
        code: 'UNSUPPORTED_PROTOCOL_VERSION',
        user_action: '协议版本不兼容，请刷新页面。',
      },
    })

    expect(handler).toHaveBeenCalledWith(
      expect.objectContaining({
        type: 'error',
        session_id: 'sid-1',
      }),
    )
  })

  it('sends cancel requests with a reusable idempotency key for the same session', () => {
    vi.stubGlobal('WebSocket', FakeWebSocket)
    vi.spyOn(Date, 'now').mockReturnValue(1700000000000)

    const client = new WebSocketClient('ws://127.0.0.1:50321/ws')
    client.connect('sid-1')
    const socket = FakeWebSocket.latest
    expect(socket).not.toBeNull()
    socket!.readyState = FakeWebSocket.OPEN

    expect(client.sendCancel('sid-1')).toBe(true)
    expect(client.sendCancel('sid-1')).toBe(true)

    const first = socket!.sentMessages[0] as Record<string, unknown>
    const second = socket!.sentMessages[1] as Record<string, unknown>
    expect(first).toMatchObject({
      type: 'cancel',
      session_id: 'sid-1',
      data: {
        session_id: 'sid-1',
      },
    })
    expect(first.idempotency_key).toEqual(expect.stringMatching(/^cancel-sid-1-/))
    expect((first.data as Record<string, unknown>).idempotency_key).toBe(first.idempotency_key)
    expect(second.idempotency_key).toBe(first.idempotency_key)
  })

  it('refreshes cancel idempotency after the backend confirms cancellation', () => {
    vi.stubGlobal('WebSocket', FakeWebSocket)
    vi.spyOn(Date, 'now')
      .mockReturnValueOnce(1700000000000)
      .mockReturnValueOnce(1700000001000)

    const client = new WebSocketClient('ws://127.0.0.1:50321/ws')
    client.connect('sid-1')
    const socket = FakeWebSocket.latest
    expect(socket).not.toBeNull()
    socket!.readyState = FakeWebSocket.OPEN

    expect(client.sendCancel('sid-1')).toBe(true)
    const first = socket!.sentMessages[0] as Record<string, unknown>

    socket!.emitMessage({
      type: 'cancelled',
      content: 'cancelled',
      session_id: 'sid-1',
    })

    expect(client.sendCancel('sid-1')).toBe(true)
    const second = socket!.sentMessages[1] as Record<string, unknown>
    expect(second.idempotency_key).not.toBe(first.idempotency_key)
  })
})
