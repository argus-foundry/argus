import { test, expect, Page } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const shell = readFileSync(resolve(__dirname, '../../internal/api/static/decide.html'), 'utf8');
const origin = 'http://decide.test';
const item = {
  id: 40, updated_at: 'revision',
  subject: 'Review https://example.com/subject',
  body: 'See https://example.com/body).\n\nKeep `https://example.com/code` literal. <img src=x onerror=alert(1)> javascript:alert(1) data:text/html,payload',
  structured: {
    options: [{ id: 'review', label: 'Review references',
      summary: 'Summary http://example.com/summary,',
      upside: '**Read https://example.com/upside**',
      downside: 'Check https://example.com/downside.' }],
    recommended: 'review', reason: 'Compare https://example.com/reason',
  },
};

async function fixture(page: Page) {
  const requests: string[] = [];
  page.on('request', request => requests.push(request.url()));
  await page.route('**/*', async route => {
    const url = new URL(route.request().url());
    if (url.origin !== origin) throw new Error(`Unexpected external request: ${url}`);
    if (url.pathname === '/decide') {
      // Observe the restored preference immediately after the blocking head script.
      const html = shell.replace('</script>', '</script><script>globalThis.themeBeforeBody=document.documentElement.dataset.theme;</script>');
      await route.fulfill({ contentType: 'text/html', body: html });
    } else if (url.pathname === '/api/hera/board/decision-session') {
      expect(route.request().postDataJSON()).toEqual({ ticket: 'launch-secret' });
      await route.fulfill({ json: { role_id: 7, session: 'session-secret' } });
    } else if (url.pathname === '/api/hera/roles/7/board/decisions') {
      expect(route.request().headers().authorization).toBe('Bearer session-secret');
      await route.fulfill({ json: { items: [item] } });
    } else throw new Error(`Unexpected request: ${url}`);
  });
  await page.addInitScript(() => {
    const original = globalThis.fetch;
    globalThis.fetch = (...args) => {
      if (location.hash) throw Error('Launch fragment survived until fetch');
      return original(...args);
    };
  });
  return requests;
}

test('phone themes and all decision references preserve security boundaries', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const requests = await fixture(page);
  await page.goto(`${origin}/decide?role=7#t=launch-secret`);
  await expect(page.locator('#decision-40')).toBeVisible();
  await expect(page).toHaveURL(`${origin}/decide?role=7`);
  for (const field of ['subject', 'body', 'summary', 'upside', 'downside', 'reason']) {
    const link = page.locator(`a[href$="/${field}"]`);
    await expect(link).toHaveCount(1);
    await expect(link).toHaveAttribute('target', '_blank');
    await expect(link).toHaveAttribute('rel', 'noopener noreferrer');
  }
  await expect(page.locator('a')).toHaveCount(6);
  await expect(page.locator('code')).toHaveText('https://example.com/code');
  await expect(page.locator('img, svg')).toHaveCount(0);
  await expect(page.locator('.situation')).toContainText('<img src=x onerror=alert(1)>');
  for (const mode of ['light', 'dark']) {
    await page.getByLabel('Theme').selectOption(mode);
    await expect(page.locator('html')).toHaveCSS('color-scheme', mode);
    await page.reload();
    await expect(page.locator('#decision-40')).toBeVisible();
    await expect(page.getByLabel('Theme')).toHaveValue(mode);
    expect(await page.evaluate(() => (globalThis as any).themeBeforeBody)).toBe(mode);
    for (const width of [320, 390]) {
      await page.setViewportSize({ width, height: 844 });
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    }
    await page.screenshot({ path: test.info().outputPath(`decide-${mode}.png`), fullPage: true });
  }
  const values = await page.evaluate(() => Object.values(localStorage));
  expect(values.join(' ')).not.toContain('launch-secret');
  expect(values.join(' ')).not.toContain('session-secret');
  expect(requests.every(url => new URL(url).origin === origin)).toBe(true);
});

test('System follows live color scheme changes', async ({ page }) => {
  await fixture(page);
  await page.emulateMedia({ colorScheme: 'light' });
  await page.goto(`${origin}/decide`);
  await expect(page.getByLabel('Theme')).toHaveValue('system');
  await expect(page.locator('html')).toHaveCSS('color-scheme', 'light');
  await page.emulateMedia({ colorScheme: 'dark' });
  await expect(page.locator('html')).toHaveCSS('color-scheme', 'dark');
  await page.getByLabel('Theme').selectOption('light');
  await expect(page.locator('html')).toHaveCSS('color-scheme', 'light');
  await page.getByLabel('Theme').selectOption('system');
  await expect(page.locator('html')).toHaveCSS('color-scheme', 'dark');
});

test('throwing localStorage still renders decisions and permits theme changes', async ({ page }) => {
  await fixture(page);
  await page.addInitScript(() => {
    Object.defineProperty(globalThis, 'localStorage', { get() { throw Error('Storage denied'); } });
  });
  await page.goto(`${origin}/decide?role=7#t=launch-secret`);
  await expect(page.locator('#decision-40')).toBeVisible();
  await expect(page.getByLabel('Theme')).toHaveValue('system');
  for (const mode of ['dark', 'light']) {
    await page.getByLabel('Theme').selectOption(mode);
    await expect(page.locator('html')).toHaveCSS('color-scheme', mode);
  }
});
