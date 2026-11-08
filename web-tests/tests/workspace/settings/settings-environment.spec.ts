import { test, expect } from '@playwright/test';
import { resetServer } from '../../_helpers';
import { confirmClick } from '../_confirmation-fixture.ts';
import assert from 'node:assert/strict';
let browser, section;
const cleanup = [];
async function fixture() {
  const page = await browser.newPage();
  cleanup.push(() => page.close());
  const writes = [],
    errors = [];
  page.on('pageerror', e => errors.push(e.message));
  cleanup.push(() => assert.deepEqual(errors, []));
  const state = {
    fail: false,
    proposals: [{
      id: 'p1',
      task_id: 'a',
      project: 'demo',
      entries: [{
        path: '/tmp/proposed',
        reason: '<script>untrusted</script>',
        warning: 'Broad grant'
      }]
    }]
  };
  await page.route('**/api/**', route => route.fulfill({
    status: 404,
    json: {}
  }));
  for (const [path, data] of [['tasks?archived=all', {
    tasks: []
  }], ['projects/full', {
    projects: []
  }], ['hera', {
    orchestrators: []
  }], ['backends', {
    backends: [{
      name: 'alpha',
      command: 'agent --flag',
      kind: 'claude',
      model: 'model-a',
      models: ['model-a', 'model-b']
    }]
  }], ['accounts', [{
    name: 'work',
    label: 'Work',
    supports: {
      claude: true,
      codex: true
    },
    logged_in: true,
    email: 'fixture@example.invalid',
    is_default: true
  }]]]) await page.route('**/api/' + path, route => route.fulfill({
    json: data
  }));
  await page.route('**/api/settings', route => {
    if (route.request().method() === 'PUT') {
      writes.push(route.request().postDataJSON());
      if (state.fail) return route.fulfill({
        status: 403,
        json: {}
      });
    }
    return route.fulfill({
      json: {
        sandbox: {
          enabled: true,
          available: true,
          deny_read: ['/private'],
          extra_write: ['/tmp/old'],
          allow_apple_events: ['com.apple.finder']
        }
      }
    });
  });
  await page.route('**/api/sandbox/proposals?status=pending', route => route.fulfill({
    json: {
      proposals: state.proposals
    }
  }));
  await page.route('**/api/sandbox/proposals/*/*', route => {
    writes.push({
      path: new URL(route.request().url()).pathname,
      body: route.request().postDataJSON()
    });
    if (state.fail) return route.fulfill({
      status: 403,
      json: {}
    });
    state.proposals = [];
    return route.fulfill({
      json: {
        scope: 'project',
        applied: ['/tmp/edited'],
        warnings: []
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
    state,
    writes
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
test('Sandbox partial saves retain denied drafts and unrelated grants', async () => {
  const {
    page,
    state,
    writes
  } = await fixture();
  await section(page, 'Sandbox');
  await page.getByLabel('Global extra write paths', {
    exact: true
  }).fill('/tmp/edited\n /tmp/second ');
  state.fail = true;
  await page.getByRole('button', {
    name: 'Save sandbox',
    exact: true
  }).click();
  await page.locator('#settings-status').filter({
    hasText: 'master token'
  }).waitFor();
  assert.equal(await page.getByLabel('Global extra write paths', {
    exact: true
  }).inputValue(), '/tmp/edited\n /tmp/second ');
  state.fail = false;
  await page.getByRole('button', {
    name: 'Save sandbox',
    exact: true
  }).click();
  await page.locator('#settings-status').filter({
    hasText: 'new sessions'
  }).waitFor();
  assert.deepEqual(writes.at(-1), {
    sandbox: {
      enabled: true,
      deny_read: ['/private'],
      extra_write: ['/tmp/edited', '/tmp/second']
    }
  });
});
test('Proposal editing survives failed approval and explicit confirmation controls grants', async () => {
  const {
    page,
    state,
    writes
  } = await fixture();
  await section(page, 'Sandbox');
  await page.getByText('<script>untrusted</script>', {
    exact: true
  }).waitFor();
  assert.equal(await page.getByText('<script>untrusted</script>', {
    exact: true
  }).count(), 1);
  await page.getByLabel('Proposed path', {
    exact: true
  }).fill('/tmp/edited');
  await confirmClick(page, () => page.getByRole('button', {
    name: 'Approve paths',
    exact: true
  }).click(), dialog => dialog.dismiss());
  assert.deepEqual(writes, []);
  state.fail = true;
  await confirmClick(page, () => page.getByRole('button', {
    name: 'Approve paths',
    exact: true
  }).click(), dialog => dialog.accept());
  await page.locator('#settings-status').filter({
    hasText: 'master token'
  }).waitFor();
  assert.equal(await page.getByLabel('Proposed path', {
    exact: true
  }).inputValue(), '/tmp/edited');
  state.fail = false;
  await confirmClick(page, () => page.getByRole('button', {
    name: 'Approve paths',
    exact: true
  }).click(), dialog => dialog.accept());
  await page.getByText('No pending write-path proposals.', {
    exact: true
  }).waitFor();
  assert.deepEqual(writes.at(-1).body, {
    scope: 'project',
    paths: ['/tmp/edited']
  });
});
test('Agents show real backend/account fields and isolate account read failures', async () => {
  const {
    page,
    writes
  } = await fixture();
  await section(page, 'Agents');
  await page.getByText('agent --flag', {
    exact: true
  }).waitFor();
  await page.getByText('fixture@example.invalid', {
    exact: true
  }).waitFor();
  assert.ok(await page.getByText('Codex sign-in unknown', {
    exact: true
  }).count());
  await page.route('**/api/accounts', route => route.fulfill({
    status: 500,
    json: {}
  }));
  await page.getByRole('button', {
    name: 'Refresh agents',
    exact: true
  }).click();
  await page.getByText(/Accounts unavailable/).waitFor();
  assert.equal(await page.getByText('agent --flag', {
    exact: true
  }).isVisible(), true);
  assert.deepEqual(writes, []);
});
test('Empty proposal grants are blocked and rejection requires confirmation', async () => {
  const {
    page,
    writes
  } = await fixture();
  await section(page, 'Sandbox');
  await page.getByLabel('Proposed path', {
    exact: true
  }).fill(' ');
  await page.getByRole('button', {
    name: 'Approve paths',
    exact: true
  }).click();
  await page.locator('#settings-status').filter({
    hasText: 'at least one'
  }).waitFor();
  assert.deepEqual(writes, []);
  await page.getByRole('button', {
    name: 'Add path',
    exact: true
  }).click();
  assert.equal(await page.getByLabel('Proposed path', {
    exact: true
  }).count(), 2);
  await page.getByRole('button', {
    name: 'Remove path',
    exact: true
  }).last().click();
  assert.equal(await page.getByLabel('Proposed path', {
    exact: true
  }).count(), 1);
  await confirmClick(page, () => page.getByRole('button', {
    name: 'Reject proposal',
    exact: true
  }).click(), dialog => dialog.dismiss());
  assert.deepEqual(writes, []);
  await confirmClick(page, () => page.getByRole('button', {
    name: 'Reject proposal',
    exact: true
  }).click(), dialog => dialog.accept());
  await page.getByText('No pending write-path proposals.', {
    exact: true
  }).waitFor();
  assert.equal(writes.at(-1).path, '/api/sandbox/proposals/p1/reject');
});
test('proposal approval cannot be overwritten by the stale global Sandbox form', async () => {
  const {
    page,
    writes
  } = await fixture();
  await section(page, 'Sandbox');
  await page.getByLabel('Grant scope', {
    exact: true
  }).selectOption('global');
  await confirmClick(page, () => page.getByRole('button', {
    name: 'Approve paths',
    exact: true
  }).click(), dialog => dialog.accept());
  await page.getByText('No pending write-path proposals.', {
    exact: true
  }).waitFor();
  await page.getByRole('button', {
    name: 'Save sandbox',
    exact: true
  }).click();
  assert.equal(writes.filter(write => write.sandbox).length, 0, 'Stale global paths must not replace the just-approved grant');
  await page.locator('#settings-status').filter({
    hasText: 'Reload Sandbox'
  }).waitFor();
});
test('missing account sign-in fields stay unknown instead of claiming signed out', async () => {
  const {
    page
  } = await fixture();
  await page.route('**/api/accounts', route => route.fulfill({
    json: [{
      name: 'unknown',
      label: 'Unknown sign-in',
      supports: {
        claude: true,
        codex: true
      }
    }]
  }));
  await section(page, 'Agents');
  await page.getByRole('heading', {
    name: 'Unknown sign-in',
    exact: true
  }).waitFor();
  assert.equal(await page.getByText('Claude sign-in unknown', {
    exact: true
  }).count(), 1);
  assert.equal(await page.getByText('Codex sign-in unknown', {
    exact: true
  }).count(), 1);
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});

for(const removed of [0,1,2])test(`keyboard removal of proposal path ${removed} preserves editing focus`,async()=>{
  const {page}=await fixture();
  await section(page,'Sandbox');
  const paths=page.getByLabel('Proposed path',{exact:true}),add=page.getByRole('button',{name:'Add path',exact:true});
  await expect(paths).toHaveCount(1);await add.click();await add.click();
  for(let i=0;i<3;i++)await paths.nth(i).fill(`/tmp/path-${i}`);
  const remove=page.getByRole('button',{name:'Remove path',exact:true});
  await remove.nth(removed).focus();await page.keyboard.press('Enter');
  await expect(paths).toHaveCount(2);
  await expect(paths.nth(Math.min(removed,1))).toBeFocused();
  await expect(paths.nth(Math.min(removed,1))).toHaveValue(removed===0?'/tmp/path-1':removed===1?'/tmp/path-2':'/tmp/path-1');
  for(let i=0;i<2;i++){await remove.first().focus();await page.keyboard.press('Enter');}
  await expect(paths).toHaveCount(0);await expect(add).toBeFocused();
});
