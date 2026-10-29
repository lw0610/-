import { expect, test } from '@playwright/test'

import { mockDesktopRuntime } from './helpers/desktopRuntime'

test('instances page hides stale startup error once authoritative desktop runtime is available', async ({
  page,
}) => {
  await mockDesktopRuntime(page, {
    startupError:
      '启动后端失败: 解析 discovery descriptor 响应失败: invalid type: integer `2`, expected struct BackendRuntimeEnvelope at line 1 column 1',
  })

  await page.goto('/instances.html', { waitUntil: 'networkidle' })

  await expect(page.getByRole('heading', { name: 'GIS 实例管理', exact: true })).toBeVisible()
  await expect(page.locator('body')).toContainText(/新建 \/ 打开工程|暂无 QGIS 实例/)
  await expect(page.locator('body')).not.toContainText('后端启动失败')
  await expect(page.locator('body')).not.toContainText('解析 discovery descriptor 响应失败')
})
