import { defineConfig } from '@playwright/test'

const liveE2E = process.env.AUTOCARTO_LIVE_E2E === '1'
const reuseExistingServer = process.env.AUTOCARTO_PLAYWRIGHT_REUSE_EXISTING === '1'
const useDevServer = process.env.AUTOCARTO_PLAYWRIGHT_USE_DEV_SERVER === '1'
const frontendPort = Number(process.env.AUTOCARTO_PLAYWRIGHT_FRONTEND_PORT || '3005')

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
        : 'bash -lc "lsof -ti:3000 | xargs -r kill; export AUTOCARTO_HOME=\"${HOME}/.autocarto\"; cd ../carto_agent && go run ./cmd/agent"',
      port: liveE2E ? 50002 : 3000,
      reuseExistingServer,
      timeout: 120_000,
    },
    {
      command: useDevServer
        ? `bash -lc "lsof -ti:${frontendPort} | xargs -r kill; npx next build && python -m http.server ${frontendPort} -d out"`
        : `bash -lc "lsof -ti:${frontendPort} | xargs -r kill; python -m http.server ${frontendPort} -d out"`,
      port: frontendPort,
      reuseExistingServer: false,
      timeout: useDevServer ? 300_000 : 120_000,
    },
  ],
})
