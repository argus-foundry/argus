import { test } from '@playwright/test';
import { resetServer } from '../../../_helpers';
import assert from 'node:assert/strict';
import { frontend } from '../../_lifecycle-fixture.ts';
let browser;
const cleanup = [];
async function app({
  historical = false,
  autoReplay = true
} = {}) {
  const page = await browser.newPage();
  const errors = [],
    reads = [],
    held = [],
    inputs = [];
  cleanup.push(async () => {
    for (const release of held.splice(0)) await release(503);
    await page.close();
    assert.deepEqual(errors, []);
  });
  page.on('pageerror', error => errors.push(error.message));
  page.setDefaultTimeout(5000);
  const tasks = [{
    id: 'a',
    name: 'Selected task',
    project: 'fixture',
    status: 'in_progress',
    live: true,
    idle: false,
    archived: false
  }, {
    id: 'b',
    name: 'Before event',
    project: 'fixture',
    status: 'complete',
    live: false,
    needs_input: false,
    archived: false
  }];
  await page.addInitScript(({
    historical,
    autoReplay
  }) => {
    window.EventSource = class extends EventTarget {
      constructor() {
        super();
        queueMicrotask(() => this.onopen?.({}));
      }
      close() {}
    };
    const fetch = window.fetch.bind(window);
    window.eventStreams = [];
    window.eventURLs = [];
    window.emit = (event, index = window.eventStreams.length - 1) => {
      window.eventStreams[index].enqueue(new TextEncoder().encode(`event: ${event.type}\ndata: ${JSON.stringify(event)}\n\n`));
    };
    window.fetch = (url, options) => {
      if (String(url).startsWith('/api/events/stream')) {
        window.eventURLs.push(String(url));
        return Promise.resolve(new Response(new ReadableStream({
          start(controller) {
            window.eventStreams.push(controller);
            if (historical || autoReplay && window.eventStreams.length > 1) window.emit({
              id: window.eventStreams.length,
              type: 'task.archived',
              task_id: 'a'
            });
          }
        }), {
          headers: {
            'Content-Type': 'text/event-stream'
          }
        }));
      }
      return fetch(url, options);
    };
  }, {
    historical,
    autoReplay
  });
  await page.route('**/api/**', async route => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith('/input')) {
      inputs.push({
        path,
        body: route.request().postData()
      });
      return route.fulfill({
        json: {}
      });
    }
    if (path === '/api/tasks') {
      reads.push(route.request().url());
      if (reads.length === 1) return route.fulfill({
        json: {
          tasks
        }
      });
      return new Promise(resolve => held.push(async (status = 200) => {
        await route.fulfill({
          status,
          json: status === 200 ? {
            tasks
          } : {
            error: 'Snapshot unavailable'
          }
        });
        resolve();
      }));
    }
    if (path.endsWith('/output')) return route.fulfill({
      body: '',
      headers: {
        'X-Output-Total': '0'
      }
    });
    const payload = path === '/api/projects' ? {
      projects: [{
        name: 'fixture'
      }]
    } : path === '/api/tasks/a' ? tasks[0] : path.endsWith('/size') ? {
      cols: 80,
      rows: 24
    } : {};
    return route.fulfill({
      json: payload
    });
  });
  await page.goto(frontend);
  await page.clock.install();
  await page.locator('#token').fill('fixture');
  await page.locator('#connect-button').click();
  await page.waitForFunction(() => !document.querySelector('#connect-dialog').open);
  return {
    page,
    reads,
    held,
    tasks,
    inputs
  };
}
async function waitHeld(held) {
  for (let i = 0; !held.length && i < 100; i++) await new Promise(resolve => setTimeout(resolve, 10));
  assert.ok(held.length, 'Expected a held authoritative task read');
}
async function emitChanges(page) {
  await page.evaluate(() => {
    window.emit({
      id: 10,
      type: 'task.renamed',
      task_id: 'b',
      payload: {
        to: 'After event'
      }
    });
    window.emit({
      id: 11,
      type: 'session.needs_input',
      task_id: 'b',
      payload: {
        needs_input: true
      }
    });
  });
  // A subsequent browser task lets the stream reader consume both envelopes.
  await page.evaluate(() => new Promise(resolve => queueMicrotask(resolve)));
}
async function assertBuffered(page) {
  assert.equal(await page.locator('.task-name').filter({
    hasText: 'After event'
  }).count(), 0);
  assert.equal(await page.title(), 'Theia');
  assert.equal(await page.locator('.attention-alert').count(), 0);
}
async function assertReconciled(page) {
  await page.locator('.task-name').filter({
    hasText: 'After event'
  }).waitFor();
  assert.equal(await page.title(), '(1) Theia');
  assert.equal(await page.locator('.task-row').filter({
    hasText: 'After event'
  }).getByLabel('Needs input', {
    exact: true
  }).count(), 1);
  assert.equal(await page.locator('.attention-alert').count(), 0, 'Buffered attention must never replay alerts');
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
test('real app fences a periodic snapshot while retaining newer named events and silent badges', async () => {
  const {
    page,
    held,
    reads
  } = await app();
  await page.clock.runFor(30001);
  await waitHeld(held);
  await emitChanges(page);
  await assertBuffered(page);
  await held.shift()();
  await assertReconciled(page);
  assert.equal(reads.length, 2);
  assert.equal(await page.evaluate(() => window.eventStreams.length), 1, 'Ordinary refresh must keep the original connection');
});
test('real app retains the failed snapshot fence through a stale successful replacement', async () => {
  const {
    page,
    held,
    reads
  } = await app();
  await page.clock.runFor(30001);
  await waitHeld(held);
  await held.shift()(503);
  await page.waitForFunction(() => document.querySelector('#connection-banner').textContent.includes('Task refresh failed'));
  await emitChanges(page);
  await assertBuffered(page);
  await page.clock.runFor(30000);
  await waitHeld(held);
  await held.shift()();
  await assertReconciled(page);
  assert.equal(reads.length, 3);
  assert.equal(await page.evaluate(() => window.eventStreams.length), 1, 'Failed snapshot recovery must not reconnect');
});
for (const historical of [true, false]) test(`archive replay cannot override a restored task on ${historical ? 'initial connect' : 'outage reconnect'}`, async () => {
  const {
    page,
    held
  } = await app({
    historical
  });
  const assertRestored = async () => {
    await page.locator('.task-row').filter({
      hasText: 'Selected task'
    }).waitFor();
    await page.locator('#draft').fill('Unsent eligibility proof');
    await page.waitForFunction(() => !document.querySelector('#send').disabled);
    assert.equal(await page.locator('#send').isEnabled(), true);
  };
  await page.clock.runFor(101);
  if (historical) {
    await waitHeld(held);
    await held.shift()();
  }
  await assertRestored();
  // The old archive is the only archive-state event: restore emitted no inverse.
  await page.clock.runFor(101);
  if (held.length) await held.shift()();
  await page.evaluate(() => window.eventStreams.at(-1).error(new Error('Outage')));
  await page.locator('#connection-banner:not([hidden])').waitFor();
  await page.getByRole('button', {
    name: 'Retry connection',
    exact: true
  }).click();
  await waitHeld(held);
  await held.shift()();
  await page.waitForFunction(() => !document.querySelector('#connection-banner button').disabled);
  await assertRestored();
  assert.deepEqual(await page.evaluate(() => window.eventURLs), ['/api/events/stream?since=0', `/api/events/stream?since=${historical ? 1 : 0}`]);
});
test('a live archive reconciles actual archive state and retains events during its read', async () => {
  const {
    page,
    held,
    tasks
  } = await app();
  await page.locator('#draft').fill('Keep this draft');
  await page.waitForFunction(() => !document.querySelector('#send').disabled);
  tasks[0].archived = true;
  await page.evaluate(() => window.emit({
    id: 5,
    type: 'task.archived',
    task_id: 'a'
  }));
  await page.clock.runFor(101);
  await waitHeld(held);
  await emitChanges(page);
  await assertBuffered(page);
  await held.shift()();
  await assertReconciled(page);
  assert.equal(await page.locator('.task-row').filter({
    hasText: 'Selected task'
  }).count(), 0);
  assert.equal(await page.locator('#send').isDisabled(), true);
  assert.equal(await page.locator('#draft').inputValue(), 'Keep this draft');
});
for (const split of [false, true]) for (const failed of [false, true]) test(`live archive blocks ${split ? 'split' : 'primary'} input while reconciliation ${failed ? 'fails' : 'is held'}`, async () => {
  const {
    page,
    held,
    tasks,
    inputs
  } = await app();
  const target = tasks[0];
  if (split) {
    await page.locator('.task-row').filter({
      hasText: 'Before event'
    }).click();
    await page.locator('#split-toggle').click();
    await page.locator('#secondary-task-picker').selectOption('a');
  }
  const host = split ? '#secondary-terminal' : '#terminal';
  const state = split ? '#secondary-terminal-state' : '#terminal-state';
  await page.waitForFunction(selector => document.querySelector(selector).textContent === 'Live terminal', state);
  // Positive control proves this exact terminal sends real raw input before archive.
  await Promise.all([page.waitForRequest(request => request.url().endsWith('/input')), page.locator(host + ' .xterm-helper-textarea').press('p')]);
  assert.deepEqual(inputs, [{
    path: '/api/tasks/a/input',
    body: 'p'
  }]);
  inputs.length = 0;
  if (!split) {
    await page.locator('#draft').fill('Retained archive draft');
    assert.equal(await page.locator('#send').isEnabled(), true);
  }
  target.archived = true;
  await page.evaluate(() => window.emit({
    id: 5,
    type: 'task.archived',
    task_id: 'a'
  }));
  await page.clock.runFor(101);
  await waitHeld(held);
  if (failed) {
    await held.shift()(503);
    await page.waitForFunction(() => document.querySelector('#connection-banner').textContent.includes('Task refresh failed'));
  }
  // Exercise raw typing before checking presentation, so a failure also detects unsafe POSTs.
  await page.locator(host + ' .xterm-helper-textarea').press('x');
  await page.clock.runFor(20);
  await page.evaluate(() => new Promise(resolve => queueMicrotask(resolve)));
  assert.deepEqual(inputs, [], 'Archived terminals must send no raw input while archive state is unresolved');
  assert.equal(await page.locator('.task-row').filter({
    hasText: 'Selected task'
  }).count(), 1, 'Pending task stays in Active');
  if (!split) {
    assert.equal(await page.locator('#send').isDisabled(), true);
    assert.equal(await page.locator('#draft').inputValue(), 'Retained archive draft');
  }
  if (failed) {
    target.archived = false;
    await page.clock.runFor(30000);
    await waitHeld(held);
    await held.shift()();
    await page.locator('.task-row').filter({
      hasText: 'Selected task'
    }).waitFor();
    await page.waitForFunction(selector => document.querySelector(selector).textContent === 'Live terminal', state);
    await Promise.all([page.waitForRequest(request => request.url().endsWith('/input')), page.locator(host + ' .xterm-helper-textarea').press('r')]);
    assert.deepEqual(inputs, [{
      path: '/api/tasks/a/input',
      body: 'r'
    }], 'Only an authoritative restored task enables input again');
  }
});
test('a live archive during a periodic snapshot immediately blocks input and survives its stale baseline', async () => {
  const {
    page,
    held,
    inputs,
    tasks
  } = await app();
  await page.waitForFunction(() => document.querySelector('#terminal-state').textContent === 'Live terminal');
  await page.locator('#draft').fill('Retained snapshot draft');
  await page.clock.runFor(30001);
  await waitHeld(held);
  await page.evaluate(() => window.emit({
    id: 5,
    type: 'task.archived',
    task_id: 'a'
  }));
  await page.locator('#terminal .xterm-helper-textarea').press('x');
  await page.clock.runFor(20);
  assert.deepEqual(inputs, []);
  assert.equal(await page.locator('#send').isDisabled(), true);
  assert.equal(await page.locator('.task-row').filter({
    hasText: 'Selected task'
  }).count(), 1);
  // A read started before the event cannot move the pending row, even if it returns archived=true.
  tasks[0].archived = true;
  await emitChanges(page);
  await held.shift()();
  await assertReconciled(page);
  assert.equal(await page.locator('#send').isDisabled(), true);
  assert.equal(await page.locator('.task-row').filter({
    hasText: 'Selected task'
  }).count(), 1);
  await page.clock.runFor(101);
  await waitHeld(held);
  await held.shift()();
  await page.locator('.task-row').filter({
    hasText: 'Selected task'
  }).waitFor({
    state: 'detached'
  });
});
for (const split of [false, true]) test(`archive during reconnect fences ${split ? 'split' : 'primary'} input through held and failed reads`, async () => {
  const {
    page,
    held,
    tasks,
    inputs
  } = await app({
    autoReplay: false
  });
  if (split) {
    await page.locator('.task-row').filter({
      hasText: 'Before event'
    }).click();
    await page.locator('#split-toggle').click();
    await page.locator('#secondary-task-picker').selectOption('a');
  }
  const host = split ? '#secondary-terminal' : '#terminal';
  const state = split ? '#secondary-terminal-state' : '#terminal-state';
  await page.waitForFunction(selector => document.querySelector(selector).textContent === 'Live terminal', state);
  await Promise.all([page.waitForRequest(request => request.url().endsWith('/input')), page.locator(host + ' .xterm-helper-textarea').press('p')]);
  assert.deepEqual(inputs, [{
    path: '/api/tasks/a/input',
    body: 'p'
  }]);
  inputs.length = 0;
  if (!split) await page.locator('#draft').fill('Retained reconnect draft');
  const row = page.locator('.task-row').filter({
    hasText: 'Selected task'
  });
  const order = await page.locator('.task-row').evaluateAll(rows => rows.map(row => row.dataset.taskId));
  await page.evaluate(() => window.eventStreams.at(-1).error(new Error('Outage')));
  await page.getByRole('button', {
    name: 'Retry connection',
    exact: true
  }).click();
  await waitHeld(held);
  tasks[0].archived = true;
  await page.evaluate(() => window.emit({
    id: 5,
    type: 'task.archived',
    task_id: 'a'
  }));
  for (const key of ['x', 'f']) {
    if (key === 'f') {
      await held.shift()(503);
      await page.waitForFunction(() => document.querySelector('#connection-banner').textContent.includes('Task refresh failed'));
    }
    await page.locator(host + ' .xterm-helper-textarea').press(key);
    await page.clock.runFor(20);
    assert.deepEqual(inputs, [], 'Unresolved archives send no raw input');
    assert.equal(await row.count(), 1);
    assert.deepEqual(await page.locator('.task-row').evaluateAll(rows => rows.map(row => row.dataset.taskId)), order);
    assert.equal(await row.getByLabel('Archive pending', {
      exact: true
    }).count(), 1);
    if (!split) assert.equal(await page.locator('#send').isDisabled(), true);
  }
  await page.clock.runFor(101);
  await waitHeld(held);
  await held.shift()();
  await row.waitFor({
    state: 'detached'
  });
});
for (const reconnect of [false, true]) test(`delayed archive history after ${reconnect ? 'reconnect' : 'initial'} restored baseline clears only on authoritative read`, async () => {
  const {
    page,
    held,
    inputs
  } = await app({
    autoReplay: false
  });
  if (reconnect) {
    await page.evaluate(() => window.eventStreams.at(-1).error(new Error('Outage')));
    await page.getByRole('button', {
      name: 'Retry connection',
      exact: true
    }).click();
    await waitHeld(held);
    await held.shift()();
    await page.waitForFunction(() => !document.querySelector('#connection-banner button').disabled);
  }
  await page.waitForFunction(() => document.querySelector('#terminal-state').textContent === 'Live terminal');
  await page.locator('#draft').fill('Restored draft');
  await page.evaluate(() => window.emit({
    id: 5,
    type: 'task.archived',
    task_id: 'a'
  }));
  await page.clock.runFor(101);
  await waitHeld(held);
  await page.locator('#terminal .xterm-helper-textarea').press('x');
  await page.clock.runFor(20);
  assert.deepEqual(inputs, []);
  assert.equal(await page.locator('#send').isDisabled(), true);
  const row = page.locator('.task-row').filter({
    hasText: 'Selected task'
  });
  assert.equal(await row.count(), 1);
  await held.shift()(503);
  await page.waitForFunction(() => document.querySelector('#connection-banner').textContent.includes('Task refresh failed'));
  assert.equal(await row.getByLabel('Archive pending', {
    exact: true
  }).count(), 1);
  await page.clock.runFor(30000);
  await waitHeld(held);
  await held.shift()();
  await page.waitForFunction(() => !document.querySelector('#send').disabled);
  assert.equal(await row.count(), 1);
  assert.equal(await row.getByLabel('Archive pending', {
    exact: true
  }).count(), 0);
  await Promise.all([page.waitForRequest(request => request.url().endsWith('/input')), page.locator('#terminal .xterm-helper-textarea').press('r')]);
  assert.deepEqual(inputs, [{
    path: '/api/tasks/a/input',
    body: 'r'
  }]);
});
test('a second archive during reconciliation cannot be cleared by the older read', async () => {
  const {
    page,
    held,
    inputs
  } = await app({
    autoReplay: false
  });
  await page.locator('#draft').fill('Revision fence');
  await page.waitForFunction(() => !document.querySelector('#send').disabled);
  await page.evaluate(() => window.emit({
    id: 5,
    type: 'task.archived',
    task_id: 'a'
  }));
  await page.clock.runFor(101);
  await waitHeld(held);
  await page.evaluate(() => window.emit({
    id: 6,
    type: 'task.archived',
    task_id: 'a'
  }));
  await held.shift()();
  // Observe a separate named event to ensure the old snapshot has drained.
  await page.evaluate(() => window.emit({
    id: 7,
    type: 'task.renamed',
    task_id: 'b',
    payload: {
      to: 'Read drained'
    }
  }));
  await page.locator('.task-name').filter({
    hasText: 'Read drained'
  }).waitFor();
  assert.equal(await page.locator('#send').isDisabled(), true);
  assert.equal(await page.getByLabel('Archive pending', {
    exact: true
  }).count(), 1);
  await page.locator('#terminal .xterm-helper-textarea').press('x');
  await page.clock.runFor(20);
  assert.deepEqual(inputs, []);
  await page.clock.runFor(101);
  await waitHeld(held);
  await held.shift()();
  await page.waitForFunction(() => !document.querySelector('#send').disabled);
  assert.equal(await page.getByLabel('Archive pending', {
    exact: true
  }).count(), 0);
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
