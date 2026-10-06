import { expect, test, type Page, type Route } from '@playwright/test'

const runtimePort = Number(process.env.AUTOCARTO_TEST_RUNTIME_PORT || '3000')
const routeSuffix = '.html'

async function mockDesktopRuntime(page: Page, port = 3000) {
  await page.addInitScript((runtimePort) => {
    const runtime = window as Window & {
      __TAURI_INTERNALS__?: {
        invoke?: (cmd: string, args?: unknown) => Promise<unknown>
      }
      __TAURI__?: Record<string, unknown>
      WebSocket?: typeof window.WebSocket
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
        if (cmd === 'pick_qgis_project_directory') {
          return null
        }
        throw new Error(`unexpected tauri invoke: ${cmd}`)
      },
    }
    runtime.__TAURI__ = {
      ...(runtime.__TAURI__ || {}),
    }

    class MockWebSocket {
      static CONNECTING = 0
      static OPEN = 1
      static CLOSING = 2
      static CLOSED = 3
      readyState = MockWebSocket.OPEN
      url: string
      onopen: ((event: Event) => void) | null = null
      onclose: ((event: CloseEvent) => void) | null = null
      onerror: ((event: Event) => void) | null = null
      onmessage: ((event: MessageEvent<string>) => void) | null = null
      constructor(url: string) {
        this.url = url
        queueMicrotask(() => {
          this.onopen?.(new Event('open'))
        })
      }
      addEventListener() {}
      removeEventListener() {}
      send() {}
      close() {
        this.readyState = MockWebSocket.CLOSED
        this.onclose?.(new CloseEvent('close'))
      }
    }

    Object.defineProperty(window, 'WebSocket', {
      configurable: true,
      writable: true,
      value: MockWebSocket,
    })
  }, port)
}

async function fulfillJson(route: Route, data: unknown) {
  await route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify({ success: true, data }),
  })
}

async function gotoRoute(page: Page, path: string) {
  for (let attempt = 0; attempt < 3; attempt += 1) {
    try {
      await page.goto(path, { waitUntil: 'commit' })
      return
    } catch (error) {
      if (attempt === 2) {
        throw error
      }
      await page.waitForTimeout(1000)
    }
  }
}

const degradedToolCatalogPayload = {
  scope: 'global',
  catalog: {
    id: 'tool_catalog/global',
    version: '1',
    availability: 'unavailable',
    degradation_status: 'abnormal_empty',
    missing_dependency_reason: 'abnormal_empty',
    items: [],
  },
  tool_infos: [
    {
      name: 'buffer',
      description: '创建缓冲区。',
      backend: 'cli',
      available: false,
      missing_reason: 'provider_missing',
      evidence_id: 'EVIDENCE-provider-missing',
    },
    {
      name: 'flash_feature',
      description: '高亮地物。',
      backend: 'unknown_runtime',
      available: false,
      missing_reason: 'unsupported_runtime',
      evidence_id: 'EVIDENCE-unsupported-runtime',
    },
  ],
  tools: ['buffer', 'flash_feature'],
}

const abnormalEmptyToolCatalogPayload = {
  scope: 'global',
  catalog: {
    id: 'tool_catalog/global',
    version: '1',
    availability: 'unavailable',
    degradation_status: 'abnormal_empty',
    missing_dependency_reason: 'abnormal_empty',
    items: [],
  },
  tool_infos: [],
  tools: ['buffer'],
}

test('degraded dictionary fixtures render on diagnostics, tool center, and instances pages', async ({ page }) => {
  test.setTimeout(90_000)
  await mockDesktopRuntime(page, runtimePort)
  let toolCatalogPayload = degradedToolCatalogPayload

  await page.route('**/api/health', async (route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ success: true }) })
  })
  await page.route('**/api/services/ports', async (route) => {
    await fulfillJson(route, {})
  })
  await page.route('**/api/capabilities', async (route) => {
    await fulfillJson(route, { qgis_available: true, instance_count: 1 })
  })
  await page.route('**/api/config', async (route) => {
    await fulfillJson(route, {
      data_dir: '/tmp/autocarto-data',
      log: { dir: '/tmp/autocarto-data/logs' },
      backend: { ws_url: `ws://127.0.0.1:${runtimePort}/ws` },
      ports: { agent_system: { default: runtimePort } },
    })
  })
  await page.route('**/api/llm/providers', async (route) => {
    await fulfillJson(route, {
      active: 'mock-provider',
      providers: [
        { id: 'mock-provider-a' },
        { id: 'mock-provider-b' },
        { id: 'mock-provider-c' },
      ],
    })
  })
  await page.route('**/api/agent-configs', async (route) => {
    await fulfillJson(route, {
      configs: [
        { config_name: 'cfg-1', is_active: true, is_default: true },
        { config_name: 'cfg-2', is_active: false, is_default: false },
        { config_name: 'cfg-3', is_active: false, is_default: false },
        { config_name: 'cfg-4', is_active: false, is_default: false },
        { config_name: 'cfg-5', is_active: false, is_default: false },
        { config_name: 'cfg-6', is_active: false, is_default: false },
      ],
    })
  })
  await page.route('**/api/skills', async (route) => {
    await fulfillJson(route, { skills: [] })
  })
  await page.route('**/api/runtime/snapshot', async (route) => {
    await fulfillJson(route, {
      runtime_revision: 'rev-degraded',
      tool_schema_revision: 'schema-degraded',
      tool_availability_revision: 'availability-degraded',
      services: {
        agent: {
          status: 'running',
          host: '127.0.0.1',
          port: 3000,
          diagnostics: {
            startup_error: '等待 plugin 注册完成后再开放连接。',
            suggested_actions: ['请等待实例重新注册后重试。'],
            evidence_id: 'EVIDENCE-diagnostics-service',
          },
        },
        qgis: {
          status: 'running',
          port: 50101,
        },
      },
      instances: {
        items: [
          {
            id: 'instance-expired',
            pid: 1001,
            qgis_version: '3.40',
            project_path: '/tmp/expired.qgz',
            status: 'dead',
            status_reason: 'lease_expired',
            status_detail: '实例心跳已过期，等待重新注册恢复。',
            services: {},
            created_at: '2026-04-04T10:00:00Z',
            updated_at: '2026-04-04T10:00:00Z',
            last_heartbeat: '2026-04-04T10:00:00Z',
            source: 'reverse_ws',
          },
        ],
        active_id: undefined,
        instance_count: 1,
        running_count: 0,
      },
      stale: false,
    })
  })
  await page.route('**/api/tools/catalog*', async (route) => {
    await fulfillJson(route, toolCatalogPayload)
  })

  await gotoRoute(page, `/diagnostics${routeSuffix}`)
  await expect(page.locator('body')).toContainText('abnormal_empty')
  await expect(page.locator('body')).toContainText('工具目录异常为空，当前能力已降级。')
  await expect(page.locator('body')).toContainText('请刷新工具目录或检查运行态注册是否完成。')

  await gotoRoute(page, `/tool-center${routeSuffix}`)
  await page.getByRole('button', { name: '工具目录' }).click()
  await expect(page.locator('body')).toContainText('abnormal_empty')
  await expect(page.locator('body')).toContainText('工具目录异常为空，当前能力已降级。')

  toolCatalogPayload = abnormalEmptyToolCatalogPayload
  await gotoRoute(page, `/tool-center${routeSuffix}`)
  await page.getByRole('button', { name: '工具目录' }).click()
  await expect(page.locator('body')).toContainText('工具目录异常为空，当前能力已降级。')

  await gotoRoute(page, `/instances${routeSuffix}`)
  await expect(page.locator('body')).toContainText('实例心跳已过期，等待重新注册恢复。')
  await expect(page.locator('body')).toContainText('等待实例重新注册或重新启动 QGIS。')
})
