import { test } from '@playwright/test';
import { resetServer } from '../../_helpers';
import { refreshWorkspace } from '../_workspace-controls-fixture.ts';
import assert from 'node:assert/strict';
let browser, frontend;
const cleanup = [];
async function fixture() {
  const page = await browser.newPage();
  cleanup.push(() => page.close());
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  cleanup.push(() => assert.deepEqual(errors, []));
  // Fulfill every API read locally; unexpected writes fail without reaching any daemon.
  await page.route('**/api/**', route => route.fulfill({
    status: 404,
    json: {}
  }));
  await page.route('**/api/tasks?archived=all', route => route.fulfill({
    json: {
      tasks: [{
        id: 'review-fixture',
        name: 'Populated task',
        project: 'populated',
        status: 'complete',
        live: false
      }]
    }
  }));
  return page;
}
async function connect(page) {
  await page.goto(frontend);
  await page.locator('#token').fill('fixture-token');
  await page.locator('#connect-button').click();
  await page.locator('.task-row').waitFor();
}
test.beforeAll(async ({
  browser: workerBrowser
}) => {
  browser = workerBrowser;
  await resetServer();
  frontend = process.env.WORKSPACE_TEST_URL || 'http://127.0.0.1:17750';
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
test('configured empty projects survive refresh, hide during search and clear on disconnect', async () => {
  const page = await fixture();
  let projectReads = 0;
  await page.route('**/api/projects/full', route => {
    projectReads++;
    assert.equal(route.request().headers().authorization, 'Bearer fixture-token');
    return route.fulfill({
      json: {
        projects: [{
          name: 'populated'
        }, {
          name: 'empty-project'
        }]
      }
    });
  });
  await connect(page);
  const empty = page.locator('.project-group[data-project="empty-project"]');
  assert.equal(await empty.count(), 1);
  assert.equal(await empty.locator('summary').count(), 0);
  assert.equal(await empty.locator('.project-name').textContent(), 'empty-project');
  assert.equal(await empty.locator('.task-row').count(), 0);
  assert.equal(await empty.locator('.project-count').count(), 0);
  assert.equal(await page.locator('.task-row').count(), 1);
  await page.locator('#search').fill('populated');
  assert.equal(await empty.count(), 0);
  assert.equal(await page.locator('.task-row').count(), 1);
  await page.locator('#search').fill('empty-project');
  assert.equal(await empty.count(), 0);
  assert.match(await page.locator('#task-groups').textContent(), /No tasks match/);
  await page.locator('#search').fill('');
  await refreshWorkspace(page);
  assert.equal(await empty.count(), 1);
  assert.equal(projectReads, 2);
  await page.locator('#archive-filter').click();
  assert.equal(await empty.locator('summary').count(), 0);
  assert.equal(await empty.getAttribute('open'), null);
  await page.locator('#disconnect').click();
  assert.equal(await page.locator('.project-group').count(), 0);
});
test('expanded directory rows request diffs with decoded UTF-8 and tab paths', async () => {
  const page = await fixture();
  await page.route('**/api/projects/full', route => route.fulfill({
    json: {
      projects: [{
        name: 'populated'
      }]
    }
  }));
  await page.route('**/api/tasks/*/git/status', route => route.fulfill({
    json: {
      Status: '?? nested/'
    }
  }));
  await page.route('**/api/tasks/*/files?dir=*', route => route.fulfill({
    json: {
      Files: [{
        Path: '"nested/caf\\303\\251.md"',
        Status: 'A'
      }, {
        Path: '"nested/tab\\tname.md"',
        Status: 'A'
      }]
    }
  }));
  const requested = [];
  await page.route('**/api/tasks/*/git/diff?path=*', route => {
    requested.push(new URL(route.request().url()).searchParams.get('path'));
    return route.fulfill({
      json: {
        Diff: 'diff --git a/f b/f\n@@ -0,0 +1 @@\n+Nested file'
      }
    });
  });
  await connect(page);
  await page.locator('.file-row').filter({
    hasText: 'nested/'
  }).click();
  const rows = page.locator('.directory-files .file-row');
  await rows.first().waitFor();
  await page.waitForFunction(() => [...document.querySelectorAll('.directory-files .file-counts')].length === 2 && [...document.querySelectorAll('.directory-files .file-counts')].every(node => node.textContent === '+1 −0'));
  for (let index = 0; index < 2; index++) {
    const selectedPath = ['nested/café.md', 'nested/tab\tname.md'][index];
    const response = page.waitForResponse(response => new URL(response.url()).pathname.endsWith('/git/diff') && new URL(response.url()).searchParams.get('path') === selectedPath);
    await rows.nth(index).click();
    await response;
  }
  assert.equal(requested.length, 4);
  assert.deepEqual(requested.slice(0, 2).sort(), ['nested/café.md', 'nested/tab\tname.md'].sort());
  assert.deepEqual(requested.slice(2), ['nested/café.md', 'nested/tab\tname.md']);
  assert.match(await page.locator('#review-panel').textContent(), /Nested file/);
  assert.equal(await rows.locator('[title]').count(), 0);
  assert.equal(await rows.first().getAttribute('title'), null);
  assert.equal(await rows.nth(1).getAttribute('title'), null);
  assert.equal(await rows.first().getAttribute('data-tooltip'), 'nested/café.md');
  assert.equal(await rows.nth(1).getAttribute('data-tooltip'), 'nested/tab\tname.md');
});
test('project list failure on connect still loads tasks and preserves authentication', async () => {
  const page = await fixture();
  await page.route('**/api/projects/full', route => route.fulfill({
    status: 500,
    json: {}
  }));
  await page.goto(frontend);
  await page.locator('#token').fill('fixture-token');
  await page.locator('#connect-button').click();
  await page.waitForFunction(() => !document.querySelector('#connect-button').disabled);
  assert.equal(await page.locator('#connect-dialog').evaluate(dialog => dialog.open), false);
  assert.equal(await page.locator('.task-row').textContent().then(text => text.includes('Populated task')), true);
  assert.match(await page.locator('#projects-warning').textContent(), /Project list unavailable/);
  assert.equal(await page.locator('#projects-warning').isVisible(), true);
  assert.equal(await page.locator('#connection-state').getAttribute('class'), 'connection-state connected');
  const taskRequest = page.waitForRequest(request => new URL(request.url()).pathname === '/api/tasks');
  await refreshWorkspace(page);
  assert.equal((await taskRequest).headers().authorization, 'Bearer fixture-token');
});
test('project list failure on refresh retains projects while task state updates and retry clears warning', async () => {
  const page = await fixture();
  let failed = false,
    updated = false;
  await page.route('**/api/projects/full', route => failed ? route.fulfill({
    status: 500,
    json: {}
  }) : route.fulfill({
    json: {
      projects: [{
        name: 'populated'
      }, {
        name: 'empty-project'
      }]
    }
  }));
  await page.route('**/api/tasks?archived=all', route => route.fulfill({
    json: {
      tasks: [{
        id: 'review-fixture',
        name: updated ? 'Updated task' : 'Populated task',
        project: 'populated',
        status: 'complete',
        live: false
      }]
    }
  }));
  await connect(page);
  failed = true;
  updated = true;
  await refreshWorkspace(page);
  assert.match(await page.locator('.task-row').textContent(), /Updated task/);
  assert.equal(await page.locator('.project-group[data-project="empty-project"] .project-name').count(), 1);
  assert.equal(await page.locator('#projects-warning').isVisible(), true);
  assert.equal(await page.locator('#connection-state').getAttribute('class'), 'connection-state connected');
  failed = false;
  await refreshWorkspace(page);
  assert.equal(await page.locator('#projects-warning').isVisible(), false);
  failed = true;
  await refreshWorkspace(page);
  assert.equal(await page.locator('#projects-warning').isVisible(), true);
  await page.locator('#disconnect').click();
  assert.equal(await page.locator('#projects-warning').isVisible(), false);
  assert.equal(await page.locator('.project-group').count(), 0);
});
for (const status of [401, 403]) test(`project authentication failures still reject connection (${status})`, async () => {
  const page = await fixture();
  await page.route('**/api/projects/full', route => route.fulfill({
    status,
    json: {}
  }));
  await page.goto(frontend);
  await page.locator('#token').fill('fixture-token');
  await page.locator('#connect-button').click();
  await page.locator('#connect-error:not([hidden])').waitFor();
  assert.equal(await page.locator('#connect-dialog').evaluate(dialog => dialog.open), true);
  assert.equal(await page.locator('.task-row').count(), 0);
  assert.equal(await page.locator('#disconnect').isVisible(), false);
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
