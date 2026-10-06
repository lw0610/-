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

async function getPackageEnabledState(request: any, packageId: string, scope?: string, configName?: string) {
  const search = new URLSearchParams()
  if (scope) {
    search.set('scope', scope)
  }
  if (configName) {
    search.set('config_name', configName)
  }
  const query = search.size > 0 ? `?${search.toString()}` : ''
  const response = await request.get(`http://127.0.0.1:3000/api/skill-packages/${packageId}${query}`)
  expect(response.ok()).toBeTruthy()
  const payload = await response.json()
  expect(payload.success).toBeTruthy()
  return Boolean(payload.data?.enabled)
}

async function setPackageEnabledState(request: any, packageId: string, enabled: boolean, scope?: string, configName?: string) {
  const search = new URLSearchParams()
  if (scope) {
    search.set('scope', scope)
  }
  if (configName) {
    search.set('config_name', configName)
  }
  const query = search.size > 0 ? `?${search.toString()}` : ''
  const response = await request.post(`http://127.0.0.1:3000/api/skill-packages/${packageId}/toggle${query}`, {
    data: { enabled },
  })
  expect(response.ok()).toBeTruthy()
  const payload = await response.json()
  expect(payload.success).toBeTruthy()
}

async function readPackageSummaryCount(page: Page) {
  const body = await page.locator('body').innerText()
  const summaryMatch = body.match(/技能包\s*(\d+)\s*个/)
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
  await waitForBodyState(page, /工具中心|API Error 404|File not found/, 20000)
  if (await isExplicitApiFailure(page)) {
    await expect(page.locator('body')).toContainText(/API Error 404|File not found/)
    return
  }
  await expect(page.locator('#tool-center-list')).toContainText('工具中心')
  await expect(page.locator('#tool-center-list')).toContainText('技能包目录')

  await ensurePackageCatalogReady(page)

  const body = await page.locator('body').innerText()
  expect(body.includes('分类包') || body.includes('流程包') || body.includes('知识包')).toBeTruthy()
  const firstPackageButton = page.locator('#tool-center-list button').filter({ hasText: /分类包|流程包|知识包/ }).first()
  await expect(firstPackageButton).toBeVisible()
  await firstPackageButton.click()
  await expect(page.locator('#tool-center-detail-drawer')).toBeVisible()
  await expect(page.locator('#tool-center-detail')).toBeVisible()
})

test('tool center supports filters, detail tabs, and toggle feedback', async ({ page, request }) => {
  test.setTimeout(90_000)
  await mockDesktopRuntime(page)
  await page.goto('/tool-center.html', { waitUntil: 'networkidle' })
  await waitForBodyState(page, /工具中心|API Error 404|File not found/, 20000)
  if (await isExplicitApiFailure(page)) {
    await expect(page.locator('body')).toContainText(/API Error 404|File not found/)
    return
  }
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
  await list.getByPlaceholder('搜索技能包...').fill(packageId)
  await list.locator('select').nth(0).selectOption(packageScope)
  await list.locator('select').nth(1).selectOption('category')
  const sourceOptions = await list.locator('select').nth(3).evaluate((node) =>
    Array.from((node as HTMLSelectElement).options).map((option) => option.value),
  )
  expect(sourceOptions.length).toBeGreaterThan(0)
  expect(sourceOptions).toContain(packageId)
  await list.locator('select').nth(3).selectOption(packageId)
  await expect(list).toContainText(packageId)
  await builtinCanvasButton().click()
  await expect(drawer).toBeVisible()
  await expect(detail).toContainText(packageId)
  await detail.getByRole('button', { name: '工具' }).click()
  await expect(detail).toContainText('canvas_zoom_to_extent')
  await detail.getByRole('button', { name: 'Markdown' }).click()
  await expect(detail).toContainText('画布缩放')
  await drawer.getByRole('button', { name: '关闭' }).click()
  await expect(drawer).not.toBeVisible()
  await list.locator('select').nth(0).selectOption(packageScope)
  await list.locator('select').nth(1).selectOption('all')
  await list.locator('select').nth(2).selectOption('all')
  await list.locator('select').nth(3).selectOption('all')
  await list.locator('select').nth(4).selectOption('all')
  await list.getByPlaceholder('搜索技能包...').fill('accessibility-service-area')
  await list.getByRole('button', { name: /accessibility-service-area/ }).click()
  await expect(drawer).toBeVisible()
  await detail.locator('button').filter({ hasText: /^引用$/ }).first().click()
  await expect(detail).toContainText('层级：')
  await expect(detail).toContainText('来源：')
  await drawer.getByRole('button', { name: '关闭' }).click()
  await expect(drawer).not.toBeVisible()
  await list.getByPlaceholder('搜索技能包...').fill(packageId)
  await builtinCanvasButton().click()
  await expect(drawer).toBeVisible()
  const originalEnabled = await getPackageEnabledState(request, packageId, packageScope)
  const firstToggleLabel = originalEnabled ? '停用技能包' : '启用技能包'
  const secondToggleLabel = originalEnabled ? '启用技能包' : '停用技能包'
  await drawer.getByRole('button', { name: firstToggleLabel }).click()
  await expect.poll(async () => getPackageEnabledState(request, packageId, packageScope)).toBe(!originalEnabled)
  await drawer.getByRole('button', { name: '关闭', exact: true }).click()
  await expect(drawer).not.toBeVisible()
  await builtinCanvasButton().click()
  await expect(drawer).toBeVisible()
  await expect(drawer.getByRole('button', { name: '处理中...' })).not.toBeVisible({ timeout: 15000 })
  await expect(drawer.getByRole('button', { name: secondToggleLabel })).toBeVisible({ timeout: 30000 })
  await setPackageEnabledState(request, packageId, originalEnabled, packageScope)
  await expect.poll(async () => getPackageEnabledState(request, packageId, packageScope)).toBe(originalEnabled)
})
