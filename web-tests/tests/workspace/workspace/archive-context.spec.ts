import { test } from '@playwright/test';
import { resetServer } from '../../_helpers';
import { archiveSelected } from '../_workspace-controls-fixture.ts';
import assert from 'node:assert/strict';
let browser;
const cleanup = [];
async function fixture() {
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
  let tasks = Array.from({
    length: 40
  }, (_, index) => ({
    id: `task-${index}`,
    name: `Task ${index}`,
    project: 'archive-project',
    status: 'complete',
    live: false
  }));
  await page.route('**/api/**', route => route.fulfill({
    status: 404,
    json: {}
  }));
  await page.route('**/api/tasks?archived=all', route => route.fulfill({
    json: {
      tasks
    }
  }));
  await page.route('**/api/projects/full', route => route.fulfill({
    json: {
      projects: [{
        name: 'archive-project'
      }]
    }
  }));
  await page.route('**/api/hera', route => route.fulfill({
    json: {}
  }));
  for (const operation of ['archive', 'unarchive']) await page.route(`**/api/tasks/*/${operation}`, route => {
    const id = new URL(route.request().url()).pathname.split('/')[3];
    tasks = tasks.map(task => task.id === id ? {
      ...task,
      archived: operation === 'archive'
    } : task);
    return route.fulfill({
      json: {}
    });
  });
  await page.goto(process.env.WORKSPACE_TEST_URL || 'http://127.0.0.1:17750');
  await page.locator('#token').fill('fixture-token');
  await page.locator('#connect-button').click();
  await page.locator('.task-row').first().waitFor();
  await page.locator('.task-row[data-task-id="task-10"]').click();
  await page.locator('.xterm').waitFor();
  await page.evaluate(() => {
    window.archiveTerminal = document.querySelector('.xterm');
  });
  return page;
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
for (const source of ['shortcut', 'rail']) test(`selected archive from ${source} keeps main task, active filter and scroll; shortcut restore reselects it`, async () => {
  const page = await fixture();
  await page.locator('#task-groups').evaluate((groups, top) => {
    groups.scrollTop = top;
  }, source === 'shortcut' ? 700 : 250);
  const before = await page.locator('#task-groups').evaluate(groups => groups.scrollTop);
  if (source === 'shortcut') await archiveSelected(page);else await page.getByRole('button', {
    name: 'Archive Task 10',
    exact: true
  }).click();
  await page.locator('#archive-dialog .primary').click();
  await page.locator('.task-row[data-task-id="task-10"]').waitFor({
    state: 'detached'
  });
  assert.equal(await page.locator('#active-filter').getAttribute('aria-pressed'), 'true');
  assert.equal(await page.locator('.task-row[aria-current="page"]').count(), 0);
  assert.equal(await page.locator('#task-title').textContent(), 'Task 10');
  assert.equal(await page.locator('#archive-task').count(), 0);
  assert.equal(await page.locator('#task-groups').evaluate(groups => groups.scrollTop), before);
  assert.equal(await page.evaluate(() => window.archiveTerminal === document.querySelector('.xterm')), true);
  assert.equal(new URL(page.url()).searchParams.get('task'), 'task-10');
  await archiveSelected(page);
  await page.locator('#archive-dialog .primary').click();
  await page.locator('.task-row[data-task-id="task-10"]').waitFor();
  assert.equal(await page.locator('.task-row[aria-current="page"]').getAttribute('data-task-id'), 'task-10');
  assert.equal(await page.locator('#active-filter').getAttribute('aria-pressed'), 'true');
  assert.equal(await page.locator('#task-groups').evaluate(groups => groups.scrollTop), before);
});
test('restoring from Archived leaves that filter open and the same main task without a rail highlight', async () => {
  const page = await fixture();
  await archiveSelected(page);
  await page.locator('#archive-dialog .primary').click();
  await page.locator('.task-row[data-task-id="task-10"]').waitFor({
    state: 'detached'
  });
  await page.locator('#archive-filter').click();
  await page.locator('.task-row[data-task-id="task-10"]').click();
  await archiveSelected(page);
  await page.locator('#archive-dialog .primary').click();
  await page.locator('.task-row[data-task-id="task-10"]').waitFor({
    state: 'detached'
  });
  assert.equal(await page.locator('#archive-filter').getAttribute('aria-pressed'), 'true');
  assert.equal(await page.locator('#task-title').textContent(), 'Task 10');
  assert.equal(await page.locator('.task-row[aria-current="page"]').count(), 0);
  assert.equal(await page.locator('#archive-task').count(), 0);
  await page.locator('#active-filter').click();
  assert.equal(await page.locator('.task-row[aria-current="page"]').getAttribute('data-task-id'), 'task-10');
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
