import { test } from '@playwright/test';
import { resetServer } from '../../../_helpers';
import assert from 'node:assert/strict';
import { refreshWorkspace } from '../../_workspace-controls-fixture';
import { auditIconTooltips } from '../../shared/_icon-tooltip-audit.ts';
let browser, tasks;
const cleanup = [];
async function setup(width = 1280) {
  const page = await browser.newPage({
    viewport: {
      width,
      height: 900
    }
  });
  cleanup.push(() => page.close());
  await page.route('**/api/**', route => {
    const path = new URL(route.request().url()).pathname;
    let json = {};
    if (path === '/api/tasks') json = {
      tasks
    };else if (path === '/api/projects') json = {
      projects: [{
        name: 'fixture'
      }]
    };else if (path === '/api/hera') json = {
      orchestrators: [{
        id: 1,
        name: 'Fixture',
        pinned: true,
        roles: [{
          role_id: 1,
          kind: 'coordinator',
          name: 'coord',
          task_id: 'parent',
          live: true,
          status: 'working'
        }, {
          role_id: 2,
          kind: 'worker',
          name: 'worker',
          task_id: 'child',
          live: true,
          status: 'blocked'
        }]
      }]
    };else if (path === '/api/backends') json = {
      backends: [{
        name: 'fixture',
        kind: 'custom'
      }]
    };else if (path === '/api/accounts') json = [];else if (path.endsWith('/size')) json = {
      cols: 80,
      rows: 24
    };else if (path.endsWith('/output')) return route.fulfill({
      body: 'output\r\n',
      headers: {
        'X-Output-Total': '8'
      }
    });else if (path.endsWith('/stream') || path === '/api/events') return route.fulfill({
      contentType: 'text/event-stream',
      body: ':fixture\n\n'
    });else json = tasks.find(task => task.id === path.split('/')[3]) || {};
    return route.fulfill({
      json
    });
  });
  await page.goto(process.env.WORKSPACE_TEST_URL || 'http://127.0.0.1:17750');
  await page.locator('#token').fill('fixture');
  await page.locator('#connect-button').click();
  await page.locator('.task-row').first().waitFor({
    state: 'attached'
  });
  if (width < 700) await page.locator('#toggle-nav').click();
  return page;
}
test.beforeAll(async ({
  browser: workerBrowser
}) => {
  browser = workerBrowser;
  await resetServer();
  tasks = [{
    id: 'parent',
    name: 'Parent',
    pinned: true
  }, {
    id: 'child',
    name: 'Child',
    needs_input: true
  }, {
    id: 'pr',
    name: 'Pull request',
    pr_state: 'awaiting-review'
  }].map(task => ({
    ...task,
    project: 'fixture',
    status: 'in_progress',
    live: false,
    backend: 'fixture'
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
for (const width of [1280, 390]) test(`rail icons and single line footer at ${width}`, async () => {
  const page = await setup(width);
  await auditIconTooltips(page, 'body');
  const boxes = await page.locator('.nav-footer #connection-state,.nav-footer #shortcuts-help,.nav-footer #workspace-settings').evaluateAll(elements => elements.map(element => {
    const box = element.getBoundingClientRect();
    return {
      x: box.x,
      y: box.y + box.height / 2,
      right: box.right
    };
  }));
  assert.equal(boxes.length, 3);
  assert.ok(Math.max(...boxes.map(box => box.y)) - Math.min(...boxes.map(box => box.y)) <= 1);
  assert.ok(boxes[0].x < boxes[1].x && boxes[1].x < boxes[2].x);
  assert.ok(boxes[2].right <= width);
  const parent = page.locator('.task-row[data-task-id=parent]');
  assert.match(await parent.getAttribute('aria-label'), /Parent, Descendant needs input.*Pinned/);
  const disclosure = page.locator('.task-disclosure[data-parent-task-id="parent"]');
  const glyph = disclosure.locator(':scope > .task-caret'),
    bubble = page.getByRole('tooltip');
  await glyph.hover();
  await bubble.waitFor();
  assert.equal(await bubble.textContent(), 'Collapse 1 task');
  await glyph.click();
  assert.equal(await disclosure.getAttribute('data-tooltip'), 'Expand 1 task');
  await page.mouse.move(0, 0);
  await glyph.hover();
  await bubble.waitFor();
  assert.equal(await bubble.textContent(), 'Expand 1 task');
  await auditIconTooltips(page, 'body');
  await glyph.click();
  assert.equal(await disclosure.getAttribute('data-tooltip'), 'Collapse 1 task');
  const gear = page.locator('#workspace-settings');
  await gear.evaluate(element => {
    element.removeAttribute('data-tooltip');
    const hidden = document.createElement('span');
    hidden.className = 'sr-only';
    hidden.textContent = 'Settings';
    element.append(hidden);
  });
  await assert.rejects(() => auditIconTooltips(page, '#navigation'), /workspace-settings/);
  await gear.evaluate(element => {
    element.dataset.tooltip = 'Settings';
    element.querySelector('.sr-only').remove();
  });
  const redundant = await page.evaluate(() => {
    const button = document.createElement('button');
    button.id = 'redundant-audit';
    button.textContent = 'Redundant';
    button.dataset.tooltip = 'Redundant';
    document.querySelector('#navigation').append(button);
    return button.id;
  });
  await assert.rejects(() => auditIconTooltips(page, 'body'), /Redundant tooltip on redundant-audit/);
  await page.locator(`#${redundant}`).evaluate(element => element.remove());
  const status = page.locator('#connection-state');
  assert.equal(await status.getAttribute('data-tooltip'), 'Connected to Argus');
  await page.screenshot({
    path: `.context/rail-tooltips-${width}.png`
  });
  await page.locator('#shortcuts-help').click();
  await page.locator('#shortcuts-dialog').waitFor();
  await auditIconTooltips(page, 'body');
  await page.keyboard.press('Escape');
  await page.locator('#search').fill('Child');
  await page.locator('#search-clear').click();
  assert.equal(await page.locator('#search').inputValue(), '');
  await auditIconTooltips(page, '#navigation');
  await page.route('**/api/tasks?archived=all', route => route.fulfill({
    status: 503,
    json: {}
  }));
  await refreshWorkspace(page);
  await page.waitForFunction(() => document.querySelector('#connection-state').dataset.tooltip === 'Connection unavailable');
  assert.equal(await status.getAttribute('aria-label'), 'Connection unavailable');
  assert.equal(await status.getAttribute('data-state'), 'error');
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
