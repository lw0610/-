import { expect, test, type Page } from '@playwright/test'
import type { RuntimeSnapshot } from '../../src/lib/api'
import type { QGISInstance } from '../../src/types/instance'
import { DEFAULT_DESKTOP_RUNTIME_PORT, mockDesktopRuntime } from './helpers/desktopRuntime'

async function disableRealtimeWebSocket(page: Page) {
  await page.addInitScript(() => {
    class MockWebSocket {
      static CONNECTING = 0
      static OPEN = 1
      static CLOSING = 2
      static CLOSED = 3

      readonly url: string
      readyState = MockWebSocket.OPEN
      onopen: ((event: Event) => void) | null = null
      onmessage: ((event: MessageEvent) => void) | null = null
      onclose: ((event: CloseEvent) => void) | null = null
      onerror: ((event: Event) => void) | null = null

      constructor(url: string) {
        this.url = url
        window.setTimeout(() => {
          this.onopen?.(new Event('open'))
        }, 0)
      }

      send() {}

      close() {
        this.readyState = MockWebSocket.CLOSED
        this.onclose?.(new CloseEvent('close'))
      }

      addEventListener() {}

      removeEventListener() {}
    }

    Object.defineProperty(window, 'WebSocket', {
      configurable: true,
      writable: true,
      value: MockWebSocket,
    })
  })
}

test.describe('Release Candidate Walk F3', () => {
  test('should display evidence_id and correlation_id on key UI surfaces', async ({ page }) => {
    await disableRealtimeWebSocket(page)
    await mockDesktopRuntime(page)

    let runtimeSnapshot: RuntimeSnapshot = {
      services: {},
      instances: {
        items: [],
        active_id: undefined,
        instance_count: 0,
        running_count: 0,
      },
      stale: true,
    }

    const runningInstance: QGISInstance = {
      id: 'instance-f3-running',
      pid: 4101,
      qgis_version: '3.34',
      project_path: '/tmp/release-candidate.qgz',
      status: 'running',
      services: {},
      created_at: '2026-03-19T10:00:00.000Z',
      updated_at: '2026-03-19T10:00:00.000Z',
      last_heartbeat: '2026-03-19T10:00:00.000Z',
      source: 'reverse_ws',
    }

    await page.route('**/api/config', async (route) => {
      await route.fulfill({
        json: {
          success: true,
          data: {},
        },
      })
    })

    await page.route('**/api/capabilities', async (route) => {
      await route.fulfill({
        json: {
          success: true,
          data: {
            qgis_available: true,
            instance_count: runtimeSnapshot.instances.instance_count,
          },
        },
      })
    })

    await page.route('**/api/health', async (route) => {
      await route.fulfill({
        json: {
          success: true,
          data: {
            ok: true,
          },
        },
      })
    })

    await page.route('**/api/services/ports', async (route) => {
      await route.fulfill({
        json: {
          success: true,
          data: {
            agent: {
              host: '127.0.0.1',
              port: DEFAULT_DESKTOP_RUNTIME_PORT,
              protocol: 'http',
              pid: 4101,
              status: 'running',
            },
          },
        },
      })
    })

    await page.route('**/api/llm/providers', async (route) => {
      await route.fulfill({
        json: {
          success: true,
          data: { active: '', providers: [] },
        },
      })
    })

    await page.route('**/api/agent-configs', async (route) => {
      await route.fulfill({
        json: {
          success: true,
          data: { configs: [] },
        },
      })
    })

    await page.route('**/api/skill-packages*', async (route) => {
      await route.fulfill({ json: { success: true, data: { packages: [] } } })
    })

    await page.route('**/api/skills', async (route) => {
      await route.fulfill({
        json: {
          success: true,
          data: { skills: [] },
        },
      })
    })

    await page.route('**/api/sessions**', async (route) => {
      await route.fulfill({
        json: {
          success: true,
          data: [],
        },
      })
    })

    await page.route('**/api/tools/catalog?scope=global', async (route) => {
      await route.fulfill({
        json: {
          success: true,
          data: {
            scope: 'global',
            catalog: {
              id: 'tool_catalog/global',
              version: '1',
              source: 'go_control_plane',
              capability_type: 'tool_catalog',
              runtime_target: 'mixed',
              availability: 'degraded',
              missing_dependency_reason: 'provider_missing',
              degradation_status: 'provider_missing',
              items: [
                {
                  id: 'Test Tool',
                  source: 'compute-provider',
                  capability_type: 'tool',
                  runtime_target: 'cli',
                  availability: 'unavailable',
                  missing_dependency_reason: 'Degraded provider.',
                  degradation_status: 'provider_missing',
                  evidence_id: 'EVIDENCE-8888',
                  correlation_id: 'CORR-8888',
                },
              ],
            },
          },
        },
      })
    })

    await page.route('**/api/runtime/snapshot', async (route) => {
      await route.fulfill({
        json: {
          success: true,
          data: runtimeSnapshot,
        },
      })
    })

    await page.route('**/api/runtime/services', async (route) => {
      await route.fulfill({
        json: {
          success: true,
          data: runtimeSnapshot,
        },
      })
    })

    await page.route('**/api/config/batch', async (route) => {
      await route.fulfill({
        status: 400,
        json: {
          success: false,
          error: 'Failed to save config',
          code: 'E_CONFIG_BATCH',
          data: {
            evidence_id: 'EVIDENCE-1234',
            correlation_id: 'CORR-5678',
            required_action: 'runtime_rebuild',
          },
        },
      })
    })

    await page.goto('/chat.html', { waitUntil: 'networkidle' })
    await expect(page.locator('body')).toContainText('运行时同步中')
    await expect(page.getByTestId('project-capability-banner')).toContainText('正在等待 QGIS 运行态恢复同步')
    await expect(page.getByRole('textbox', { name: '消息输入' })).toBeDisabled()
    await expect(page.getByRole('button', { name: '发送', exact: true })).toBeDisabled()

    runtimeSnapshot = {
      services: {
        agent: {
          status: 'running',
          port: DEFAULT_DESKTOP_RUNTIME_PORT,
          last_error: 'Some service error',
          diagnostics: {
            startup_error: 'Simulated startup failure',
            evidence_id: 'EVIDENCE-9999',
            correlation_id: 'CORR-9999',
          },
        },
      },
      instances: {
        items: [runningInstance],
        active_id: 'instance-f3-running',
        instance_count: 1,
        running_count: 1,
      },
      stale: false,
      last_rebuild_reason: 'Testing Evidence',
    }

    await page.goto('/diagnostics.html', { waitUntil: 'networkidle' })
    await expect(page.locator('body')).toContainText('Some service error')
    await expect(page.locator('body')).toContainText('EVIDENCE-9999')
    await expect(page.locator('body')).toContainText('CORR-9999')

    await page.goto('/tool-center.html', { waitUntil: 'networkidle' })
    await page.getByRole('button', { name: '能力手册', exact: true }).click()
    await expect(page.locator('body')).toContainText('Test Tool')
    await expect(page.locator('body')).toContainText('Degraded provider.')
    await expect(page.locator('body')).toContainText('EVIDENCE-8888')
    await expect(page.locator('body')).toContainText('CORR-8888')

    await page.goto('/instances.html', { waitUntil: 'networkidle' })
    await expect(page.locator('body')).toContainText('共 1 个实例')
    await expect(page.locator('body')).toContainText('release-candidate')

    await page.goto('/settings/system.html', { waitUntil: 'networkidle' })
    await page.getByLabel('QGIS 可执行路径').fill('/opt/qgis/bin/qgis')
    await page.getByRole('button', { name: '保存', exact: true }).click()
    await expect(page.locator('body')).toContainText('EVIDENCE-1234')
    await expect(page.locator('body')).toContainText('CORR-5678')
  })
})
