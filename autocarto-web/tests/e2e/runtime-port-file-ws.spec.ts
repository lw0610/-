import { expect, test } from '@playwright/test'
import { mockDesktopRuntime } from './helpers/desktopRuntime'

test('desktop frontend connects to websocket URL from runtime port file', async ({ page }) => {
  const runtimePort = 61234
  await mockDesktopRuntime(page, { port: runtimePort })

  await page.addInitScript(() => {
    const calls: string[] = []

    class MockWebSocket {
      static CONNECTING = 0
      static OPEN = 1
      static CLOSING = 2
      static CLOSED = 3

      readyState = MockWebSocket.CONNECTING
      onopen: ((event: Event) => void) | null = null
      onmessage: ((event: MessageEvent) => void) | null = null
      onclose: ((event: CloseEvent) => void) | null = null
      onerror: ((event: Event) => void) | null = null

      constructor(url: string | URL) {
        calls.push(String(url))
        setTimeout(() => {
          this.readyState = MockWebSocket.OPEN
          this.onopen?.(new Event('open'))
        }, 0)
      }

      close() {
        this.readyState = MockWebSocket.CLOSED
        this.onclose?.(new CloseEvent('close'))
      }

      send() {}
      addEventListener() {}
      removeEventListener() {}
      dispatchEvent() {
        return true
      }
    }

    ;(window as Window & { __AUTOCARTO_WS_CALLS__?: string[] }).__AUTOCARTO_WS_CALLS__ =
      calls
    ;(window as Window & { WebSocket: typeof WebSocket }).WebSocket =
      MockWebSocket as unknown as typeof WebSocket
  })

  await page.goto('/')

  await expect
    .poll(async () =>
      page.evaluate(() =>
        (window as Window & { __AUTOCARTO_WS_CALLS__?: string[] }).__AUTOCARTO_WS_CALLS__ || [],
      ),
    )
    .toContain(`ws://127.0.0.1:${runtimePort}/ws`)
})
