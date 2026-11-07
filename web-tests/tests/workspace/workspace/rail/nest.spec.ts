import { test } from '@playwright/test';
import { resetServer } from '../../../_helpers';
import { refreshWorkspace } from '../../_workspace-controls-fixture.ts';
import assert from 'node:assert/strict';
let browser, tasks, role, roster, row, disclosure;
const cleanup = [];
async function fixture() {
  const page = await browser.newPage();
  cleanup.push(() => page.close());
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  cleanup.push(() => assert.deepEqual(errors, []));
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
        name: 'nest'
      }]
    }
  }));
  await page.route('**/api/hera', route => route.fulfill({
    json: roster
  }));
  await page.goto(process.env.WORKSPACE_TEST_URL);
  await page.locator('#token').fill('fixture-token');
  await page.locator('#connect-button').click();
  await page.locator('.task-row').first().waitFor();
  return page;
}
test.beforeAll(async ({
  browser: workerBrowser
}) => {
  browser = workerBrowser;
  await resetServer();
  tasks = ['parent', 'child', 'grandchild', 'standalone', 'archived'].map(id => ({
    id,
    name: id,
    project: 'nest',
    status: 'complete',
    archived: id === 'archived',
    needs_input: id === 'grandchild'
  }));
  role = (role_id, kind, task_id) => ({
    role_id,
    kind,
    task_id,
    live: true
  });
  roster = {
    orchestrators: [{
      id: 1,
      roles: [role(1, 'coordinator', 'parent'), role(2, 'worker', 'child'), role(3, 'worker', 'archived')]
    }, {
      id: 2,
      bridge_parent_orch_id: 1,
      bridge_parent_role_id: 2,
      roles: [role(4, 'coordinator', 'child'), role(5, 'worker', 'grandchild')]
    }]
  };
  row = (page, id) => page.locator(`.task-row[data-task-id="${id}"]`);
  disclosure = (page, id) => page.locator(`summary[data-parent-task-id="${id}"]`);
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
test('nested task disclosures hide every descendant, roll up input and survive refresh with keyboard focus', async () => {
  const page = await fixture();
  assert.equal(await disclosure(page, 'parent').count(), 1);
  assert.equal(await row(page, 'grandchild').isVisible(), true);
  const parentBox = await row(page, 'parent').boundingBox();
  const childBox = await row(page, 'child').boundingBox();
  const grandchildBox = await row(page, 'grandchild').boundingBox();
  assert.ok(childBox.x > parentBox.x && grandchildBox.x > childBox.x);
  await disclosure(page, 'child').focus();
  await page.keyboard.press('Enter');
  assert.equal(await row(page, 'grandchild').isVisible(), false);
  await disclosure(page, 'parent').click({
    position: {
      x: 2,
      y: 16
    }
  });
  assert.equal(await row(page, 'child').isVisible(), false);
  assert.equal(await row(page, 'standalone').isVisible(), true);
  assert.equal(await row(page, 'parent').locator('.task-status-icon').getAttribute('aria-label'), 'Descendant needs input');
  await disclosure(page, 'parent').focus();
  await refreshWorkspace(page);
  assert.equal(await disclosure(page, 'parent').evaluate(item => item === document.activeElement), true);
  assert.equal(await row(page, 'child').isVisible(), false);
  await page.keyboard.press('Space');
  assert.equal(await row(page, 'child').isVisible(), true);
  assert.equal(await row(page, 'grandchild').isVisible(), false);
  await disclosure(page, 'child').click({
    position: {
      x: 2,
      y: 16
    }
  });
  assert.equal(await row(page, 'grandchild').isVisible(), true);
  assert.equal(await row(page, 'archived').count(), 0);
  await page.locator('#archive-filter').click();
  assert.equal(await row(page, 'archived').isVisible(), true);
  assert.equal(await row(page, 'parent').count(), 0);
  await page.locator('#active-filter').click();
  await disclosure(page, 'parent').click({
    position: {
      x: 2,
      y: 16
    }
  });
  await page.locator('#search').fill('grandchild');
  assert.equal(await row(page, 'grandchild').isVisible(), true);
  await page.locator('#search').fill('');
  assert.equal(await row(page, 'grandchild').isVisible(), false);
  await page.reload();
  await row(page, 'child').waitFor();
  assert.equal(await row(page, 'child').isVisible(), true, 'disclosures reset on reload just like project groups');
});
test('roster failure leaves ordinary task navigation usable with a warning', async () => {
  const page = await fixture();
  await page.route('**/api/hera', route => route.fulfill({
    status: 500,
    json: {}
  }));
  await refreshWorkspace(page);
  assert.match(await page.locator('#hierarchy-warning').textContent(), /Task nesting unavailable/);
  assert.equal(await row(page, 'grandchild').isVisible(), true);
});
test('nested selection and archive actions stay separate from disclosure and history reveals ancestors', async () => {
  const page = await fixture();
  await row(page, 'child').focus();
  await page.keyboard.press('Enter');
  assert.equal(await page.locator('#task-title').textContent(), 'child');
  assert.equal(await row(page, 'grandchild').isVisible(), true);
  await row(page, 'grandchild').click();
  await row(page, 'standalone').click();
  await disclosure(page, 'parent').click({
    position: {
      x: 2,
      y: 16
    }
  });
  assert.equal(await row(page, 'grandchild').isVisible(), false);
  await page.goBack();
  await page.waitForFunction(() => document.querySelector('#task-title').textContent === 'grandchild');
  assert.equal(await row(page, 'grandchild').isVisible(), true);
  await page.getByRole('button', {
    name: 'Archive child',
    exact: true
  }).focus();
  await page.keyboard.press('Enter');
  assert.equal(await page.locator('#archive-dialog-title').textContent(), "You're about to archive child.");
  assert.equal(await page.locator('#task-title').textContent(), 'grandchild');
  await page.keyboard.press('Escape');
  assert.equal(await row(page, 'grandchild').isVisible(), true);
});
test('cross-project descendants follow their parent while unrelated projects retain grouping', async () => {
  const page = await fixture();
  await page.route('**/api/tasks?archived=all', route => route.fulfill({
    json: {
      tasks: tasks.map(task => task.id === 'child' ? {
        ...task,
        project: 'other-project'
      } : task)
    }
  }));
  await refreshWorkspace(page);
  assert.equal(await row(page, 'child').evaluate(item => item.closest('.project-group').querySelector('summary').dataset.project), 'nest');
  assert.equal(await page.locator('.project-count').count(), 0);
  assert.equal(await page.locator('.project-group[data-project="nest"] .task-row').count(), 4);
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
