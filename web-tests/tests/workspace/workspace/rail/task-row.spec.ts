import { test } from '@playwright/test';
import { resetServer } from '../../../_helpers';
import assert from 'node:assert/strict';
let browser, tasks;
const cleanup = [];
async function fixture(reducedMotion = 'no-preference') {
  const page = await browser.newPage({
    reducedMotion
  });
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
      projects: []
    }
  }));
  await page.route('**/api/hera', route => route.fulfill({
    json: {
      orchestrators: [{
        id: 1,
        roles: [{
          role_id: 1,
          kind: 'worker',
          task_id: 'failed',
          live: true,
          status: 'failed'
        }]
      }]
    }
  }));
  await page.goto(process.env.WORKSPACE_TEST_URL || 'http://127.0.0.1:17750');
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
  tasks = [{
    id: 'running',
    status: 'in_progress',
    live: true,
    pr_state: 'awaiting-review'
  }, {
    id: 'busy-review',
    status: 'in_review',
    live: true,
    idle: false
  }, {
    id: 'idle',
    status: 'in_progress',
    live: true,
    idle: true
  }, {
    id: 'pending',
    status: 'pending'
  }, {
    id: 'open',
    status: 'in_progress',
    idle: true,
    pr_state: 'awaiting-review'
  }, {
    id: 'review',
    status: 'in_review'
  }, {
    id: 'approved',
    status: 'in_review',
    pr_state: 'approved'
  }, {
    id: 'merged',
    status: 'in_review',
    pr_state: 'merged-closed'
  }, {
    id: 'complete',
    status: 'complete'
  }, {
    id: 'attention',
    status: 'in_progress',
    live: true,
    needs_input: true
  }, {
    id: 'failed',
    status: 'in_review'
  }, {
    id: 'long',
    status: 'pending',
    name: 'A very long task name that must truncate and never wrap onto another line '.repeat(4)
  }].map(task => ({
    name: task.id,
    project: 'compact',
    elapsed: '45m',
    ...task
  }));
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
test('task rail uses one compact line with accessible status icons and truncated names', async () => {
  const page = await fixture();
  assert.equal(await page.locator('.task-row .task-status-icon').count(), tasks.length);
  assert.equal(await page.locator('.task-row .task-state, .task-row .task-age').count(), 0);
  const expected = {
    running: ['working', 'Running'],
    idle: ['neutral', 'Waiting'],
    pending: ['neutral', 'Pending'],
    open: ['pr-open', 'PR open'],
    review: ['neutral', 'In review'],
    'busy-review': ['working', 'Running'],
    approved: ['pr-approved', 'PR approved'],
    merged: ['merged', 'PR merged or closed'],
    complete: ['merged', 'Complete'],
    attention: ['attention', 'Needs input'],
    failed: ['failed', 'Failed']
  };
  for (const [id, [kind, label]] of Object.entries(expected)) {
    const row = page.locator(`.task-row[data-task-id="${id}"]`),
      icon = row.locator('.task-status-icon');
    assert.equal(await icon.getAttribute('data-status'), kind);
    assert.equal(await icon.getAttribute('aria-label'), label);
    assert.equal(await icon.getAttribute('role'), 'img');
    const name = tasks.find(task => task.id === id).name,
      tag = id === 'failed' ? 'failed' : '';
    assert.equal(await row.locator('.task-name').textContent(), name);
    assert.equal(await row.locator('.task-role-tag').count(), tag ? 1 : 0);
    assert.equal(await row.textContent(), name + tag);
    assert.ok((await row.boundingBox()).height <= 38, 'Task row must remain one compact line');
  }
  const long = page.locator('.task-row[data-task-id="long"]');
  assert.equal(await long.getAttribute('data-tooltip'), tasks.at(-1).name);
  assert.equal(await long.getAttribute('title'), null);
  assert.deepEqual(await long.locator('.task-name').evaluate(name => ({
    ellipsis: getComputedStyle(name).textOverflow,
    wrap: getComputedStyle(name).whiteSpace,
    truncated: name.scrollWidth > name.clientWidth
  })), {
    ellipsis: 'ellipsis',
    wrap: 'nowrap',
    truncated: true
  });
  const colors = await page.locator('.task-status-icon').evaluateAll(icons => Object.fromEntries(icons.map(icon => [icon.dataset.status, getComputedStyle(icon).color])));
  assert.notEqual(colors.neutral, colors['pr-open']);
  assert.notEqual(colors.merged, colors['pr-open']);
  assert.notEqual(colors.failed, colors.attention);
  const spinner = page.locator('.task-row[data-task-id="running"] [data-status="working"]');
  assert.notEqual(await spinner.evaluate(icon => getComputedStyle(icon).animationName), 'none');
  const strokes = spinner.locator('.task-scribe-line, .task-scribe-nib');
  assert.equal(await strokes.count(), 4);
  for (const stroke of await strokes.all()) {
    assert.notEqual(await stroke.evaluate(element => getComputedStyle(element).animationName), 'none');
    assert.deepEqual(await stroke.evaluate(element => element.getAnimations().map(animation => ({
      state: animation.playState,
      duration: animation.effect.getTiming().duration
    }))), [{ state: 'running', duration: 5600 }]);
  }
  const samples = await spinner.evaluate(icon => {
    const animations = icon.getAnimations({ subtree: true });
    animations.forEach(animation => animation.pause());
    return [0, 1400, 2688, 3976].map(time => {
      animations.forEach(animation => { animation.currentTime = time; });
      return {
        offsets: [...icon.querySelectorAll('.task-scribe-line')].map(line => Number.parseFloat(getComputedStyle(line).strokeDashoffset)),
        nib: getComputedStyle(icon.querySelector('.task-scribe-nib')).transform,
      };
    });
  });
  assert.deepEqual(samples[0].offsets, [10, 10, 10]);
  for (let line = 0; line < 3; line++) {
    assert.equal(samples[line + 1].offsets[line], 0, `Scribe line ${line + 1} must finish writing`);
    assert.notEqual(samples[0].offsets[line], samples[line + 1].offsets[line]);
  }
  assert.equal(new Set(samples.map(sample => sample.nib)).size, samples.length, 'Scribe nib must move between writing positions');
});
test('reduced motion stops the task status spinner', async () => {
  const page = await fixture('reduce');
  const icon = page.locator('.task-row[data-task-id="running"] .task-status-icon');
  assert.equal(await icon.count(), 1);
  assert.equal(await icon.evaluate(icon => getComputedStyle(icon).animationName), 'none');
  const descendants = icon.locator('*');
  assert.ok(await descendants.count() > 0);
  assert.ok((await descendants.evaluateAll(elements => elements.map(element => getComputedStyle(element).animationName))).every(name => name === 'none'));
  assert.equal(await icon.evaluate(element => element.getAnimations({ subtree: true }).length), 0);
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
