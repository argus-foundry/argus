import { test } from '@playwright/test';
import { resetServer } from '../../_helpers';
import assert from 'node:assert/strict';
test('settings settings-live', async ({
  browser
}) => {
  await resetServer();
  const frontend = process.env.WORKSPACE_TEST_URL || 'http://127.0.0.1:17750',
    daemon = process.env.WORKSPACE_TEST_API || 'http://127.0.0.1:17744';
  const headers = {
    Authorization: 'Bearer test-token',
    'Content-Type': 'application/json'
  };
  async function api(resource, options = {}) {
    const response = await fetch(daemon + resource, {
      ...options,
      headers
    });
    assert.equal(response.ok, true, `${resource} returned ${response.status}`);
    return response.json();
  }
  const tasks = (await api('/api/tasks?archived=all')).tasks;
  const initial = tasks.find(task => task.name === 'echo-bash');
  assert.ok(initial?.backend === 'bash-test' && initial.worktree_path.includes('/argus-test-home-'), 'Refusing settings writes outside the isolated harness');
  const before = await api('/api/settings'),
    name = `settings-parity-${Date.now()}`,
    projectPath = initial.worktree_path + '/settings-repository';
  const page = await browser.newPage({
      viewport: {
        width: 1440,
        height: 900
      }
    }),
    errors = [];
  page.on('pageerror', error => errors.push(error.message));
  try {
    await page.goto(frontend);
    await page.locator('#token').fill('test-token');
    await page.locator('#connect-button').click();
    await page.locator('#disconnect:visible').waitFor();
    await page.getByRole('button', {
      name: 'Settings',
      exact: true
    }).click();
    await page.getByLabel('Vault path', {
      exact: true
    }).fill(initial.worktree_path + '/settings-vault');
    await page.getByRole('button', {
      name: 'Save General',
      exact: true
    }).click();
    await page.locator('#settings-status').filter({
      hasText: 'Saved'
    }).waitFor();
    const saved = await api('/api/settings');
    assert.equal(saved.kb.metis_vault_path, initial.worktree_path + '/settings-vault');
    assert.deepEqual(saved.sandbox, before.sandbox);
    assert.deepEqual(saved.bridge, before.bridge);
    assert.equal(saved.defaults.permission_mode, before.defaults.permission_mode);
    assert.match(await page.locator('#settings-restart').textContent(), /restart required/i);
    console.log('PASS real partial General save preserves unrelated settings and reports restart');
    await page.getByRole('button', {
      name: 'Add repository',
      exact: true
    }).click();
    await page.getByLabel('Project name', {
      exact: true
    }).fill(name);
    await page.getByLabel('Repository path', {
      exact: true
    }).fill(projectPath);
    await page.getByRole('button', {
      name: 'Save project',
      exact: true
    }).click();
    await page.locator('#settings-status').filter({
      hasText: 'Saved project.'
    }).waitFor();
    let project = (await api('/api/projects/full')).projects.find(project => project.name === name);
    assert.equal(project.path, projectPath);
    await api('/api/projects/' + encodeURIComponent(name), {
      method: 'PUT',
      body: JSON.stringify({
        ...project,
        sandbox: {
          enabled: false,
          deny_read: ['/private'],
          extra_write: [],
          allow_apple_events: ['com.apple.finder']
        }
      })
    });
    await page.getByRole('button', {
      name: 'Refresh projects',
      exact: true
    }).click();
    await page.getByLabel('Default branch', {
      exact: true
    }).fill('settings-check');
    await page.getByText('Project sandbox overrides', {
      exact: true
    }).click();
    await page.getByLabel('Project sandbox', {
      exact: true
    }).selectOption('inherit');
    await page.getByRole('button', {
      name: 'Save project',
      exact: true
    }).click();
    await page.locator('#settings-status').filter({
      hasText: 'Saved project.'
    }).waitFor();
    project = (await api('/api/projects/full')).projects.find(project => project.name === name);
    assert.equal(project.branch, 'settings-check');
    assert.equal(project.sandbox.enabled, null);
    assert.deepEqual(project.sandbox.allow_apple_events, ['com.apple.finder']);
    assert.deepEqual(project.sandbox.deny_read, ['/private']);
    console.log('PASS real project create/edit preserves sandbox overrides and inherit state');
    await page.getByRole('button', {
      name: `Remove ${name}`,
      exact: true
    }).click();
    await page.getByRole('button', {
      name: 'Remove project',
      exact: true
    }).click();
    await page.getByRole('button', {
      name: `Remove ${name}`,
      exact: true
    }).waitFor({
      state: 'detached'
    });
    assert.equal((await api('/api/projects/full')).projects.some(project => project.name === name), false);
    assert.deepEqual((await api('/api/tasks?archived=all')).tasks.map(task => task.id).sort(), tasks.map(task => task.id).sort());
    console.log('PASS real project removal leaves existing tasks unchanged');
    await page.locator('.settings-nav').getByRole('button', {
      name: 'Sandbox',
      exact: true
    }).click();
    await page.getByLabel('Global deny read paths', {
      exact: true
    }).fill(initial.worktree_path + '/sandbox-private');
    await page.getByLabel('Global extra write paths', {
      exact: true
    }).fill(initial.worktree_path + '/sandbox-extra');
    await page.getByRole('button', {
      name: 'Save sandbox',
      exact: true
    }).click();
    await page.locator('#settings-status').filter({
      hasText: 'Saved sandbox'
    }).waitFor();
    const sandboxSaved = (await api('/api/settings')).sandbox;
    assert.deepEqual(sandboxSaved.deny_read, [initial.worktree_path + '/sandbox-private']);
    assert.deepEqual(sandboxSaved.extra_write, [initial.worktree_path + '/sandbox-extra']);
    assert.deepEqual(sandboxSaved.allow_apple_events, before.sandbox.allow_apple_events);
    console.log('PASS real sandbox partial save preserves Apple Events grants');
    /* Exercise disconnect cancellation while workspace chrome is hidden. */
    await page.locator('#disconnect').evaluate(button => button.click());
    assert.equal(await page.locator('#settings-panel').isVisible(), false);
    assert.equal(await page.getByRole('button', {
      name: 'Settings',
      exact: true
    }).isDisabled(), true);
    assert.deepEqual(errors, []);
  } finally {
    await api('/api/projects/' + encodeURIComponent(name), {
      method: 'DELETE'
    });
    await api('/api/settings', {
      method: 'PUT',
      body: JSON.stringify({
        sandbox: {
          enabled: before.sandbox.enabled,
          deny_read: before.sandbox.deny_read,
          extra_write: before.sandbox.extra_write
        },
        kb: before.kb,
        defaults: {
          backend: before.defaults.backend,
          share_project: before.defaults.share_project
        },
        api: {
          enabled: before.api.enabled
        }
      })
    });
  }
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
