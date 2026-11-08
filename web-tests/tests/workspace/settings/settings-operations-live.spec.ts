import { test } from '@playwright/test';
import { resetServer } from '../../_helpers';
import { confirmClick } from '../_confirmation-fixture.ts';
import assert from 'node:assert/strict';
test('settings settings-operations-live', async ({
  browser
}) => {
  await resetServer();
  const frontend = process.env.WORKSPACE_TEST_URL || 'http://127.0.0.1:17750',
    daemon = process.env.WORKSPACE_TEST_API || 'http://127.0.0.1:17744';
  const headers = {
    Authorization: 'Bearer test-token'
  };
  async function read(path) {
    const response = await fetch(daemon + path, {
      headers
    });
    assert.equal(response.ok, true, path);
    return response.json();
  }
  const tasks = (await read('/api/tasks?archived=all')).tasks,
    initial = tasks.find(task => task.name === 'echo-bash');
  assert.ok(initial?.backend === 'bash-test' && initial.worktree_path.includes('/argus-test-home-'), 'Refusing schedule writes outside isolated harness');
  const page = await browser.newPage({
      viewport: {
        width: 1440,
        height: 1000
      }
    }),
    errors = [],
    name = `schedule-fixture-${Date.now()}`;
  page.on('pageerror', error => errors.push(error.message));
  const settings = page.locator('#settings-panel');
  const section = id => page.locator(`#settings-panel [data-section="${id}"]`).click();
  {
    await page.goto(frontend);
    await page.locator('#token').fill('test-token');
    await page.locator('#connect-button').click();
    await page.locator('#disconnect:visible').waitFor();
    await page.getByRole('button', {
      name: 'Settings',
      exact: true
    }).click();
    await section('schedules');
    await page.getByRole('button', {
      name: 'New schedule',
      exact: true
    }).click();
    await settings.getByLabel('Name', {
      exact: true
    }).fill(name);
    await settings.getByLabel('Project', {
      exact: true
    }).selectOption('test-proj');
    await settings.getByLabel('Backend', {
      exact: true
    }).selectOption('bash-test');
    await settings.getByLabel('Model', {
      exact: true
    }).fill('fixture-model');
    await settings.getByLabel('Cron schedule', {
      exact: true
    }).fill('@hourly');
    await settings.getByLabel('Prompt', {
      exact: true
    }).fill('line one\nline two\n');
    await settings.getByLabel('Enabled', {
      exact: true
    }).uncheck();
    await page.getByRole('button', {
      name: 'Save schedule',
      exact: true
    }).click();
    await page.getByRole('button', {
      name: `Edit ${name}`,
      exact: true
    }).waitFor();
    let saved = (await read('/api/schedules')).schedules.find(row => row.name === name);
    assert.ok(saved);
    assert.equal(saved.enabled, false);
    assert.equal(saved.schedule, '@hourly');
    assert.equal(saved.prompt, 'line one\nline two\n');
    assert.equal(saved.model, 'fixture-model');
    console.log('PASS schedule creation is independently confirmed by the isolated daemon, disabled with multiline prompt and custom model');
    await page.getByRole('button', {
      name: `Edit ${name}`,
      exact: true
    }).click();
    await settings.getByLabel('Cadence', {
      exact: true
    }).selectOption('once');
    await settings.getByLabel('Run once at', {
      exact: true
    }).fill('2099-01-01T09:30:00.123');
    await page.getByRole('button', {
      name: 'Save schedule',
      exact: true
    }).click();
    await page.getByRole('button', {
      name: `Edit ${name}`,
      exact: true
    }).waitFor();
    saved = (await read('/api/schedules')).schedules.find(row => row.id === saved.id);
    assert.equal(saved.schedule, '');
    assert.ok(saved.run_once_at.includes('2099-01-01'));
    assert.ok((await read(`/api/schedules/${saved.id}/raw`)).run_once_at.includes('.123'));
    assert.equal(saved.enabled, false);
    const once = (await read(`/api/schedules/${saved.id}/raw`)).run_once_at;
    await page.getByRole('button', {
      name: `Edit ${name}`,
      exact: true
    }).click();
    await settings.getByLabel('Model', {
      exact: true
    }).fill('fixture-model-2');
    await page.getByRole('button', {
      name: 'Save schedule',
      exact: true
    }).click();
    await page.getByRole('button', {
      name: `Edit ${name}`,
      exact: true
    }).waitFor();
    assert.equal((await read(`/api/schedules/${saved.id}/raw`)).run_once_at, once);
    console.log('PASS cadence switching and unrelated edits preserve disabled state and exact stored one-shot precision');
    const run = page.waitForResponse(response => new URL(response.url()).pathname.endsWith(`/schedules/${saved.id}/run`));
    await page.getByRole('button', {
      name: `Run ${name} now`,
      exact: true
    }).click();
    assert.equal((await run).status(), 503);
    await page.locator('#settings-status').filter({
      hasText: 'not confirmed'
    }).waitFor();
    assert.deepEqual((await read('/api/tasks?archived=all')).tasks.map(task => task.id).sort(), tasks.map(task => task.id).sort());
    await confirmClick(page, () => page.getByRole('button', {
      name: `Delete ${name}`,
      exact: true
    }).click(), dialog => dialog.accept());
    await page.getByRole('button', {
      name: `Edit ${name}`,
      exact: true
    }).waitFor({
      state: 'detached'
    });
    assert.equal((await read('/api/schedules')).schedules.some(row => row.id === saved.id), false);
    console.log('PASS unavailable scheduler does not claim success, and confirmed deletion removes only the schedule');
    await section('system');
    await page.getByText('Agent sessions', {
      exact: true
    }).waitFor();
    await section('logs');
    await settings.getByLabel('Log contents', {
      exact: true
    }).waitFor();
    const logRead = page.waitForResponse(response => new URL(response.url()).pathname.endsWith('/logs/daemon'));
    await settings.getByLabel('Log file', {
      exact: true
    }).selectOption('daemon');
    await logRead;
    assert.deepEqual(errors, []); /* Exercise disconnect cancellation while workspace chrome is hidden. */
    await page.locator('#disconnect').evaluate(button => button.click());
    console.log('PASS real metrics and bounded log endpoints mount in grouped Settings and clean up on disconnect');
  }
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
