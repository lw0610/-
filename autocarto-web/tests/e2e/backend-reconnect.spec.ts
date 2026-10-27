import { expect, test, type Page } from '@playwright/test'

import { mockDesktopRuntime } from './helpers/desktopRuntime'

async function bodyText(page: Page) {
  return page.locator('body').innerText()
}

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

function buildHistoricalSessionPayload() {
  return {
    success: true,
    data: {
      session_id: 'sid-history-restore',
      name: '历史恢复验证',
      created_at: '2026-03-19T10:00:00.000Z',
      updated_at: '2026-03-19T10:10:00.000Z',
      project_path: '/tmp/history-restore.qgz',
      execution_mode: 'build',
      run_intent: 'execute',
      messages: [
        {
          role: 'user',
          msg_type: 'user',
          content: '制作湖北省 GDP 条形统计专题图',
          timestamp: '2026-03-19T10:00:00.000Z',
        },
        {
          role: 'assistant',
          msg_type: 'assistant',
          content: '好的，开始分析。',
          timestamp: '2026-03-19T10:00:02.000Z',
        },
      ],
      tasks: [
        {
          content: '恢复任务列表',
          status: 'in_progress',
        },
        {
          content: '验证 write_todos 完成态',
          status: 'completed',
        },
      ],
      workflow_blocks: [
        {
          id: 'wf-history',
          agent: 'planner',
          agentDisplayName: 'Planner',
          summary: '已完成规划',
          status: 'completed',
          startedAt: Date.parse('2026-03-19T10:00:01.000Z'),
          updatedAt: Date.parse('2026-03-19T10:00:03.000Z'),
          entries: [
            {
              id: 'entry-write-todos',
              type: 'tool',
              content: '工具返回: write_todos',
              phase: 'tool_result',
              timestamp: Date.parse('2026-03-19T10:00:03.000Z'),
              data: {
                tool: 'write_todos',
                status: 'completed',
                result: '{"success":true,"status_source":"task_list_update"}',
              },
              toolCard: {
                id: 'tool-write-todos',
                tool: 'write_todos',
                status: 'completed',
                statusSource: 'task_list_update',
                success: true,
                result: '{"success":true,"status_source":"task_list_update"}',
                startedAt: Date.parse('2026-03-19T10:00:02.000Z'),
                finishedAt: Date.parse('2026-03-19T10:00:03.000Z'),
              },
            },
          ],
        },
      ],
    },
  }
}

async function routeHistoricalSessionApis(page: Page) {
  await page.route('**/api/sessions**', async (route) => {
    const url = route.request().url()
    if (url.includes('/api/sessions/sid-history-restore')) {
      await route.fulfill({ json: buildHistoricalSessionPayload() })
      return
    }

    await route.fulfill({
      json: {
        success: true,
        data: {
          sessions: [
            {
              session_id: 'sid-history-restore',
              name: '历史恢复验证',
              created_at: '2026-03-19T10:00:00.000Z',
              updated_at: '2026-03-19T10:10:00.000Z',
              project_path: '/tmp/history-restore.qgz',
              last_message_preview: '[assistant] 好的，开始分析。',
              archived: false,
              messages: [],
            },
          ],
        },
      },
    })
  })
}

test('ui relaunch keeps backend continuity and reattaches session', async ({ page }) => {
  await disableRealtimeWebSocket(page)
  await mockDesktopRuntime(page)
  await routeHistoricalSessionApis(page)

  await page.route('**/api/runtime/snapshot', async (route) => {
    await route.fulfill({
      json: {
        success: true,
        data: {
          services: {},
          instances: {
            items: [
              {
                id: 'instance-running',
                pid: 1201,
                qgis_version: '3.34',
                project_path: '/tmp/history-restore.qgz',
                status: 'running',
                services: {},
                created_at: '2026-03-19T10:00:00.000Z',
                updated_at: '2026-03-19T10:00:00.000Z',
                last_heartbeat: '2026-03-19T10:00:00.000Z',
                source: 'reverse_ws',
              },
            ],
            active_id: 'instance-running',
            instance_count: 1,
            running_count: 1,
          },
          stale: false,
        },
      },
    })
  })

  await page.goto('/chat.html', { waitUntil: 'networkidle' })
  await page.getByRole('button', { name: /历史恢复验证/ }).click()
  await page.getByRole('button', { name: /查看 Planner 详情/ }).click()
  await expect(page.getByRole('tab', { name: '任务与过程', exact: true })).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByText('制作湖北省 GDP 条形统计专题图')).toHaveCount(1)
  await expect(page.getByText('恢复任务列表')).toBeVisible()
  await expect(page.locator('body')).not.toContainText('无法连接到后端服务')

  await page.reload({ waitUntil: 'networkidle' })
  await page.getByRole('button', { name: /历史恢复验证/ }).click()
  await page.getByRole('button', { name: /查看 Planner 详情/ }).click()
  await expect(page.getByRole('tab', { name: '任务与过程', exact: true })).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByText('制作湖北省 GDP 条形统计专题图')).toHaveCount(1)
  await expect(page.getByText('验证 write_todos 完成态')).toBeVisible()
  await expect(page.locator('body')).not.toContainText('无法连接到后端服务')
})

test('descriptor stale refreshes runtime without fatal regression', async ({ page }) => {
  await disableRealtimeWebSocket(page)
  await mockDesktopRuntime(page)

  let serveStaleSnapshot = true
  await page.route('**/api/runtime/snapshot', async (route) => {
    const stale = serveStaleSnapshot
    await route.fulfill({
      json: {
        success: true,
        data: {
          services: {},
          instances: {
            items: stale
              ? []
              : [
                  {
                    id: 'instance-running',
                    pid: 1201,
                    qgis_version: '3.34',
                    project_path: '/tmp/recovered-runtime.qgz',
                    status: 'running',
                    services: {},
                    created_at: '2026-03-19T10:00:00.000Z',
                    updated_at: '2026-03-19T10:00:00.000Z',
                    last_heartbeat: '2026-03-19T10:00:00.000Z',
                    source: 'reverse_ws',
                  },
                ],
            active_id: stale ? null : 'instance-running',
            instance_count: stale ? 0 : 1,
            running_count: stale ? 0 : 1,
          },
          stale,
        },
      },
    })
  })

  await page.route('**/api/sessions**', async (route) => {
    await route.fulfill({
      json: {
        success: true,
        data: { sessions: [] },
      },
    })
  })

  await page.goto('/chat.html', { waitUntil: 'networkidle' })
  await expect(page.locator('body')).toContainText('运行时同步中')
  await expect(page.locator('body')).not.toContainText('无法连接到后端服务')

  serveStaleSnapshot = false
  await page.reload({ waitUntil: 'networkidle' })
  await expect.poll(async () => await bodyText(page), { timeout: 15_000 }).not.toContain('运行时同步中')
  await expect(page.locator('body')).not.toContainText('无法连接到后端服务')
})
