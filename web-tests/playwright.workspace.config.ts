import { defineConfig, devices } from '@playwright/test';

process.env.ARGUS_TEST_PORT = '17744';
process.env.WORKSPACE_TEST_API = 'http://127.0.0.1:17744';
process.env.WORKSPACE_TEST_URL = 'http://127.0.0.1:17750';

export default defineConfig({
  build: { external: ['**/web-workspace/**'] },
  testDir: './tests/workspace',
  testIgnore: '**/server-security.spec.ts',
  fullyParallel: false,
  workers: 1,
  retries: 0,
  reporter: [['list']],
  timeout: 30_000,
  use: {
    baseURL: process.env.WORKSPACE_TEST_URL,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: [{ name: 'desktop', use: { ...devices['Desktop Chrome'] } }],
  webServer: [
    {
      command: '/tmp/argus-test-server -port 17744 -token test-token',
      port: 17744,
      reuseExistingServer: false,
      timeout: 10_000,
    },
    {
      command: 'node ../web-workspace/dev-server.mjs --port 17750 --api http://127.0.0.1:17744',
      port: 17750,
      reuseExistingServer: false,
      timeout: 10_000,
    },
  ],
});
