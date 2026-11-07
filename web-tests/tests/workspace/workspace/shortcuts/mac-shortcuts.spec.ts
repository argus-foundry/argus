import { test } from '@playwright/test';
import { resetServer } from '../../../_helpers';
import assert from 'node:assert/strict';
test('workspace shortcuts mac-shortcuts', async ({
  browser
}) => {
  await resetServer();
  const page = await browser.newPage();
  const url = process.env.WORKSPACE_TEST_URL || 'http://127.0.0.1:18750';
  const task = {
    id: 'shortcut-task',
    name: 'Shortcut task',
    project: 'fixture',
    backend: 'claude',
    status: 'in_progress',
    live: true,
    idle: true
  };
  {
    await page.route('**/api/**', async route => {
      const path = new URL(route.request().url()).pathname;
      const payload = path === '/api/tasks' ? {
        tasks: [task]
      } : path === '/api/projects' ? {
        projects: [{
          name: 'fixture'
        }]
      } : path.endsWith('/size') ? {
        cols: 80,
        rows: 24
      } : path.endsWith('/output') ? '' : path === '/api/backends' ? {
        backends: []
      } : {};
      if (path.endsWith('/stream')) return route.fulfill({
        status: 200,
        contentType: 'text/event-stream',
        body: ': fixture\n\n'
      });
      await route.fulfill({
        contentType: 'application/json',
        body: typeof payload === 'string' ? payload : JSON.stringify(payload)
      });
    });
    await page.goto(url);
    await page.locator('#token').fill('fixture');
    await page.locator('#connect-button').click();
    await page.locator('.task-row').click();
    await page.locator('#shortcuts-help').click();
    await page.locator('#shortcuts-dialog').waitFor();
    await page.keyboard.press('Escape');
    await page.locator('#task-title').click();
    await page.keyboard.press('Alt+2');
    assert.equal(await page.locator('#tab-review').getAttribute('aria-selected'), 'true');
    await page.keyboard.press('Alt+1');
    assert.equal(await page.locator('#tab-agent').getAttribute('aria-selected'), 'true');
    await page.locator('#draft').fill('keep draft');
    await page.keyboard.press('Alt+2');
    assert.equal(await page.locator('#tab-agent').getAttribute('aria-selected'), 'true');
    await page.keyboard.press('Alt+Shift+F');
    assert.equal(await page.locator('#search').evaluate(el => el === document.activeElement), true);
    assert.equal(await page.locator('#draft').inputValue(), 'keep draft');
    console.log('PASS shortcuts dialog, tabs, filter and native editable ownership');
  }
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
