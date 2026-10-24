import { expect, test, type APIRequestContext, type Page } from '@playwright/test'
import { DEFAULT_DESKTOP_RUNTIME_PORT, mockDesktopRuntime } from './helpers/desktopRuntime'

const backendBaseURL = `http://127.0.0.1:${DEFAULT_DESKTOP_RUNTIME_PORT}`

async function bodyText(page: Page) {
  return page.locator('body').innerText()
}

async function waitForBodyState(page: Page, pattern: RegExp, timeout = 15000) {
  await expect.poll(async () => await bodyText(page), { timeout }).toMatch(pattern)
  return bodyText(page)
}

async function getPackageEnabledState(request: APIRequestContext, packageId: string, scope?: string, configName?: string) {
  const search = new URLSearchParams()
  if (scope) {
    search.set('scope', scope)
  }
  if (configName) {
    search.set('config_name', configName)
  }
  const query = search.size > 0 ? `?${search.toString()}` : ''
  const response = await request.get(`${backendBaseURL}/api/skill-packages/${packageId}${query}`)
  expect(response.ok()).toBeTruthy()
  const payload = await response.json()
  expect(payload.success).toBeTruthy()
  return Boolean(payload.data?.enabled)
}

async function setPackageEnabledState(request: APIRequestContext, packageId: string, enabled: boolean, scope?: string, configName?: string) {
  const search = new URLSearchParams()
  if (scope) {
    search.set('scope', scope)
  }
  if (configName) {
    search.set('config_name', configName)
  }
  const query = search.size > 0 ? `?${search.toString()}` : ''
  const response = await request.post(`${backendBaseURL}/api/skill-packages/${packageId}/toggle${query}`, {
    data: { enabled },
  })
  expect(response.ok()).toBeTruthy()
  const payload = await response.json()
  expect(payload.success).toBeTruthy()
}

async function readPackageSummaryCount(page: Page) {
  const body = await page.locator('body').innerText()
  const summaryMatch = body.match(/工具包\s*(\d+)\s*个/)
  return Number(summaryMatch?.[1] ?? '0')
}

async function ensurePackageCatalogReady(page: Page) {
  for (let attempt = 0; attempt < 3; attempt += 1) {
    try {
      await expect
        .poll(async () => readPackageSummaryCount(page), { timeout: 20000 })
        .toBeGreaterThan(0)
      return
    } catch (error) {
      if (attempt === 2) {
        throw error
      }
      await page.getByRole('button', { name: '刷新' }).click()
    }
  }
}

test('tool center renders package catalog from running backend', async ({ page }) => {
  test.setTimeout(90_000)
  await mockDesktopRuntime(page)
  await page.goto('/tool-center.html', { waitUntil: 'networkidle' })
  await waitForBodyState(page, /工具中心/, 20000)
  await expect(page.locator('#tool-center-list')).toContainText('工具中心')
  await expect(page.locator('#tool-center-list')).toContainText('工具包目录')

  await ensurePackageCatalogReady(page)

  await expect(page.locator('#tool-center-list')).not.toContainText(/分类包|流程包|知识包/)
  const firstPackageButton = page.locator('#tool-center-list button').filter({ hasText: 'canvas-navigation' }).first()
  await expect(firstPackageButton).toBeVisible()
  await firstPackageButton.click()
  await expect(page.locator('#tool-center-detail-drawer')).toBeVisible()
  await expect(page.locator('#tool-center-detail')).toBeVisible()
})

test('tool center supports filters, detail tabs, and toggle feedback', async ({ page, request }) => {
  test.setTimeout(90_000)
  await mockDesktopRuntime(page)
  await page.goto('/tool-center.html', { waitUntil: 'networkidle' })
  await waitForBodyState(page, /工具中心/, 20000)
  await ensurePackageCatalogReady(page)
  const list = page.locator('#tool-center-list')
  const drawer = page.locator('#tool-center-detail-drawer')
  const detail = page.locator('#tool-center-detail')
  const packageId = 'canvas-navigation'
  const packageScope = 'builtin'
  const builtinCanvasButton = () =>
    list
      .locator('button')
      .filter({ hasText: new RegExp(packageId) })
      .filter({ hasText: /内置/ })
      .first()
  await list.getByPlaceholder('搜索工具包...').fill(packageId)
  await list.getByRole('combobox', { name: '工具包范围' }).selectOption(packageScope)
  const sourceFilter = list.getByRole('combobox', { name: '工具来源' })
  const sourceOptions = await sourceFilter.evaluate((node) =>
    Array.from((node as HTMLSelectElement).options).map((option) => option.value),
  )
  expect(sourceOptions).toContain('all')
  const specificSource = sourceOptions.find((source) => source !== 'all')
  expect(specificSource).toBeTruthy()
  await sourceFilter.selectOption(specificSource!)
  await expect(sourceFilter).toHaveValue(specificSource!)
  await sourceFilter.selectOption('all')
  await expect(list).toContainText(packageId)
  await builtinCanvasButton().click()
  await expect(drawer).toBeVisible()
  await expect(detail).toContainText(packageId)
  await detail.getByRole('button', { name: '工具清单', exact: true }).click()
  await expect(detail).toContainText('canvas_zoom_to_extent')
  await detail.getByRole('button', { name: '使用说明', exact: true }).click()
  await expect(detail).toContainText('画布缩放')
  await drawer.getByRole('button', { name: '关闭' }).click()
  await expect(drawer).not.toBeVisible()
  await list.getByRole('combobox', { name: '工具后端' }).selectOption('all')
  await list.getByRole('combobox', { name: '工具来源' }).selectOption('all')
  await list.getByRole('combobox', { name: '启用状态' }).selectOption('all')
  await list.getByPlaceholder('搜索工具包...').fill('vector-selection')
  await list.getByRole('button', { name: /vector-selection/ }).click()
  await expect(drawer).toBeVisible()
  await detail.getByRole('button', { name: '参考资料', exact: true }).click()
  await expect(detail).toContainText('references/')
  await detail.getByRole('button', { name: '引用情况', exact: true }).click()
  await expect(detail).toContainText(/层级：|当前无引用/)
  await drawer.getByRole('button', { name: '关闭' }).click()
  await expect(drawer).not.toBeVisible()
  await list.getByPlaceholder('搜索工具包...').fill(packageId)
  await builtinCanvasButton().click()
  await expect(drawer).toBeVisible()
  const originalEnabled = await getPackageEnabledState(request, packageId, packageScope)
  try {
    const firstToggleLabel = originalEnabled ? '停用工具包' : '启用工具包'
    const secondToggleLabel = originalEnabled ? '启用工具包' : '停用工具包'
    await drawer.getByRole('button', { name: firstToggleLabel }).click()
    await expect.poll(async () => getPackageEnabledState(request, packageId, packageScope)).toBe(!originalEnabled)
    await drawer.getByRole('button', { name: '关闭', exact: true }).click()
    await expect(drawer).not.toBeVisible()
    await builtinCanvasButton().click()
    await expect(drawer).toBeVisible()
    await expect(drawer.getByRole('button', { name: '处理中...' })).not.toBeVisible({ timeout: 15000 })
    await expect(drawer.getByRole('button', { name: secondToggleLabel })).toBeVisible({ timeout: 30000 })
  } finally {
    await setPackageEnabledState(request, packageId, originalEnabled, packageScope)
    await expect.poll(async () => getPackageEnabledState(request, packageId, packageScope)).toBe(originalEnabled)
  }
})
