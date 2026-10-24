import { defineConfig } from '@playwright/test'
export default defineConfig({
  testDir:'./tests/e2e',testMatch:'agent-profile-v2.spec.ts',workers:1,timeout:90_000,
  use:{channel:'chrome',baseURL:'http://127.0.0.1:50131',viewport:{width:1440,height:1000},trace:'retain-on-failure'},
  webServer:{command:'node node_modules/next/dist/bin/next dev --hostname 127.0.0.1 --port 50131',url:'http://127.0.0.1:50131/settings/models-agents',reuseExistingServer:false,timeout:120_000},
})
