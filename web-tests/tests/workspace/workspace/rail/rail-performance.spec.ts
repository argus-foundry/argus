import { test } from '@playwright/test';
import { resetServer } from '../../../_helpers';
import assert from 'node:assert/strict';
import { frontend } from '../../_lifecycle-fixture.ts';
let browser;
const cleanup = [];
async function setup(roster = {}) {
  const page = await browser.newPage();
  const errors = [],
    reads = [];
  cleanup.push(async () => {
    await page.close();
    assert.deepEqual(errors, []);
  });
  page.on('pageerror', error => errors.push(error.message));
  const tasks = Array.from({
    length: 100
  }, (_, i) => ({
    id: `task${i}`,
    name: `Task ${i}`,
    project: 'fixture',
    status: 'complete',
    live: false,
    idle: true,
    pinned: false,
    archived: false
  }));
  await page.addInitScript(() => {
    const fetch = window.fetch.bind(window);
    window.fetch = (url, options) => String(url).startsWith('/api/events/stream') ? Promise.resolve(new Response(new ReadableStream({
      start(controller) {
        window.streamStarts = (window.streamStarts || 0) + 1;
        window.stream = controller;
      }
    }), {
      headers: {
        'Content-Type': 'text/event-stream'
      }
    })) : fetch(url, options);
    window.emit = events => window.stream.enqueue(new TextEncoder().encode(events.map(event => `data: ${JSON.stringify(event)}\n\n`).join('')));
  });
  await page.route('**/api/**', route => {
    const path = new URL(route.request().url()).pathname;
    reads.push(path);
    if (path.endsWith('/output')) return route.fulfill({
      body: '',
      headers: {
        'X-Output-Total': '0'
      }
    });
    const json = path === '/api/tasks' ? {
      tasks
    } : path === '/api/projects/full' ? {
      projects: [{
        name: 'fixture'
      }]
    } : path === '/api/hera' ? roster : path.endsWith('/size') ? {
      cols: 80,
      rows: 24
    } : tasks.find(task => path === `/api/tasks/${task.id}`) || {};
    return route.fulfill({
      json
    });
  });
  await page.goto(frontend);
  await page.clock.install();
  await page.evaluate(async () => {
    const {
      TaskNavigation
    } = await import('/workspace/rail/tasks.js');
    const update = TaskNavigation.prototype.update;
    TaskNavigation.prototype.update = function (...args) {
      window.navigation = this;
      return update.apply(this, args);
    };
  });
  await page.locator('#token').fill('fixture');
  await page.locator('#connect-button').click();
  await page.locator('.task-row').first().waitFor();
  await page.clock.runFor(100);
  await page.evaluate(() => {
    window.rebuilds = 0;
    const groups = document.querySelector('#task-groups'),
      replace = groups.replaceChildren;
    groups.replaceChildren = function (...args) {
      window.rebuilds++;
      return replace.apply(this, args);
    };
  });
  return {
    page,
    reads
  };
}
async function burst(page, type, first = 1) {
  await page.evaluate(({
    type,
    first
  }) => window.emit(Array.from({
    length: 20
  }, (_, i) => ({
    id: first + i,
    type,
    task_id: 'task99',
    payload: {
      to: `Renamed ${i}`
    }
  }))), {
    type,
    first
  });
  await page.clock.runFor(32);
  return page.evaluate(() => window.rebuilds);
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
for (const type of ['session.focus', 'session.idle']) test(`100-task rail skips 20 unchanged ${type} events`, async () => {
  const {
    page
  } = await setup();
  assert.equal(await burst(page, type), 0);
});
test('100-task rail coalesces changed event bursts and preserves focus, selection and disclosure', async () => {
  const {
    page
  } = await setup();
  await page.locator('summary[data-project=fixture]').click();
  await page.locator('summary[data-project=fixture]').focus();
  await page.evaluate(() => {
    window.rebuilds = 0;
  });
  assert.equal(await burst(page, 'task.renamed'), 1);
  assert.equal(await page.locator('.task-name').last().textContent(), 'Renamed 19');
  assert.equal(await page.locator('details.project-group').evaluate(element => element.open), false);
  assert.equal(await page.locator('summary[data-project=fixture]').evaluate(element => element === document.activeElement), true);
  assert.equal(await page.locator('.task-row[aria-current=page]').getAttribute('data-task-id'), 'task0');
});
test('unchanged roster snapshots retain rail nodes', async () => {
  const {
    page
  } = await setup();
  await page.evaluate(() => {
    window.originalRow = document.querySelector('.task-row');
    for (let i = 0; i < 12; i++) window.navigation.update(structuredClone(window.navigation.tasks), window.navigation.selected, undefined, {
      roster: {}
    });
  });
  await page.clock.runFor(32);
  assert.equal(await page.evaluate(() => window.rebuilds), 0);
  assert.equal(await page.evaluate(() => window.originalRow === document.querySelector('.task-row')), true);
});
test('twelve unchanged five-second roster polls retain the full rail', async () => {
  const roster = {
    orchestrators: [{
      id: 1,
      roles: [{
        role_id: 1,
        task_id: 'task99',
        kind: 'worker',
        live: true,
        status: 'working'
      }]
    }]
  };
  const {
    page,
    reads
  } = await setup(roster);
  const before = reads.filter(path => path === '/api/hera').length;
  for (let i = 0; i < 12; i++) {
    const next = reads.filter(path => path === '/api/hera').length;
    await page.clock.runFor(5001);
    for (let attempt = 0; attempt < 50 && reads.filter(path => path === '/api/hera').length === next; attempt++) await new Promise(resolve => setTimeout(resolve, 10));
  }
  assert.ok(reads.filter(path => path === '/api/hera').length >= before + 12, 'Exercise real roster timer reads');
  assert.equal(await page.evaluate(() => window.rebuilds), 0);
});
test('hidden 60-second polling makes no reads and visibility return refreshes each source once', async () => {
  const {
    page,
    reads
  } = await setup();
  await page.evaluate(() => {
    window.hidden = true;
    Object.defineProperty(document, 'hidden', {
      configurable: true,
      get: () => window.hidden
    });
    document.dispatchEvent(new Event('visibilitychange'));
  });
  const before = reads.length;
  for (let i = 0; i < 4; i++) {
    await page.clock.runFor(15001);
    await new Promise(resolve => setTimeout(resolve, 50));
  }
  const hidden = reads.slice(before);
  assert.deepEqual(hidden, []);
  await page.evaluate(() => {
    window.hidden = false;
    document.dispatchEvent(new Event('visibilitychange'));
  });
  await page.waitForFunction(() => document.querySelector('#connection-state').dataset.state === 'connected');
  // Wait for the two independent refreshes to finish through their API responses.
  for (let i = 0; i < 50 && reads.length < before + 8; i++) await new Promise(resolve => setTimeout(resolve, 10));
  const visible = reads.slice(before);
  assert.equal(visible.filter(path => path === '/api/tasks').length, 1);
  assert.equal(visible.filter(path => path === '/api/projects/full').length, 1);
  assert.equal(visible.filter(path => path === '/api/hera').length, 2);
  for (const path of ['/api/tasks/task0/bridge', '/api/tasks/task0/meta', '/api/accounts']) assert.equal(visible.filter(value => value === path).length, 1, path);
  assert.equal(await page.evaluate(() => window.streamStarts), 1, 'Visibility return keeps the existing SSE stream');
  await page.locator('#disconnect').click();
  const stopped = reads.length;
  await page.evaluate(() => document.dispatchEvent(new Event('visibilitychange')));
  await page.clock.runFor(60001);
  assert.equal(reads.length, stopped, 'Disconnected poll owners must ignore wake and timer callbacks');
});
test('changed role inputs repaint, unchanged roster metadata does not, and clear cancels a queued frame', async () => {
  const {
    page
  } = await setup();
  await page.clock.resume();
  const result = await page.evaluate(async () => {
    const nav = window.navigation;
    const roster = {
      orchestrators: [{
        id: 1,
        roles: [{
          role_id: 1,
          task_id: 'task99',
          kind: 'worker',
          status: 'working',
          live: true
        }]
      }]
    };
    nav.update(nav.tasks, nav.selected, undefined, {
      roster
    });
    await new Promise(requestAnimationFrame);
    const first = window.rebuilds;
    nav.update(nav.tasks, nav.selected, undefined, {
      roster: {
        ...roster,
        updated_at: 'irrelevant'
      }
    });
    await new Promise(requestAnimationFrame);
    const same = window.rebuilds;
    roster.orchestrators[0].roles[0].status = 'blocked';
    nav.update(nav.tasks, nav.selected, undefined, {
      roster
    });
    await new Promise(requestAnimationFrame);
    return {
      first,
      same,
      changed: window.rebuilds,
      tag: document.querySelector('[data-task-id=task99] .task-role-tag').textContent
    };
  });
  assert.deepEqual(result, {
    first: 1,
    same: 1,
    changed: 2,
    tag: 'blocked'
  });
  await page.evaluate(() => {
    const nav = window.navigation;
    nav.update(nav.tasks.map(task => ({
      ...task,
      name: 'Pending rename'
    })), nav.selected, undefined, undefined, {
      defer: true
    });
    nav.clear();
  });
  await page.clock.runFor(32);
  assert.equal(await page.locator('.task-row').count(), 0);
});
test('non-rail task data retains nodes while row selection and settings use the latest record', async () => {
  const {
    page
  } = await setup();
  await page.evaluate(() => {
    const nav = window.navigation;
    nav.onSelect = task => {
      window.selectedPrompt = task.prompt;
    };
    nav.actions.taskSettings = task => {
      window.settingsPrompt = task.prompt;
    };
    nav.update(nav.tasks.map(task => task.id === 'task99' ? {
      ...task,
      prompt: 'latest prompt',
      result: 'unrendered result'
    } : task), nav.selected);
  });
  assert.equal(await page.evaluate(() => window.rebuilds), 0);
  await page.locator('.task-more[data-task-id=task99]').click();
  await page.locator('[data-task-id=task99][data-task-action=settings]').click();
  assert.equal(await page.evaluate(() => window.settingsPrompt), 'latest prompt');
  await page.locator('.task-row[data-task-id=task99]').click();
  assert.equal(await page.evaluate(() => window.selectedPrompt), 'latest prompt');
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
