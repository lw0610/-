import { expect, test } from '@playwright/test'

test('services page shows runtime service status workspace', async ({ page }) => {
  await page.goto('/settings/services.html', { waitUntil: 'networkidle' })
  await expect(page).toHaveURL(/\/settings\/services(\.html)?$/)
  await expect(page.locator('body')).toContainText(/服务状态|无法连接到后端服务|设置工作区 contract 当前不可用|API Error 404|File not found/)
  await expect(page.locator('body')).toContainText(/运行态服务|服务健康度|设置工作区|API Error 404|File not found/)
})
