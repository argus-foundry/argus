import { test } from '@playwright/test';
import { resetServer } from '../../_helpers';
import assert from 'node:assert/strict';
let browser;
const cleanup = [];
async function fixture() {
  const page = await browser.newPage();
  page.setDefaultTimeout(8000);
  cleanup.push(() => page.close());
  const writes = [];
  await page.addInitScript(() => {
    window.permissionRequests = 0;
    window.holdPermission = false;
    window.Notification = class {
      static permission = 'default';
      static async requestPermission() {
        window.permissionRequests++;
        if (window.holdPermission) await new Promise(resolve => window.releasePermission = resolve);
        this.permission = 'granted';
        return 'granted';
      }
      close() {}
    };
  });
  await page.route('**/api/**', route => {
    const request = route.request(),
      path = new URL(request.url()).pathname;
    if (request.method() !== 'GET') writes.push(path);
    let result = {};
    if (path === '/api/tasks') result = {
      tasks: [{
        id: 'a',
        name: 'Task A',
        project: 'fixture',
        status: 'complete',
        live: false
      }]
    };else if (path === '/api/projects' || path === '/api/projects/full') result = {
      projects: [{
        name: 'fixture'
      }]
    };else if (path === '/api/accounts') result = [];else if (path.endsWith('/size')) result = {
      cols: 80,
      rows: 24
    };
    if (path.endsWith('/output')) return route.fulfill({
      body: '',
      headers: {
        'X-Output-Total': '0'
      }
    });
    if (path.endsWith('/stream')) return route.fulfill({
      contentType: 'text/event-stream',
      body: ':fixture\n\n'
    });
    return route.fulfill({
      json: result
    });
  });
  await page.goto(process.env.WORKSPACE_TEST_URL);
  await page.locator('#token').fill('fixture');
  await page.locator('#connect-button').click();
  await page.locator('#workspace-settings').click();
  await page.getByRole('button', {
    name: 'Notifications & devices',
    exact: true
  }).click();
  return {
    page,
    writes
  };
}
test.beforeAll(async ({
  browser: workerBrowser
}) => {
  browser = workerBrowser;
  await resetServer();
});
test.afterEach(async () => {
  const errors = [];
  for (const dispose of cleanup.splice(0)) {
    try {
      await dispose();
    } catch (error) {
      errors.push(error);
    }
  }
  if (errors.length) throw new AggregateError(errors, 'Fixture cleanup failed');
});
test('Notifications owns the persisted workspace alert flags without an attention task popover', async () => {
  const {
    page,
    writes
  } = await fixture();
  assert.equal(await page.locator('#attention-menu-toggle,#attention-menu').count(), 0);
  assert.equal(await page.getByLabel('Needs-input alerts', {
    exact: true
  }).isChecked(), true);
  assert.equal(await page.getByLabel('Idle alerts', {
    exact: true
  }).isChecked(), false);
  assert.equal(await page.evaluate(() => window.permissionRequests), 0);
  await page.getByLabel('Needs-input alerts', {
    exact: true
  }).uncheck();
  await page.getByLabel('Idle alerts', {
    exact: true
  }).check();
  assert.equal(await page.getByRole('button', {
    name: 'Enable browser banners',
    exact: true
  }).count(), 0);
  assert.deepEqual(await page.evaluate(() => JSON.parse(localStorage.getItem('argus-workspace:attention'))), {
    enabled: false,
    idle: true
  });
  assert.equal(await page.evaluate(() => window.permissionRequests), 0);
  await page.reload();
  await page.getByLabel('Needs-input alerts', {
    exact: true
  }).waitFor();
  assert.equal(await page.getByLabel('Needs-input alerts', {
    exact: true
  }).isChecked(), false);
  assert.equal(await page.getByLabel('Idle alerts', {
    exact: true
  }).isChecked(), true);
  assert.deepEqual(writes, []);
});
test('in-page alert toggles never request desktop permission', async () => {
  const {
    page
  } = await fixture();
  await page.getByLabel('Needs-input alerts', {
    exact: true
  }).uncheck();
  await page.goBack();
  await page.waitForFunction(() => document.querySelector('#settings-panel').hidden);
  assert.equal(await page.evaluate(() => window.permissionRequests), 0);
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
