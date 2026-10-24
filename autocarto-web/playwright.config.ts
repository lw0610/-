import { defineConfig } from '@playwright/test'

const liveE2E = process.env.AUTOCARTO_LIVE_E2E === '1'
const reuseExistingServer = process.env.AUTOCARTO_PLAYWRIGHT_REUSE_EXISTING === '1'
const useDevServer = process.env.AUTOCARTO_PLAYWRIGHT_USE_DEV_SERVER === '1'
const frontendPort = Number(process.env.AUTOCARTO_PLAYWRIGHT_FRONTEND_PORT || '50142')
const backendPort = Number(process.env.AUTOCARTO_TEST_RUNTIME_PORT || '50141')

export default defineConfig({
  testDir: '.',
  testMatch: ['tests/e2e/**/*.spec.ts', 'e2e/**/*.spec.ts'],
  timeout: 30_000,
  retries: 0,
  workers: 1,
  projects: [
    {
      name: 'chromium',
    },
  ],
  use: {
    baseURL: process.env.PLAYWRIGHT_BASE_URL || `http://127.0.0.1:${frontendPort}`,
    channel: 'chrome',
    trace: 'retain-on-failure',
  },
  webServer: [
    {
      command: liveE2E
        ? 'python ./scripts/playwright_live_backend_supervisor.py'
        : 'python3 ./scripts/playwright_check_server.py backend',
      port: liveE2E ? 50002 : backendPort,
      reuseExistingServer,
      timeout: 120_000,
    },
    {
      command: 'python3 ./scripts/playwright_check_server.py frontend',
      port: frontendPort,
      reuseExistingServer: false,
      timeout: useDevServer ? 300_000 : 120_000,
    },
  ],
})
