import { test, expect } from '@playwright/test';
import { resetServer } from './_helpers';

test.beforeEach(async () => { await resetServer(); });

async function login(page) {
  await page.addInitScript(() => localStorage.setItem('argus-token', 'test-token'));
  await page.goto('/');
  await expect(page.locator('#main-app')).toBeVisible();
  await page.locator('.tab[data-tab="settings"]').click();
  await expect(page.locator('#provider-usage-content')).toBeVisible();
}

test('provider usage shows Claude windows and Codex weekly from the cache endpoint', async ({ page }) => {
  let accountRequests = 0;
  await page.route('**/api/accounts', route => {
    accountRequests++;
    return route.fulfill({ json: [] });
  });
  await page.route('**/api/usage', route => route.fulfill({ json: {
    claude: [{ name: 'default', label: 'Personal', usage: {
      five_hour_pct: 0, five_hour_resets_at: new Date(Date.now() + 60_000).toISOString(),
      weekly_pct: 64, weekly_resets_at: new Date(Date.now() + 3_600_000).toISOString(),
    } }],
    codex: { weekly_pct: 72 },
  } }));

  await login(page);
  const usage = page.locator('#provider-usage-content');
  await expect(usage).toContainText('Claude — Personal');
  await expect(usage).toContainText('0% used');
  await expect(usage).toContainText('64% used');
  await expect(usage).toContainText('72% used');
  await expect(usage).toContainText('Reset time unavailable');
  expect(accountRequests).toBe(0);
});

test('usage windows become unavailable when their reset time passes', async ({ page }) => {
  const soon = new Date(Date.now() + 2000).toISOString();
  await page.route('**/api/usage', route => route.fulfill({ json: {
    claude: [{ name: 'default', label: 'Primary', usage: {
      five_hour_pct: 90, five_hour_resets_at: soon,
    } }],
    codex: null,
  } }));

  await login(page);
  const usage = page.locator('#provider-usage-content');
  await expect(usage).toContainText('90% used');
  await expect(usage).toContainText('Unavailable', { timeout: 7000 });
});

test('an older usage response cannot overwrite a newer refresh', async ({ page }) => {
  let requests = 0;
  await page.route('**/api/usage', async route => {
    requests++;
    if (requests === 1) {
      await new Promise(resolve => setTimeout(resolve, 250));
      return route.fulfill({ json: { claude: [], codex: { weekly_pct: 18 } } });
    }
    return route.fulfill({ json: { claude: [], codex: { weekly_pct: 81 } } });
  });

  await login(page);
  await page.evaluate(() => (window as any).loadProviderUsage());
  const usage = page.locator('#provider-usage-content');
  await expect(usage).toContainText('81% used');
  await page.waitForTimeout(350);
  await expect(usage).toContainText('81% used');
  await expect(usage).not.toContainText('18% used');
});

test('logging out stops the provider usage poll', async ({ page }) => {
  await page.route('**/api/usage', route => route.fulfill({ json: { claude: [], codex: null } }));
  await login(page);
  await page.evaluate(() => (window as any).logout());
  const state = await page.evaluate(() => ({
    timerRunning: providerUsageTimer !== null,
    token: localStorage.getItem('argus-token'),
  }));
  expect(state.timerRunning).toBe(false);
  expect(state.token).toBeNull();
});
