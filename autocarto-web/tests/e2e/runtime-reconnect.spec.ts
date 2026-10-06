import { expect, test, type Page } from '@playwright/test'

async function mockDesktopRuntime(page: Page, port = 3000) {
  await page.addInitScript((runtimePort) => {
    const runtime = window as Window & {
      __TAURI_INTERNALS__?: {
        invoke?: (cmd: string, args?: unknown) => Promise<unknown>
      }
      __TAURI__?: Record<string, unknown>
    }

    const existingInvoke = runtime.__TAURI_INTERNALS__?.invoke

    runtime.__TAURI_INTERNALS__ = {
      ...(runtime.__TAURI_INTERNALS__ || {}),
      invoke: async (cmd: string, args?: unknown) => {
        if (cmd === 'get_backend_runtime') {
          return {
            port: runtimePort,
            backend_url: `http://127.0.0.1:${runtimePort}`,
            ws_url: `ws://127.0.0.1:${runtimePort}/ws`,
            startup_error: '',
          }
        }
        if (existingInvoke) {
          return existingInvoke(cmd, args)
        }
        throw new Error(`unexpected tauri invoke: ${cmd}`)
      },
    }
    runtime.__TAURI__ = {
      ...(runtime.__TAURI__ || {}),
    }
  }, port)
}

async function mockVersionMismatchWebSocket(page: Page, detail: string) {
  await page.addInitScript((userAction) => {
    class MockWebSocket {
      static CONNECTING = 0
      static OPEN = 1
      static CLOSING = 2
      static CLOSED = 3

      readonly url: string
      readyState = MockWebSocket.CONNECTING
      private _onopen: ((event: Event) => void) | null = null
      private _onmessage: ((event: MessageEvent) => void) | null = null
      private _onclose: ((event: CloseEvent) => void) | null = null
      private _onerror: ((event: Event) => void) | null = null
      private _emitTimer: number | null = null

      constructor(url: string) {
        this.url = url
      }

      set onopen(handler: ((event: Event) => void) | null) {
        this._onopen = handler
        if (handler) {
          this.readyState = MockWebSocket.OPEN
          handler(new Event('open'))
        }
      }

      get onopen() {
        return this._onopen
      }

      set onmessage(handler: ((event: MessageEvent) => void) | null) {
        this._onmessage = handler
        if (handler) {
          const emitUnsupported = () => {
            if (!this._onmessage || this.readyState !== MockWebSocket.OPEN) {
              return
            }
            this._onmessage(
              new MessageEvent('message', {
                data: JSON.stringify({
                  type: 'error',
                  content: '',
                  data: {
                    code: 'UNSUPPORTED_PROTOCOL_VERSION',
                    user_action: userAction,
                  },
                }),
              }),
            )
          }

          window.setTimeout(emitUnsupported, 50)
          this._emitTimer = window.setInterval(emitUnsupported, 200)
        }
      }

      get onmessage() {
        return this._onmessage
      }

      set onclose(handler: ((event: CloseEvent) => void) | null) {
        this._onclose = handler
      }

      get onclose() {
        return this._onclose
      }

      set onerror(handler: ((event: Event) => void) | null) {
        this._onerror = handler
      }

      get onerror() {
        return this._onerror
      }

      send() {}

      close() {
        this.readyState = MockWebSocket.CLOSED
        if (this._emitTimer !== null) {
          window.clearInterval(this._emitTimer)
          this._emitTimer = null
        }
        this._onclose?.(new CloseEvent('close'))
      }

      addEventListener() {}
      removeEventListener() {}
    }

    Object.defineProperty(window, 'WebSocket', {
      configurable: true,
      writable: true,
      value: MockWebSocket,
    })
  }, detail)
}

test.describe('runtime reconnect version mismatch T19', () => {
  for (const path of ['/chat.html', '/diagnostics.html', '/instances.html']) {
    test(`surfaces unsupported protocol mismatches on ${path}`, async ({ page }) => {
      const detail = '协议版本不兼容，请刷新页面。'

      await mockDesktopRuntime(page)
      await mockVersionMismatchWebSocket(page, detail)

      await page.route('**/api/health', async (route) => {
        await route.fulfill({
          json: {
            success: true,
            data: { ok: true },
          },
        })
      })

      await page.route('**/api/runtime/snapshot', async (route) => {
        await route.fulfill({
          json: {
            success: true,
            data: {
              services: {},
              instances: {
                items: [],
                active_id: null,
                instance_count: 0,
                running_count: 0,
              },
              stale: false,
            },
          },
        })
      })

      await page.goto(path, { waitUntil: 'networkidle' })
      await expect
        .poll(async () => await page.locator('body').innerText(), { timeout: 15_000 })
        .toContain(detail)
    })
  }
})
