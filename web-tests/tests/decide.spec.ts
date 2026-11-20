import { test, expect, Page } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const shell = readFileSync(resolve(__dirname, '../../internal/api/static/decide.html'), 'utf8');
const origin = 'http://decide.test';
const item = {
  id: 40, role_id: 7, kind: 'decision', updated_at: 'revision',
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

async function fixture(page: Page, { items = [item], outcome = 'resolved', answerStatus = 200 } = {}) {
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
      await route.fulfill({ json: { initial_role_id: 7, session: 'session-secret' } });
    } else if (url.pathname === '/api/hera/board/decision-navigation') {
      await route.fulfill({ json: {roles:[{role_id:7,parent_role_id:null,orchestrator:'Release',coordinator:'coord',decisions:items.length,fyis:0}]} });
    } else if (url.pathname === '/api/hera/roles/7/board/decide-items') {
      expect(route.request().headers().authorization).toBe('Bearer session-secret');
      await route.fulfill({ json: { items,coordinator:'coord',orchestrator:'Release' } });
    } else if (url.pathname === '/api/hera/roles/7/board/answers') {
      expect(route.request().headers().authorization).toBe('Bearer session-secret');
      const { answers } = route.request().postDataJSON();
      expect(answers.map((answer: { item_id: number }) => answer.item_id)).toEqual([40]);
      await route.fulfill({ status: answerStatus, json: { results: answers.map((answer: { item_id: number }) => ({
        item_id: answer.item_id, outcome,
        ...(outcome === 'conflict' ? { item: { ...item, updated_at: 'revision2' } } : {}),
      })) } });
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
  await page.goto(`${origin}/decide#t=launch-secret`);
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

async function blockClose(page: Page) {
  await page.addInitScript(() => {
    (globalThis as any).closeAttempts = 0;
    window.close = () => { (globalThis as any).closeAttempts++; };
  });
}

for (const outcome of ['resolved', 'already_resolved']) {
  test(`${outcome} submission shows sent confirmation when closing is blocked`, async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.clock.install();
    const requests = await fixture(page, { items: [item, { ...item, id: 41 }], outcome });
    await blockClose(page);
    await page.goto(`${origin}/decide?role=7#t=launch-secret`);
    const mode = outcome === 'resolved' ? 'dark' : 'light';
    await page.getByLabel('Theme').selectOption(mode);
    await page.locator('#decision-40 input[value="review"]').check();
    await page.getByRole('button', { name: 'Submit & close', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Actions sent — you can close this tab' })).toBeVisible();
    await expect(page.locator('main')).toContainText('1 answers · 0 comments · 0 dismissals');
    await expect(page.locator('html')).toHaveCSS('color-scheme', mode);
    await expect(page.locator('.decision')).toHaveCount(0);
    expect(await page.evaluate(() => (globalThis as any).closeAttempts)).toBe(1);
    const count = requests.length;
    await page.clock.fastForward(40000);
    expect(requests).toHaveLength(count);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await page.screenshot({ path: test.info().outputPath(`decide-sent-${outcome}.png`), fullPage: true });
  });
}

test('a fully accepted partial round closes a script-opened tab', async ({ page }) => {
  await fixture(page);
  await page.goto(`${origin}/decide`);
  const opened = page.waitForEvent('popup');
  await page.evaluate(() => window.open('about:blank', '_blank'));
  const popup = await opened;
  await fixture(popup, { items: [item, { ...item, id: 41 }] });
  await popup.goto(`${origin}/decide?role=7#t=launch-secret`);
  await popup.locator('#decision-40 input[value="review"]').check();
  const closed = popup.waitForEvent('close');
  await popup.getByRole('button', { name: 'Submit & close', exact: true }).click();
  await closed;
  expect(popup.isClosed()).toBe(true);
  expect(page.isClosed()).toBe(false);
});

test('an expiring in-flight poll cannot touch controls removed by the sent confirmation', async ({ page }) => {
  await page.clock.install();
  await fixture(page);
  await blockClose(page);
  const errors: string[] = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.goto(`${origin}/decide?role=7#t=launch-secret`);
  await expect(page.locator('#decision-40')).toBeVisible();
  let release!: () => void;
  const held = new Promise<void>(resolve => { release = resolve; });
  await page.route('**/board/decide-items', async route => {
    await held;
    await route.fulfill({ status: 401, json: { error: 'expired' } });
  });
  const polling = page.waitForRequest(`${origin}/api/hera/roles/7/board/decide-items`);
  await page.clock.fastForward(20000);
  await polling;
  await page.locator('#decision-40 input[value="review"]').check();
  await page.getByRole('button', { name: 'Submit & close', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Actions sent — you can close this tab' })).toBeVisible();
  await page.evaluate(() => {
    (globalThis as any).lateControlLookups = [];
    const original = document.getElementById.bind(document);
    document.getElementById = id => { (globalThis as any).lateControlLookups.push(id); return original(id); };
  });
  const expired = page.waitForResponse(response => response.url().endsWith('/decide-items') && response.status() === 401);
  release();
  await expired;
  await page.waitForLoadState('networkidle');
  expect(await page.evaluate(() => (globalThis as any).lateControlLookups)).toEqual([]);
  expect(errors).toEqual([]);
  await expect(page.getByRole('heading', { name: 'Actions sent — you can close this tab' })).toBeVisible();
});

test('a pending copy cannot touch controls removed by the sent confirmation', async ({ page }) => {
  await fixture(page);
  await blockClose(page);
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'clipboard', { value: {
      writeText: () => new Promise<void>(resolve => { (globalThis as any).releaseCopy = resolve; }),
    } });
  });
  await page.goto(`${origin}/decide?role=7#t=launch-secret`);
  await page.locator('#decision-40 input[value="review"]').check();
  await page.getByRole('button', { name: 'Copy responses', exact: true }).click();
  await page.getByRole('button', { name: 'Submit & close', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Actions sent — you can close this tab' })).toBeVisible();
  const lookups = await page.evaluate(async () => {
    const accessed: string[] = [], original = document.getElementById.bind(document);
    document.getElementById = id => { accessed.push(id); return original(id); };
    (globalThis as any).releaseCopy();
    await new Promise<void>(resolve => queueMicrotask(resolve));
    return accessed;
  });
  expect(lookups).toEqual([]);
});

for (const outcome of ['conflict', 'invalid', 'not_found', 'request_failed']) {
  test(`${outcome} submission keeps the tab and its draft`, async ({ page }) => {
    await fixture(page, { outcome, answerStatus: outcome === 'request_failed' ? 500 : 200 });
    await blockClose(page);
    await page.goto(`${origin}/decide?role=7#t=launch-secret`);
    await page.locator('#decision-40 input[value="review"]').check();
    await page.locator('#note-40').fill('Keep this note');
    const response = page.waitForResponse(`${origin}/api/hera/roles/7/board/answers`);
    await page.getByRole('button', { name: 'Submit & close', exact: true }).click();
    await response;
    await expect(page.locator('#status')).toContainText('Review the remaining responses');
    await expect(page.locator('#main')).toContainText('Keep this note');
    await page.getByRole('button', {name:'Edit on board'}).click();
    await expect(page.locator('#decision-40')).toBeVisible();
    await expect(page.locator('#note-40')).toHaveValue('Keep this note');
    await expect(page.getByRole('heading', { name: 'Actions sent — you can close this tab' })).toHaveCount(0);
    expect(await page.evaluate(() => (globalThis as any).closeAttempts)).toBe(0);
    expect(page.isClosed()).toBe(false);
  });
}
