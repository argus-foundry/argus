import { test, expect } from '@playwright/test';
import { resetServer } from '../../_helpers';

for (const direction of ['back', 'forward'] as const) test(`browser ${direction} retains an applied General save until confirmation`, async ({ page, request }) => {
  await resetServer();
  const frontend = process.env.WORKSPACE_TEST_URL || 'http://127.0.0.1:17750';
  const daemon = process.env.WORKSPACE_TEST_API || 'http://127.0.0.1:17744';
  const headers = { Authorization: 'Bearer test-token' };
  const tasks = (await (await request.get(daemon + '/api/tasks?archived=all', { headers })).json()).tasks;
  const task = tasks.find(item => item.name === 'echo-bash');
  expect(task.backend).toBe('bash-test');
  expect(task.worktree_path).toContain('/argus-test-home-');
  const before = await (await request.get(daemon + '/api/settings', { headers })).json();
  let release = () => {};
  const held = new Promise<void>(resolve => { release = resolve; });
  let committed = () => {};
  const applied = new Promise<void>(resolve => { committed = resolve; });
  try {
    await page.goto(frontend + '?task=' + task.id);
    await page.locator('#token').fill('test-token');
    await page.locator('#connect-button').click();
    await page.locator('#disconnect:visible').waitFor();
    await page.getByRole('button', { name: 'Settings', exact: true }).click();
    await page.getByLabel('Vault path', { exact: true }).waitFor();
    if (direction === 'forward') {
      // Seed a future task entry, then restore Settings before starting the save.
      await page.evaluate(() => {
        const url = new URL(location.href); url.searchParams.delete('settings');
        history.pushState({ ...history.state, workspacePosition: (history.state?.workspacePosition || 0) + 1 }, '', url);
        history.back();
      });
      await expect(page).toHaveURL(/settings=general/);
    }
    const settingsURL = page.url();
    const settingsState = await page.evaluate(() => history.state);
    const vault = task.worktree_path + '/history-' + direction;
    await page.getByLabel('Vault path', { exact: true }).fill(vault);
    await page.evaluate(() => {
      const original = window.fetch;
      (window as any).saveAborted = false;
      window.fetch = (input, options) => {
        if (String(input).includes('/api/settings') && options?.method === 'PUT') options.signal?.addEventListener('abort', () => { (window as any).saveAborted = true; });
        return original(input, options);
      };
    });
    await page.route('**/api/settings', async route => {
      if (route.request().method() !== 'PUT') return route.continue();
      const response = await route.fetch();
      expect(response.ok()).toBe(true);
      committed();
      await held;
      await route.fulfill({ response });
    });
    await page.getByRole('button', { name: 'Save General', exact: true }).click();
    await applied;
    expect((await (await request.get(daemon + '/api/settings', { headers })).json()).kb.metis_vault_path).toBe(vault);
    await page.evaluate(direction => {
      (window as any).saveTraversals = 0;
      window.addEventListener('popstate', () => { (window as any).saveTraversals++; });
      history[direction]();
    }, direction);
    await page.waitForFunction(() => (window as any).saveTraversals > 0);
    await expect(page).toHaveURL(settingsURL);
    await expect(page.locator('#settings-panel')).toBeVisible();
    await expect(page.locator('#settings-status')).toHaveText('Saving General…');
    expect(await page.evaluate(() => history.state)).toEqual(settingsState);
    expect(await page.evaluate(() => (window as any).saveAborted)).toBe(false);
    release();
    await expect(page.locator('#settings-status')).toHaveText('Saved General settings.');
    await expect(page.locator('#settings-restart')).toContainText('restart required');
    expect((await (await request.get(daemon + '/api/settings', { headers })).json()).kb.metis_vault_path).toBe(vault);
    await page.evaluate(direction => { history[direction](); }, direction);
    await expect(page.locator('#settings-panel')).toBeHidden();
    await page.evaluate(direction => { history[direction === 'back' ? 'forward' : 'back'](); }, direction);
    await expect(page.locator('#settings-panel')).toBeVisible();
    await expect(page).toHaveURL(settingsURL);
    expect(await page.evaluate(() => history.state)).toEqual(settingsState);
  } finally {
    release();
    await request.put(daemon + '/api/settings', { headers, data: { kb: before.kb } });
  }
});
