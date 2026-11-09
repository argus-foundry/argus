import { test, expect } from '@playwright/test';
import { resetServer } from '../../../_helpers';
import assert from 'node:assert/strict';
let browser;
const cleanup = [];
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
test('settings rename, workflow and session actions reach only an owned isolated task', async () => {
  const api = process.env.WORKSPACE_TEST_API || 'http://127.0.0.1:17744',
    headers = {
      Authorization: 'Bearer test-token',
      'Content-Type': 'application/json'
    };
  async function read(path) {
    const response = await fetch(api + path, {
      headers
    });
    assert.equal(response.ok, true);
    return response.json();
  }
  const initial = (await read('/api/tasks?archived=all')).tasks.find(task => task.name === 'echo-bash');
  assert.ok(initial?.backend === 'bash-test' && initial.worktree_path.includes('/argus-test-home-'), 'Refusing writes outside the isolated harness');
  const name = `task-settings-${Date.now()}`;
  const response = await fetch(api + '/api/tasks', {
    method: 'POST',
    headers,
    body: JSON.stringify({
      name,
      project: initial.project
    })
  });
  assert.equal(response.status, 201);
  const task = await response.json();
  const page = await browser.newPage({
    viewport: {
      width: 1440,
      height: 900
    }
  });
  cleanup.push(() => page.close());
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  cleanup.push(() => assert.deepEqual(errors, []));
  await page.goto(process.env.WORKSPACE_TEST_URL || 'http://127.0.0.1:17750');
  await page.locator('#token').fill('test-token');
  await page.locator('#connect-button').click();
  await page.getByRole('button', {
    name: `Task actions for ${name}`,
    exact: true
  }).click();
  await page.getByRole('menuitem', {
    name: 'Task settings',
    exact: true
  }).click();
  const panel = page.locator('#task-settings-panel');
  const renamed = name + '-renamed';
  await panel.getByLabel('Task name', {
    exact: true
  }).fill(renamed);
  await panel.getByRole('button', {
    name: 'Save name',
    exact: true
  }).click();
  await page.waitForFunction(() => document.querySelector('#task-settings-status').textContent.includes('Saved'));
  assert.equal((await read(`/api/tasks/${task.id}`)).name, renamed);
  await panel.getByLabel('Workflow status', {
    exact: true
  }).selectOption('in_review');
  await panel.getByRole('button', {
    name: 'Save status',
    exact: true
  }).click();
  await page.waitForFunction(() => document.querySelector('#task-settings-status').textContent.includes('Saved'));
  assert.equal((await read(`/api/tasks/${task.id}`)).status, 'in_review');
  await panel.getByRole('button', {
    name: 'Stop agent',
    exact: true
  }).click();
  await page.locator('#task-action-dialog').getByRole('button', {
    name: 'Confirm',
    exact: true
  }).click();
  await page.waitForFunction(() => !document.querySelector('#task-settings-panel fieldset').disabled);
  await expect.poll(async () => (await read(`/api/tasks/${task.id}`)).live, {timeout:10000}).toBe(false);
  await panel.getByRole('button', {
    name: 'Resume agent',
    exact: true
  }).click();
  await page.locator('#task-action-dialog').getByRole('button', {
    name: 'Confirm',
    exact: true
  }).click();
  await page.waitForFunction(() => !document.querySelector('#task-settings-panel fieldset').disabled);
  const resumed = await read(`/api/tasks/${task.id}`);
  assert.equal(resumed.live, true);
  assert.equal(resumed.status, 'in_progress');
  assert.equal((await read(`/api/tasks/${initial.id}`)).name, initial.name);
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
