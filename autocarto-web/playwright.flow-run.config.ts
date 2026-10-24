import { defineConfig } from '@playwright/test'
export default defineConfig({
  testDir: './tests/e2e', testMatch: 'flow-run.spec.ts', workers: 1, timeout: 120_000,
  use: { channel: 'chrome', baseURL: 'http://127.0.0.1:50132', viewport: { width: 1440, height: 1100 }, trace: 'retain-on-failure' },
  webServer: { command: 'node node_modules/next/dist/bin/next dev --hostname 127.0.0.1 --port 50132', url: 'http://127.0.0.1:50132/chat', reuseExistingServer: false, timeout: 120_000 },
})
