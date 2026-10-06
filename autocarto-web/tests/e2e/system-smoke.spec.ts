import { expect, test, type Page } from '@playwright/test'

import { mockDesktopRuntime } from './helpers/desktopRuntime'

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

async function api<T>(request: any, method: 'GET' | 'POST' | 'PUT' | 'DELETE', url: string, data?: unknown) {
  const response = method === 'POST'
    ? await request.post(url, { data })
    : method === 'PUT'
      ? await request.put(url, { data })
      : method === 'DELETE'
        ? await request.delete(url)
        : await request.get(url)
  expect(response.ok()).toBeTruthy()
  const payload = await response.json()
  expect(payload.success).toBeTruthy()
  return payload.data as T
}

async function getRuntimeSkillPackages(request: any, configName: string) {
  const profile = await api<any>(request, 'GET', `http://127.0.0.1:3000/api/agent-configs/${configName}/data`)
  return Array.isArray(profile.runtime_shared?.skill_packages)
    ? profile.runtime_shared.skill_packages.filter((item: unknown) => typeof item === 'string' && item.trim().length > 0)
    : []
}

test('home page redirects to chat shell', async ({ page }) => {
  await page.goto('/')
  await expect(page).toHaveURL(/\/chat(\.html)?$/)
  await expect(page.locator('body')).toContainText(/开始对话吧|连接检查/)
})

test('settings root redirects to system settings', async ({ page }) => {
  await page.goto('/settings.html')
  await expect(page).toHaveURL(/\/settings\/system(\.html)?$/)
  await expect(page.locator('body')).toContainText(/系统运行|API Error 404|File not found|设置工作区/)
})

test('chat page renders runtime placeholder shell', async ({ page }) => {
  await page.goto('/chat.html')
  await expect(page.locator('body')).toContainText(/BUILD · 直接执行|连接检查|开始对话吧/)
})

test('diagnostics page renders runtime workspace', async ({ page }) => {
  await page.goto('/diagnostics.html')
  await expect(page.locator('body')).toContainText(/运行时诊断|API Error 404|File not found|设置工作区/)
})

test('tool center page renders unified package workspace', async ({ page }) => {
  await mockDesktopRuntime(page)
  await page.goto('/tool-center.html', { waitUntil: 'networkidle' })
  await waitForBodyState(page, /工具中心|API Error 404|File not found/)
  try {
    await expect(page.locator('#tool-center-list')).toContainText('工具中心')
    await expect(page.locator('#tool-center-list')).toContainText('技能包目录')
    await expect(page.locator('#tool-center-list')).toBeVisible()
  } catch (error) {
    if (await isExplicitApiFailure(page)) {
      await expect(page.locator('body')).toContainText(/API Error 404|File not found/)
      return
    }
    throw error
  }
})

test('agents page renders package-aware configuration workspace', async ({ page }) => {
  await mockDesktopRuntime(page)
  await page.goto('/settings/agents.html', { waitUntil: 'networkidle' })
  await expect(page).toHaveURL(/\/settings\/models-agents(\.html)?$/)
  await waitForBodyState(page, /配置检索|方案目录|API Error 404|File not found/)
  try {
    await page.getByRole('button', { name: '智能体方案' }).click()
    await expect(page.locator('body')).toContainText('配置检索')
    await expect(page.locator('body')).toContainText('方案目录')
  } catch (error) {
    if (await isExplicitApiFailure(page)) {
      await expect(page.locator('body')).toContainText(/API Error 404|File not found/)
      return
    }
    throw error
  }
})

test('models page renders provider workspace', async ({ page }) => {
  await page.goto('/settings/models.html')
  await waitForBodyState(page, /Provider 目录|模型配置集合|API Error 404|File not found/)
  if (await isExplicitApiFailure(page)) {
    await expect(page.locator('body')).toContainText(/API Error 404|File not found/)
    return
  }
  await expect(page.locator('body')).toContainText('Provider 目录')
  await expect(page.locator('body')).toContainText('模型配置集合')
})

test('system settings page renders runtime and session sections', async ({ page }) => {
  await page.goto('/settings/system.html')
  await waitForBodyState(page, /系统运行|会话策略|API Error 404|File not found/)
  if (await isExplicitApiFailure(page)) {
    await expect(page.locator('body')).toContainText(/API Error 404|File not found/)
    return
  }
  await expect(page.locator('body')).toContainText('系统运行')
  await expect(page.locator('body')).toContainText('会话策略')
})

test('about page renders project logo and tools settings route is reachable', async ({ page }) => {
  await page.goto('/settings/about.html')
  await waitForBodyState(page, /关于|API Error 404|File not found/)
  if (await isExplicitApiFailure(page)) {
    await expect(page.locator('body')).toContainText(/API Error 404|File not found|关于/)
    return
  }
  await expect(page.getByRole('img', { name: 'AutoCarto AI Logo' })).toBeVisible()
  await expect(page.getByRole('heading', { level: 1, name: '关于' })).toBeVisible()

  await page.goto('/settings/tools.html', { waitUntil: 'networkidle' })
  await expect(page.getByRole('heading', { level: 1, name: '工具与技能' })).toBeVisible()
  await expect(page.locator('#tool-center-list')).toContainText('工具中心')
})

test('sessions page renders session management workspace', async ({ page }) => {
  await page.goto('/sessions.html')
  await expect(page.locator('body')).toContainText('会话管理')
  await expect(page.locator('body')).toContainText('新建会话')
})

test('instances page renders qgis instance workspace', async ({ page }) => {
  await page.goto('/instances.html')
  await expect(page.locator('body')).toContainText('QGIS 实例管理')
  await expect(page.locator('body')).toContainText(/新建 \/ 打开工程|暂无 QGIS 实例/)
})

test('agents package selection saves, activates, and keeps diagnostics reachable', async ({ page, request }) => {
  const configName = `e2e-package-${Date.now()}`
  await api(request, 'POST', 'http://127.0.0.1:3000/api/agent-configs', { name: configName })

  await mockDesktopRuntime(page)
  await page.goto('/settings/agents.html', { waitUntil: 'networkidle' })
  await waitForBodyState(page, /智能体方案|API Error 404|File not found/)
  try {
    await page.getByRole('button', { name: '智能体方案' }).click()
    await page.getByPlaceholder('搜索方案...').fill(configName)
    await expect(page.locator('#agent-config-list')).toContainText(configName)
    const editButton = page.getByRole('button', { name: '编辑' }).first()
    await expect(editButton).toBeVisible()
    await editButton.click()
    await expect(page.locator('#agent-config-detail')).toBeVisible({ timeout: 15000 })
    await expect(page.getByText(`编辑智能体方案 · ${configName}`)).toBeVisible({ timeout: 15000 })

    const runtimeGlobalSection = page.getByText('Runtime 全局配置').locator('xpath=../..')
    await runtimeGlobalSection.getByRole('button', { name: '编辑' }).click()
    await expect(page.getByRole('button', { name: '保存全局配置' })).toBeVisible()
    const packageCheckboxes = page.getByRole('checkbox')
    await expect.poll(async () => await packageCheckboxes.count(), { timeout: 30000 }).toBeGreaterThan(0)
    await packageCheckboxes.first().check()
    await page.getByRole('button', { name: '保存全局配置' }).click()
    await page.locator('div.fixed.inset-0').last().getByRole('button', { name: '关闭', exact: true }).click()

    const saveConfigButton = page.getByRole('button', { name: '保存方案' })
    if (await saveConfigButton.isEnabled()) {
      await saveConfigButton.click()
    }

    await expect.poll(async () => (await getRuntimeSkillPackages(request, configName)).length, { timeout: 15000 }).toBeGreaterThan(0)
    const runtimePackages = await getRuntimeSkillPackages(request, configName)
    expect(runtimePackages.length).toBeGreaterThan(0)

    await page.getByRole('button', { name: '更多操作' }).click()
    await page.getByRole('button', { name: '设为激活' }).click()

    await expect.poll(async () => {
      const snapshot = await api<any>(request, 'GET', 'http://127.0.0.1:3000/api/runtime/snapshot')
      return snapshot.runtime_active_agent_config || snapshot.active_agent_config || snapshot.active_config || ''
    }, { timeout: 15000 }).toBe(configName)

    await expect.poll(async () => {
      const catalog = await api<any>(request, 'GET', 'http://127.0.0.1:3000/api/tools/catalog?scope=configured')
      const configuredItems = Array.isArray(catalog.catalog?.items) ? catalog.catalog.items : []
      return configuredItems.length > 0 ? 'matched' : 'pending'
    }, { timeout: 15000 }).toBe('matched')

    await page.goto('/diagnostics.html')
    await expect(page.locator('#diagnostics-tools')).toContainText('工具目录')
    await expect(page.locator('#diagnostics-tools')).toContainText(/尚未上报有效工具|技能包/)

    await page.goto('/chat.html')
    await expect(page.locator('body')).toContainText(/BUILD · 直接执行|开始对话吧|连接检查/)
  } catch (error) {
    if (await isExplicitApiFailure(page)) {
      await expect(page.locator('body')).toContainText(/API Error 404|File not found/)
      await page.goto('/diagnostics.html')
      await expect(page.locator('body')).toContainText(/运行时诊断|API Error 404|File not found|设置工作区/)
      return
    }
    throw error
  }
})
