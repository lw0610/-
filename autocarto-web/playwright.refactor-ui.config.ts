import { defineConfig } from '@playwright/test'
process.env.AUTOCARTO_TEST_RUNTIME_PORT = process.env.AUTOCARTO_TEST_RUNTIME_PORT || '50151'
export default defineConfig({
  testDir: './tests/e2e',
  testMatch: 'refactor-ui-live.spec.ts',
  workers: 1,
  outputDir: '/tmp/enio2-t53-playwright-results',
  timeout: 120_000,
  use: { actionTimeout: 15000, channel: 'chrome', baseURL: 'http://127.0.0.1:50152', viewport: { width: 1440, height: 1000 }, trace: 'retain-on-failure' },
  webServer: { command: process.env.AUTOCARTO_REFACTOR_UI_STATIC === '1' ? 'python3 ./scripts/playwright_refactor_ui_static.py 50152' : 'AUTOCARTO_NEXT_DIST_DIR=.next/refactor-ui node node_modules/next/dist/bin/next dev --hostname 127.0.0.1 --port 50152', url: 'http://127.0.0.1:50152/flows', reuseExistingServer: false, timeout: 120_000 },
})
