import { test } from '@playwright/test';
import { resetServer } from '../../_helpers';
import { confirmClick } from '../_confirmation-fixture.ts';
import assert from 'node:assert/strict';
let browser, section;
const cleanup = [];
async function fixture() {
  const page = await browser.newPage();
  page.setDefaultTimeout(5000);
  cleanup.push(() => page.close());
  const calls = [],
    errors = [],
    state = {
      path: '/tmp/argus-fixture',
      sourceStatus: 200,
      updateStatus: 200,
      cleanupStatus: 200,
      restart: false
    };
  page.on('pageerror', error => errors.push(error.message));
  cleanup.push(() => assert.deepEqual(errors, []));
  await page.route('**/api/**', route => route.fulfill({
    status: 404,
    json: {}
  }));
  for (const [path, json] of [['tasks?archived=all', {
    tasks: []
  }], ['projects/full', {
    projects: []
  }], ['hera', {
    orchestrators: []
  }], ['backends', {
    backends: []
  }], ['settings', {
    defaults: {},
    kb: {},
    api: {}
  }]]) await page.route('**/api/' + path, route => route.fulfill({
    json
  }));
  await page.route('**/api/source-path', route => {
    if (route.request().method() === 'PUT') {
      calls.push({
        path: '/api/source-path',
        method: 'PUT',
        body: route.request().postDataJSON()
      });
      if (state.sourceStatus === 200) state.path = route.request().postDataJSON().path.trim();
    }
    return route.fulfill({
      status: state.sourceStatus,
      json: {
        path: state.path
      }
    });
  });
  await page.route('**/api/update', route => {
    calls.push({
      path: '/api/update',
      method: route.request().method()
    });
    return route.fulfill({
      status: state.updateStatus,
      json: {
        output: '<script>build output</script>',
        error: state.updateStatus === 200 ? undefined : 'fixture compilation failed',
        restart: state.restart
      }
    });
  });
  await page.route('**/api/maintenance/prune-completed', route => {
    calls.push({
      path: '/api/maintenance/prune-completed',
      method: route.request().method()
    });
    return route.fulfill({
      status: state.cleanupStatus,
      json: {
        pruned: 2,
        worktrees: 2,
        orphans: 1,
        skippedHeraBound: 3
      }
    });
  });
  await page.goto(process.env.WORKSPACE_TEST_URL || 'http://127.0.0.1:17750');
  await page.locator('#token').fill('fixture-token');
  await page.locator('#connect-button').click();
  await page.locator('#disconnect:visible').waitFor();
  await page.locator('#workspace-settings').click();
  return {
    page,
    calls,
    state
  };
}
test.beforeAll(async ({
  browser: workerBrowser
}) => {
  browser = workerBrowser;
  await resetServer();
  section = (page, name) => page.locator('.settings-nav').getByRole('button', {
    name,
    exact: true
  }).click();
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
test('Maintenance confirms the deletion boundary and reports all daemon counts', async () => {
  const {
    page,
    calls
  } = await fixture();
  await section(page, 'Maintenance');
  await confirmClick(page, () => page.getByRole('button', {
    name: 'Clean up completed tasks',
    exact: true
  }).click(), dialog => {
    assert.match(dialog.message(), /completed.*tasks/i);
    assert.match(dialog.message(), /logs/i);
    assert.match(dialog.message(), /worktrees.*branches/i);
    assert.match(dialog.message(), /orphan/i);
    dialog.dismiss();
  });
  assert.deepEqual(calls, []);
  await confirmClick(page, () => page.getByRole('button', {
    name: 'Clean up completed tasks',
    exact: true
  }).click(), dialog => dialog.accept());
  await page.locator('#maintenance-result').filter({
    hasText: '2 tasks'
  }).waitFor();
  const result = await page.locator('#maintenance-result').textContent();
  assert.match(result, /2 worktrees/);
  assert.match(result, /1 orphan/);
  assert.match(result, /3.*Hera/);
  assert.deepEqual(calls, [{
    path: '/api/maintenance/prune-completed',
    method: 'POST'
  }]);
});
test('failed cleanup is not retried and controls recover', async () => {
  const {
    page,
    state,
    calls
  } = await fixture();
  await section(page, 'Maintenance');
  state.cleanupStatus = 500;
  await confirmClick(page, () => page.getByRole('button', {
    name: 'Clean up completed tasks',
    exact: true
  }).click(), dialog => dialog.accept());
  await page.locator('#settings-status').filter({
    hasText: 'not confirmed'
  }).waitFor();
  assert.equal(calls.length, 1);
  assert.equal(await page.getByRole('button', {
    name: 'Clean up completed tasks',
    exact: true
  }).isEnabled(), true);
});
test('Update retains failed source edits and prevents rebuilding from an unsaved path', async () => {
  const {
    page,
    state,
    calls
  } = await fixture();
  await section(page, 'Update');
  await page.getByLabel('Source path', {
    exact: true
  }).fill('/tmp/changed');
  await page.getByRole('button', {
    name: 'Update Argus',
    exact: true
  }).click();
  await page.locator('#settings-status').filter({
    hasText: 'Save source path'
  }).waitFor();
  assert.deepEqual(calls, []);
  state.sourceStatus = 403;
  await page.getByRole('button', {
    name: 'Save source path',
    exact: true
  }).click();
  await page.locator('#settings-status').filter({
    hasText: 'master token'
  }).waitFor();
  assert.equal(await page.getByLabel('Source path', {
    exact: true
  }).inputValue(), '/tmp/changed');
  state.sourceStatus = 500;
  await page.getByRole('button', {
    name: 'Save source path',
    exact: true
  }).click();
  await page.locator('#settings-status').filter({
    hasText: 'not confirmed'
  }).waitFor();
  assert.match(await page.locator('#settings-status').textContent(), /edits are kept/);
  state.sourceStatus = 200;
  await page.getByRole('button', {
    name: 'Save source path',
    exact: true
  }).click();
  await page.locator('#settings-status').filter({
    hasText: 'Saved source path'
  }).waitFor();
  assert.deepEqual(calls.at(-1).body, {
    path: '/tmp/changed'
  });
  await confirmClick(page, () => page.getByRole('button', {
    name: 'Update Argus',
    exact: true
  }).click(), dialog => {
    assert.match(dialog.message(), /rebuild|go install/i);
    assert.match(dialog.message(), /restart.*daemon/i);
    dialog.dismiss();
  });
  assert.equal(calls.some(call => call.path === '/api/update'), false);
});
test('failed update exposes plain build output and master errors without automatic restart', async () => {
  const {
    page,
    state,
    calls
  } = await fixture();
  await section(page, 'Update');
  await page.getByLabel('Source path', {
    exact: true
  }).waitFor();
  state.updateStatus = 500;
  await confirmClick(page, () => page.getByRole('button', {
    name: 'Update Argus',
    exact: true
  }).click(), dialog => dialog.accept());
  await page.locator('#update-output').filter({
    hasText: '<script>build output</script>'
  }).waitFor();
  assert.equal(await page.locator('#update-output script').count(), 0);
  await page.locator('#settings-status').filter({
    hasText: 'failed'
  }).waitFor();
  assert.equal(calls.filter(call => call.path === '/api/update').length, 1);
  state.updateStatus = 403;
  await confirmClick(page, () => page.getByRole('button', {
    name: 'Update Argus',
    exact: true
  }).click(), dialog => dialog.accept());
  await page.locator('#settings-status').filter({
    hasText: 'master token'
  }).waitFor();
});
test('closing a successful update clears its pending reload timer', async () => {
  const {
    page,
    state
  } = await fixture();
  await section(page, 'Update');
  await page.getByLabel('Source path', {
    exact: true
  }).waitFor();
  state.restart = true;
  await page.evaluate(() => {
    window.reloadTimers = new Set();
    const set = window.setTimeout,
      clear = window.clearTimeout;
    window.setTimeout = (fn, ms, ...args) => {
      const id = set(fn, ms, ...args);
      if (ms === 5000) window.reloadTimers.add(id);
      return id;
    };
    window.clearTimeout = id => {
      window.reloadTimers.delete(id);
      return clear(id);
    };
  });
  await confirmClick(page, () => page.getByRole('button', {
    name: 'Update Argus',
    exact: true
  }).click(), dialog => dialog.accept());
  await page.locator('#settings-status').filter({
    hasText: 'restart requested'
  }).waitFor();
  assert.equal(await page.evaluate(() => window.reloadTimers.size), 1);
  await section(page, 'General');
  await page.getByLabel('Vault path', {
    exact: true
  }).waitFor();
  assert.equal(await page.evaluate(() => window.reloadTimers.size), 0);
});
test('initial master denial recovers through explicit source-path reload without writes', async () => {
  const {
    page,
    state,
    calls
  } = await fixture();
  state.sourceStatus = 403;
  await section(page, 'Update');
  await page.locator('#settings-status').filter({
    hasText: 'master token'
  }).waitFor();
  state.sourceStatus = 200;
  await page.getByRole('button', {
    name: 'Reload saved source path',
    exact: true
  }).click();
  await page.waitForFunction(() => document.querySelector('[name="argusSourcePath"]').value === '/tmp/argus-fixture');
  assert.deepEqual(calls, []);
});
test('pending update locks controls and disconnect ignores the late response', async () => {
  const {
    page
  } = await fixture();
  await section(page, 'Update');
  await page.getByLabel('Source path', {
    exact: true
  }).waitFor();
  let release, reached;
  const held = new Promise(resolve => {
      reached = resolve;
    }),
    gate = new Promise(resolve => {
      release = resolve;
    });
  await page.route('**/api/update', async route => {
    reached();
    await gate;
    try {
      await route.fulfill({
        json: {
          output: 'late output',
          restart: true
        }
      });
    } catch {/* Disconnect canceled the request. */}
  });
  await confirmClick(page, () => page.getByRole('button', {
    name: 'Update Argus',
    exact: true
  }).click(), dialog => dialog.accept());
  await held;
  assert.equal(await page.getByRole('button', {
    name: 'Update Argus',
    exact: true
  }).isDisabled(), true);
  assert.equal(await page.locator('.settings-nav').getByRole('button', {
    name: 'General',
    exact: true
  }).isDisabled(), true);
  /* Exercise disconnect cancellation while workspace chrome is hidden. */
  await page.locator('#disconnect').evaluate(button => button.click());
  release();
  await page.waitForFunction(() => document.querySelector('#settings-panel').hidden);
  assert.equal(await page.locator('#update-output').count(), 0);
  assert.equal(await page.locator('#settings-status').textContent(), '');
});
test('incomplete update responses never claim a completed build or an absent restart', async () => {
  const {
    page
  } = await fixture();
  await section(page, 'Update');
  await page.getByLabel('Source path', {
    exact: true
  }).waitFor();
  for (const body of ['partial response', JSON.stringify({
    output: 'incomplete fixture'
  })]) {
    await page.route('**/api/update', route => route.fulfill({
      status: 200,
      contentType: 'application/json',
      body
    }));
    await confirmClick(page, () => page.getByRole('button', {
      name: 'Update Argus',
      exact: true
    }).click(), dialog => dialog.accept());
    await page.waitForFunction(() => /Update completed|not confirmed/.test(document.querySelector('#settings-status').textContent));
    assert.match(await page.locator('#settings-status').textContent(), /Update was not confirmed/);
  }
});
test('pending cleanup locks navigation, suppresses duplicates and refreshes the task snapshot', async () => {
  const {
    page
  } = await fixture();
  await section(page, 'Maintenance');
  let release,
    reached,
    posts = 0,
    reads = 0;
  const held = new Promise(resolve => {
      reached = resolve;
    }),
    gate = new Promise(resolve => {
      release = resolve;
    });
  await page.route('**/api/tasks?archived=all', route => {
    reads++;
    return route.fulfill({
      json: {
        tasks: []
      }
    });
  });
  await page.route('**/api/maintenance/prune-completed', async route => {
    posts++;
    reached();
    await gate;
    await route.fulfill({
      json: {
        pruned: 1,
        worktrees: 1,
        orphans: 0,
        skippedHeraBound: 0
      }
    });
  });
  await confirmClick(page, () => page.getByRole('button', {
    name: 'Clean up completed tasks',
    exact: true
  }).click(), dialog => dialog.accept());
  await held;
  assert.equal(await page.getByRole('button', {
    name: 'Clean up completed tasks',
    exact: true
  }).isDisabled(), true);
  assert.equal(await page.locator('.settings-nav [data-section="general"]').isDisabled(), true);
  await page.getByRole('button', {
    name: 'Clean up completed tasks',
    exact: true
  }).evaluate(button => button.dispatchEvent(new MouseEvent('click', {
    bubbles: true
  })));
  assert.equal(await page.locator('dialog[open]').count(), 0);
  assert.equal(posts, 1);
  assert.equal(reads, 0);
  release();
  await page.waitForFunction(() => document.querySelector('#maintenance-result').textContent.includes('1 tasks'));
  await page.waitForFunction(() => !document.querySelector('.settings-nav [data-section=general]').disabled);
  assert.equal(reads, 1);
  assert.equal(posts, 1);
  assert.equal(await page.locator('.settings-nav [data-section="general"]').isEnabled(), true);
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
