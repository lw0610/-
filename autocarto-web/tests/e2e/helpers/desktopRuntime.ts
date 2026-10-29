import type { Page } from '@playwright/test'

export const DEFAULT_DESKTOP_RUNTIME_PORT = Number(process.env.AUTOCARTO_TEST_RUNTIME_PORT || '50141')

interface MockDesktopRuntimeOptions {
  port?: number
  startupError?: string
  omitAuthoritativeUrls?: boolean
  nullCommands?: string[]
}

export async function mockDesktopRuntime(
  page: Page,
  options: MockDesktopRuntimeOptions = {},
) {
  const {
    port = DEFAULT_DESKTOP_RUNTIME_PORT,
    startupError = '',
    omitAuthoritativeUrls = false,
    nullCommands = [],
  } = options

  await page.addInitScript(
    ({ runtimePort, startupError, omitAuthoritativeUrls, nullCommands }) => {
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
              backend_url: omitAuthoritativeUrls ? '' : `http://127.0.0.1:${runtimePort}`,
              ws_url: omitAuthoritativeUrls ? '' : `ws://127.0.0.1:${runtimePort}/ws`,
              startup_error: startupError,
            }
          }
          if (nullCommands.includes(cmd)) {
            return null
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
    },
    {
      runtimePort: port,
      startupError,
      omitAuthoritativeUrls,
      nullCommands,
    },
  )
}
