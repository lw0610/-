import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: '.',
  testMatch: ['tests/e2e/runtime-port-file-ws.spec.ts'],
  timeout: 30_000,
  retries: 0,
  workers: 1,
  projects: [{ name: 'chromium' }],
  use: {
    baseURL: process.env.PLAYWRIGHT_BASE_URL || 'http://127.0.0.1:3005',
    channel: 'chrome',
    trace: 'retain-on-failure',
  },
})
