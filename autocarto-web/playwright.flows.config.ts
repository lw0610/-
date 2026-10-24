import { defineConfig } from '@playwright/test'
export default defineConfig({
  testDir: './tests/e2e',
  testMatch: 'flow-center.spec.ts',
  workers: 1,
  timeout: 60_000,
  use: {
    channel: 'chrome',
    baseURL: 'http://127.0.0.1:50130',
    trace: 'retain-on-failure',
    viewport: { width: 1440, height: 1000 },
  },
  webServer: {
    command:
      'node node_modules/next/dist/bin/next dev --hostname 127.0.0.1 --port 50130',
    url: 'http://127.0.0.1:50130/flows',
    reuseExistingServer: false,
    timeout: 120_000,
  },
})
