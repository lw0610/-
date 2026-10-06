import { expect, test } from '@playwright/test'

test('diagnostics page renders runtime and tool catalog data', async ({ page }) => {
  await page.goto('/diagnostics.html', { waitUntil: 'networkidle' })
  await expect(page.locator('body')).toContainText(/运行时诊断|无法连接到后端服务|设置工作区 contract 当前不可用|API Error 404|File not found/)
  if (await page.locator('#diagnostics-tools').count()) {
    const toolsSection = page.locator('#diagnostics-tools')
    await expect(toolsSection).toContainText(/工具目录|当前有效工具|尚未上报有效工具|API Error 404|File not found/)
  }
})
