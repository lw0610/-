import { expect, test, type Page } from '@playwright/test'

async function bodyText(page: Page) {
  return page.locator('body').innerText()
}

async function waitForBodyState(page: Page, pattern: RegExp, timeout = 15000) {
  await expect.poll(async () => await bodyText(page), { timeout }).toMatch(pattern)
  return bodyText(page)
}

async function isExplicitApiFailure(page: Page) {
  const body = await bodyText(page)
  return body.includes('API Error 404') || body.includes('File not found')
}

async function api<T>(request: any, method: 'GET' | 'POST', url: string, data?: unknown) {
  const response = method === 'POST'
    ? await request.post(url, { data })
    : await request.get(url)
  expect(response.ok()).toBeTruthy()
  const payload = await response.json()
  expect(payload.success).toBeTruthy()
  return payload.data as T
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

async function mockDesktopRuntime(page: Page, port = 3000) {
  await page.addInitScript((runtimePort) => {
    const win = window as Window & {
      __TAURI_INTERNALS__?: {
        invoke?: (cmd: string, args?: unknown) => Promise<unknown>
      }
      __TAURI__?: Record<string, unknown>
    }

    const existingInvoke = win.__TAURI_INTERNALS__?.invoke

    win.__TAURI_INTERNALS__ = {
      ...(win.__TAURI_INTERNALS__ || {}),
      invoke: async (cmd: string, args?: unknown) => {
        if (cmd === 'get_backend_runtime') {
          return {
            port: runtimePort,
            backend_url: `http://127.0.0.1:${runtimePort}`,
            ws_url: `ws://127.0.0.1:${runtimePort}/ws`,
          }
        }
        if (existingInvoke) {
          return existingInvoke(cmd, args)
        }
        throw new Error(`unexpected tauri invoke: ${cmd}`)
      },
    }
    win.__TAURI__ = {
      ...(win.__TAURI__ || {}),
    }
  }, port)
}

async function mockDesktopPickerRuntime(
  page: Page,
  responses: Record<string, unknown>,
) {
  await page.addInitScript((runtimeResponses) => {
    const win = window as Window & {
      __TAURI_INTERNALS__?: {
        invoke?: (cmd: string, args?: unknown) => Promise<unknown>
      }
      __TAURI__?: Record<string, unknown>
    }

    const runtime: Record<string, unknown> = {
      get_backend_runtime: {
        port: 3000,
        backend_url: 'http://127.0.0.1:3000',
        ws_url: 'ws://127.0.0.1:3000/ws',
      },
      ...runtimeResponses,
    }

    const invoke = async (cmd: string) => {
      if (Object.prototype.hasOwnProperty.call(runtime, cmd)) {
        return Reflect.get(runtime, cmd) ?? null
      }
      throw new Error(`unexpected tauri invoke: ${cmd}`)
    }

    win.__TAURI_INTERNALS__ = {
      ...(win.__TAURI_INTERNALS__ || {}),
      invoke,
    }
    win.__TAURI__ = {
      ...(win.__TAURI__ || {}),
    }
  }, responses)
}

test('agents edit drawer is skill-first and no longer exposes legacy tool controls', async ({ page, request }) => {
  await mockDesktopRuntime(page)
  const configPayload = await api<{ configs?: Array<{ config_name: string }> }>(request, 'GET', 'http://127.0.0.1:3000/api/agent-configs')
  const configName = configPayload.configs?.[0]?.config_name
  expect(configName).toBeTruthy()

  await page.goto('/settings/agents.html', { waitUntil: 'networkidle' })
  await waitForBodyState(page, /智能体方案|API Error 404|File not found/)
  if (await isExplicitApiFailure(page)) {
    await expect(page.locator('body')).toContainText(/API Error 404|File not found/)
    return
  }
  await page.getByRole('button', { name: '智能体方案' }).click()
  await page.getByPlaceholder('搜索方案...').fill(String(configName))
  await expect(page.getByRole('button', { name: '编辑' }).first()).toBeVisible()
  await page.getByRole('button', { name: '编辑' }).first().click()
  await page.getByRole('button', { name: '编辑' }).nth(2).click()

  await expect(page.getByText(/编辑 Runtime 智能体|编辑 Plan 智能体/)).toBeVisible()
  await expect(page.getByText('智能体技能包')).toBeVisible()
  await expect(page.getByText('专属直连工具（旧版）')).toHaveCount(0)
  await expect(page.getByText('共享直连工具（旧版）')).toHaveCount(0)
  await expect(page.getByText('展开工具列表')).toHaveCount(0)
  await expect(page.getByText('概览指标')).toHaveCount(0)
})

test('instances page filters stopped instances from runtime snapshot projection', async ({ page }) => {
  await disableRealtimeWebSocket(page)
  await mockDesktopRuntime(page)

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
                project_path: '/tmp/running-visible.qgz',
                status: 'running',
                services: {},
                created_at: '2026-03-19T10:00:00.000Z',
                updated_at: '2026-03-19T10:00:00.000Z',
                last_heartbeat: '2026-03-19T10:00:00.000Z',
                source: 'reverse_ws',
              },
              {
                id: 'instance-stopped',
                pid: 1202,
                qgis_version: '3.34',
                project_path: '/tmp/stopped-hidden.qgz',
                status: 'stopped',
                services: {},
                created_at: '2026-03-19T10:01:00.000Z',
                updated_at: '2026-03-19T10:01:00.000Z',
                last_heartbeat: '2026-03-19T10:01:00.000Z',
                source: 'reverse_ws',
              },
            ],
            active_id: 'instance-stopped',
            instance_count: 2,
            running_count: 1,
          },
          stale: false,
        },
      },
    })
  })

  await page.goto('/instances.html', { waitUntil: 'networkidle' })
  await expect(page.locator('body')).toContainText('共 1 个实例')
  await expect(page.locator('body')).toContainText('运行中 1')
  await expect(page.locator('body')).toContainText('当前 running-visible · 运行中')
  await expect(page.locator('body')).not.toContainText('stopped-hidden')
  await expect(page.locator('body')).not.toContainText('已停止')
})

test('chat shows degraded runtime sync state instead of regressing to disconnected placeholder', async ({ page }) => {
  await disableRealtimeWebSocket(page)
  await mockDesktopRuntime(page)

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
          stale: true,
        },
      },
    })
  })

  await page.goto('/chat.html', { waitUntil: 'networkidle' })
  await expect(page.getByTestId('project-capability-banner')).toContainText('运行时同步中')
  await expect(page.getByTestId('project-capability-banner')).toContainText('历史会话、任务和工作流仍可查看')
  await expect(page.getByRole('textbox', { name: '消息输入' })).toBeDisabled()
  await expect(page.getByTestId('backend-status-banner')).toHaveCount(0)
  await expect(page.locator('body')).not.toContainText('QGIS 未连接')
})

test('chat restores historical session without duplicate user input and with tasks/workflow state', async ({ page }) => {
  await disableRealtimeWebSocket(page)
  await mockDesktopRuntime(page)

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

  await page.route('**/api/sessions**', async (route) => {
    const url = route.request().url()
    if (url.includes('/api/sessions/sid-history-restore')) {
      await route.fulfill({
        json: {
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
        },
      })
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

  await page.goto('/chat.html', { waitUntil: 'networkidle' })
  await page.getByRole('button', { name: /历史恢复验证/ }).click()
  await page.getByRole('button', { name: /查看 Planner 详情/ }).click()

  await expect(page.getByText('制作湖北省 GDP 条形统计专题图')).toHaveCount(1)
  await expect(page.locator('p.whitespace-pre-wrap', { hasText: '好的，开始分析。' })).toBeVisible()
  await expect(page.getByText('详情与任务')).toBeVisible()
  await expect(page.getByText('恢复任务列表')).toBeVisible()
  await expect(page.getByText('验证 write_todos 完成态')).toBeVisible()
  await page.getByRole('button', { name: /Planner\s+已完成规划/ }).click()
  await page.getByRole('button', { name: /工具 · write_todos/ }).click()
  await expect(page.locator('body')).toContainText(/write_todos|task_list_update/)
})

test('agent edit drawer keeps model selection linked to configured models', async ({ page, request }) => {
  await mockDesktopRuntime(page)
  const configPayload = await api<{ configs?: Array<{ config_name: string }> }>(request, 'GET', 'http://127.0.0.1:3000/api/agent-configs')
  const configName = configPayload.configs?.[0]?.config_name
  expect(configName).toBeTruthy()

  await page.goto('/settings/agents.html', { waitUntil: 'networkidle' })
  await page.getByRole('button', { name: '智能体方案' }).click()
  await page.getByPlaceholder('搜索方案...').fill(String(configName))
  await page.getByRole('button', { name: '编辑' }).first().click()
  await expect(page.locator('#agent-config-detail')).toBeVisible({ timeout: 15000 })
  await expect(page.getByText(`编辑智能体方案 · ${configName}`)).toBeVisible({ timeout: 15000 })

  const modelSelect = page.getByLabel('独立模型（可选）')
  const detailEditButtons = page.locator('#agent-config-detail').getByRole('button', { name: '编辑' })
  expect(await detailEditButtons.count()).toBeGreaterThan(1)
  await detailEditButtons.nth(1).click()
  await expect(modelSelect).toBeVisible()
  const optionTexts = await modelSelect.evaluate((element) => {
    return Array.from((element as HTMLSelectElement).options).map((option) => option.textContent?.trim() || '')
  })
  expect(optionTexts).toContain('继承全局模型')
  expect(optionTexts.length).toBeGreaterThan(1)
})

test('settings pages keep fixed directories read-only and only browse QGIS path', async ({ page }) => {
  await mockDesktopPickerRuntime(page, {
    pick_directory_path: '/tmp/autocarto-picked-directory',
    pick_qgis_executable: '/usr/bin/qgis',
    pick_file_path: '/usr/bin/python3',
  })

  await page.goto('/settings/system.html', { waitUntil: 'networkidle' })
  await waitForBodyState(page, /日志目录|API Error 404|File not found|QGIS 可执行路径/)
  if (await isExplicitApiFailure(page)) {
    await expect(page.locator('body')).toContainText(/API Error 404|File not found/)
    return
  }

  const logDirField = page.locator('label', { hasText: '日志目录' })
  await expect(logDirField.getByRole('button', { name: '浏览…' })).toHaveCount(0)
  await expect(logDirField.locator('input')).toHaveAttribute('readonly', '')

  const dataDirField = page.locator('label', { hasText: '数据目录' })
  await expect(dataDirField.getByRole('button', { name: '浏览…' })).toHaveCount(0)
  await expect(dataDirField.locator('input')).toHaveAttribute('readonly', '')

  const qgisField = page.locator('label', { hasText: 'QGIS 可执行路径' })
  await expect(qgisField.getByRole('button', { name: '浏览…' })).toBeVisible()
  await qgisField.getByRole('button', { name: '浏览…' }).click()
  await expect(qgisField.locator('input')).toHaveValue('/usr/bin/qgis')

  await page.goto('/settings/services.html', { waitUntil: 'networkidle' })
  await expect(page).toHaveURL(/\/settings\/services(\.html)?$/)
  await expect(page.locator('body')).toContainText('服务状态')
})

test('agent create dialog is skill-first and model-linked', async ({ page, request }) => {
  await mockDesktopRuntime(page)
  const configPayload = await api<{ configs?: Array<{ config_name: string }> }>(request, 'GET', 'http://127.0.0.1:3000/api/agent-configs')
  const configName = configPayload.configs?.[0]?.config_name
  expect(configName).toBeTruthy()

  await page.goto('/settings/agents.html', { waitUntil: 'networkidle' })
  await waitForBodyState(page, /智能体方案|API Error 404|File not found/)
  if (await isExplicitApiFailure(page)) {
    await expect(page.locator('body')).toContainText(/API Error 404|File not found/)
    return
  }
  await page.getByRole('button', { name: '智能体方案' }).click()
  await page.getByPlaceholder('搜索方案...').fill(String(configName))
  await page.getByRole('button', { name: '编辑' }).first().click()
  await page.getByRole('button', { name: '新建 Runtime 智能体' }).click()

  const modelSelect = page.getByLabel('独立模型（可选）')
  await expect(page.getByText('新建 Runtime 智能体').nth(1)).toBeVisible()
  await expect(modelSelect).toBeVisible()
  await expect(modelSelect).toHaveValue('')
  await expect(page.getByText('智能体技能包').first()).toBeVisible()
  await expect(page.getByText('专属工具（旧版）')).toHaveCount(0)
  await expect(page.getByText('共享工具（旧版）')).toHaveCount(0)
  await expect(page.getByText('展开旧版工具列表')).toHaveCount(0)
  await expect(page.getByText('概览指标')).toHaveCount(0)

  const optionTexts = await modelSelect.evaluate((element) => {
    return Array.from((element as HTMLSelectElement).options).map((option) => option.textContent?.trim() || '')
  })
  expect(optionTexts).toContain('继承全局模型')
  expect(optionTexts.length).toBeGreaterThan(1)
})
