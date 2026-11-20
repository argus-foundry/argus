import { defineConfig, devices } from '@playwright/test';

const PORT = Number(process.env.ARGUS_TEST_PORT || 7744);

// The 'iphone' project uses devices['iPhone 14 Pro'], whose default browser is
// WebKit. Playwright's WebKit build is a real app (Playwright.app), so every
// local run bounces an icon in the macOS Dock. Run it only in CI or when
// ARGUS_PW_WEBKIT=1; otherwise the same device emulation runs on Chromium.
const useWebKit = Boolean(process.env.CI) || process.env.ARGUS_PW_WEBKIT === '1';

export default defineConfig({
  testDir: './tests',
  testIgnore: ['**/workspace/**','**/decide-annotations-api.spec.ts'],
  fullyParallel: false, // single backing daemon — keep serial
  workers: 1,
  retries: 0,
  reporter: [['list']],
  timeout: 30_000,
  use: {
    baseURL: `http://127.0.0.1:${PORT}`,
    trace: 'retain-on-failure',
    video: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: [
    {
      name: 'iphone',
      use: useWebKit
        ? { ...devices['iPhone 14 Pro'] }
        : { ...devices['iPhone 14 Pro'], defaultBrowserType: 'chromium' },
    },
    {
      name: 'desktop',
      use: { ...devices['Desktop Chrome'], viewport: { width: 1280, height: 800 } },
    },
  ],
  webServer: process.env.ARGUS_NO_WEBSERVER ? undefined : {
    command: '/tmp/argus-test-server -port ' + PORT + ' -token test-token',
    port: PORT,
    reuseExistingServer: false,
    timeout: 10_000,
  },
});
