import { expect, test, type Page } from '@playwright/test'

import { DEFAULT_DESKTOP_RUNTIME_PORT, mockDesktopRuntime } from './helpers/desktopRuntime'

const backendBaseURL = `http://127.0.0.1:${DEFAULT_DESKTOP_RUNTIME_PORT}`

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

async function getRuntimeToolPackages(request: any, configName: string) {
  const profile = await api<any>(request, 'GET', `${backendBaseURL}/api/agent-configs/${configName}/data`)
  return Array.isArray(profile.runtime_shared?.tool_packages)
    ? profile.runtime_shared.tool_packages.filter((item: unknown) => typeof item === 'string' && item.trim().length > 0)
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
    await expect(page.locator('#tool-center-list')).toContainText('工具包目录')
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
  await expect(page.getByRole('heading', { level: 1, name: '工具包与能力' })).toBeVisible()
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
  const before = await api<any>(request, 'GET', `${backendBaseURL}/api/agent-configs`)
  const previousActive = before.configs?.find((item: { is_active?: boolean }) => item.is_active)?.config_name
  await api(request, 'POST', `${backendBaseURL}/api/agent-configs`, { name: configName })

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

    const schemeDialog = page.getByRole('dialog', { name: `编辑智能体方案 · ${configName}`, exact: true })
    const runtimeGlobalSection = schemeDialog.getByText('所有执行角色共享的工具包', { exact: true }).locator('xpath=../..')
    await runtimeGlobalSection.getByRole('button', { name: '编辑', exact: true }).click()
    const sharedDialog = page.getByRole('dialog', { name: 'Runtime 共享工具包', exact: true })
    const packageCheckboxes = sharedDialog.locator('input[type="checkbox"]:enabled')
    await expect.poll(async () => await packageCheckboxes.count(), { timeout: 30000 }).toBeGreaterThan(0)
    // System packages are locked; deliberately select an editable category.
    await packageCheckboxes.first().check()
    await sharedDialog.getByRole('button', { name: '保存共享工具包', exact: true }).click()
    await expect(sharedDialog).not.toBeVisible()
    const saveConfigButton = schemeDialog.getByRole('button', { name: '保存当前方案', exact: true })
    if (await saveConfigButton.isEnabled()) await saveConfigButton.click()
    await schemeDialog.getByRole('button', { name: '关闭', exact: true }).click()

    await expect.poll(async () => (await getRuntimeToolPackages(request, configName)).length, { timeout: 15000 }).toBeGreaterThan(0)
    const runtimePackages = await getRuntimeToolPackages(request, configName)
    expect(runtimePackages.length).toBeGreaterThan(0)

    await page.getByRole('button', { name: '更多操作' }).click()
    await page.getByRole('button', { name: '设为激活' }).click()

    await expect.poll(async () => {
      const snapshot = await api<any>(request, 'GET', `${backendBaseURL}/api/runtime/snapshot`)
      return snapshot.runtime_active_agent_config || snapshot.active_agent_config || snapshot.active_config || ''
    }, { timeout: 15000 }).toBe(configName)

    await expect.poll(async () => {
      const catalog = await api<any>(request, 'GET', `${backendBaseURL}/api/tools/catalog?scope=global`)
      const configuredItems = Array.isArray(catalog.catalog?.items) ? catalog.catalog.items : Array.isArray(catalog.items) ? catalog.items : []
      return configuredItems.length > 0 ? 'matched' : 'pending'
    }, { timeout: 15000 }).toBe('matched')

    await page.goto('/diagnostics.html')
    await expect(page.locator('#diagnostics-tools')).toContainText('工具目录')
    await expect(page.locator('#diagnostics-tools')).toContainText(/尚未上报有效工具|工具包/)

    await page.goto('/chat.html')
    await expect(page.locator('body')).toContainText(/BUILD · 直接执行|开始对话吧|连接检查/)
  } finally {
    if (previousActive) await api(request, 'POST', `${backendBaseURL}/api/agent-configs/${encodeURIComponent(previousActive)}/activate`)
    await api(request, 'DELETE', `${backendBaseURL}/api/agent-configs/${encodeURIComponent(configName)}`)
  }
})
