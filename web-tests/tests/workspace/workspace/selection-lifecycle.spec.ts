import { test } from '@playwright/test';
import { resetServer } from '../../_helpers';
import assert from 'node:assert/strict';
import { frontend } from '../_lifecycle-fixture.ts';
let browser;
const cleanup = [];
async function fixture(viewport, live = false) {
  const page = await browser.newPage({
    viewport
  });
  const mutations = [],
    requests = [],
    errors = [],
    resizeGuards = [];
  const tasks = ['a', 'b'].map(id => ({
    id,
    name: `Task ${id}`,
    project: 'fixture',
    status: live ? 'in_progress' : 'complete',
    live,
    idle: id === 'a'
  }));
  const sizes = {
    a: {
      cols: 80,
      rows: 24
    },
    b: {
      cols: 80,
      rows: 24
    }
  };
  cleanup.push(async () => {
    await page.close();
    assert.deepEqual(errors, []);
  });
  page.on('pageerror', error => errors.push(error.message));
  page.on('request', request => {
    if (new URL(request.url()).pathname.startsWith('/api/')) {
      requests.push(request.url());
      if (!['GET', 'HEAD', 'OPTIONS'].includes(request.method())) mutations.push({
        method: request.method(),
        path: new URL(request.url()).pathname,
        body: request.postData()
      });
    }
  });
  await page.addInitScript(() => {
    window.EventSource = class extends EventTarget {
      constructor() {
        super();
        queueMicrotask(() => this.onopen?.({}));
      }
      close() {}
    };
    const original = window.fetch;
    window.eventStreams = [];
    window.heldReads = 0;
    window.fetch = (url, options) => {
      if (window.holdTasks && String(url).startsWith('/api/tasks?')) {
        window.heldReads++;
        return new Promise(resolve => {
          window.releaseTasks = () => resolve(new Response(JSON.stringify({
            tasks: [{
              id: 'late',
              name: 'LATE TASK',
              project: 'fixture'
            }]
          }), {
            headers: {
              'Content-Type': 'application/json'
            }
          }));
        });
      }
      if (String(url).includes('/api/events/stream')) return Promise.resolve(new Response(new ReadableStream({
        start(controller) {
          window.eventStreams.push({
            controller,
            signal: options.signal
          });
        }
      }), {
        headers: {
          'Content-Type': 'text/event-stream'
        }
      }));
      return original(url, options);
    };
  });
  await page.route('**/api/**', async route => {
    const path = new URL(route.request().url()).pathname;
    let payload = {};
    if (path === '/api/tasks') payload = {
      tasks
    };else if (path.includes('/projects')) payload = {
      projects: [{
        name: 'fixture'
      }]
    };else if (path.endsWith('/git/status')) payload = {
      Status: ' M file.go'
    };else if (path.endsWith('/git/diff')) payload = {
      Diff: 'diff --git a/file.go b/file.go\n@@ -0,0 +1 @@\n+Selected diff'
    };else if (/^\/api\/tasks\/[ab]$/.test(path)) payload = tasks.find(task => task.id === path.split('/').at(-1));else if (path.endsWith('/size')) payload = sizes[path.split('/')[3]];else if (path.endsWith('/resize')) {
      const id = path.split('/')[3];
      resizeGuards.push(await page.evaluate(() => ({
        task: document.querySelector('.task-row[aria-current="page"]')?.dataset.taskId,
        hidden: document.querySelector('#agent-panel').hidden
      })));
      sizes[id] = route.request().postDataJSON();
      payload = {
        rerendered: false
      };
    } else if (path.endsWith('/output')) return route.fulfill({
      body: '',
      headers: {
        'X-Output-Total': '0'
      }
    });
    return route.fulfill({
      json: payload
    });
  });
  await page.goto(frontend);
  await page.locator('#token').fill('fixture');
  await page.locator('#connect-button').click();
  await page.locator('#task-title').filter({
    hasText: 'Task a'
  }).waitFor();
  return {
    page,
    mutations,
    requests,
    resizeGuards
  };
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
for (const live of [false, true]) test(`task and review switches permit only guarded resize mutations (${live ? 'live' : 'offline'})`, async () => {
  const {
    page,
    mutations,
    resizeGuards
  } = await fixture({
    width: 1440,
    height: 900
  }, live);
  if (live) await page.waitForResponse(response => new URL(response.url()).pathname.endsWith('/resize'));
  await page.locator('#draft').fill('Retained a draft');
  await page.locator('.task-row[data-task-id="b"]').click();
  await page.locator('#tool-changes').click();
  await page.locator('.file-row').first().click();
  await page.locator('.diff-table').waitFor();
  await page.getByRole('button', {
    name: 'Split',
    exact: true
  }).click();
  await page.locator('.task-row[data-task-id="a"]').click();
  assert.equal(await page.locator('#draft').inputValue(), 'Retained a draft');
  await page.locator('#tool-changes').click();
  await page.locator('.file-row').first().click();
  await page.locator('.diff-table').waitFor();
  await page.locator('#tab-agent').click();
  await page.waitForTimeout(650);
  assert.deepEqual(mutations.filter(write => !/^\/api\/tasks\/[ab]\/resize$/.test(write.path)), []);
  if (!live) assert.deepEqual(mutations, []);else {
    assert.ok(mutations.length > 0, 'Eligible idle terminal should exercise the resize exception');
    assert.ok(resizeGuards.every(guard => guard.task === 'a' && !guard.hidden));
    assert.ok(mutations.every(write => write.path === '/api/tasks/a/resize'));
  }
  for (const write of mutations) {
    assert.equal(write.method, 'POST');
    const geometry = JSON.parse(write.body);
    assert.ok(geometry.cols > 0 && geometry.rows > 0);
  }
});
test('compact drawer controls and backdrop close either rail during agent focus', async () => {
  const {
    page
  } = await fixture({
    width: 390,
    height: 844
  });
  await page.locator('#focus-agent').click();
  for (const rail of ['nav', 'tools']) for (const close of ['control', 'backdrop']) {
    await page.locator(`#toggle-${rail}`).click();
    assert.equal(await page.locator(`#toggle-${rail}`).getAttribute('aria-expanded'), 'true');
    assert.equal(await page.locator('#focus-agent').getAttribute('aria-pressed'), 'false');
    assert.equal(await page.locator('#main').evaluate(node => node.inert), true);
    if (close === 'control') await page.locator(`[data-close="${rail}"]`).click();else await page.locator('#backdrop').click({
      position: {
        x: rail === 'nav' ? 385 : 2,
        y: 500
      }
    });
    assert.equal(await page.locator(`#toggle-${rail}`).getAttribute('aria-expanded'), 'false');
    assert.equal(await page.locator(`#toggle-${rail}`).evaluate(node => document.activeElement === node), true);
    assert.equal(await page.locator('#main').evaluate(node => node.inert), false);
    assert.equal(await page.locator('#backdrop').isVisible(), false);
    await page.locator('#focus-agent').click();
  }
});
for (const failed of [false, true]) test(`disconnect fences held refresh and ${failed ? 'scheduled retry' : 'pending stream read'}`, async () => {
  const {
    page,
    requests,
    mutations
  } = await fixture({
    width: 1440,
    height: 900
  });
  await page.clock.install();
  await page.evaluate(() => {
    window.holdTasks = true;
  });
  await page.evaluate(() => window.eventStreams[0].controller.error(new Error('Lost stream')));
  await page.locator('#connection-banner').waitFor();
  // Retry opens a new held stream and then starts a held reconciliation read.
  await page.getByRole('button', {
    name: 'Retry connection',
    exact: true
  }).click();
  await page.waitForFunction(() => window.heldReads === 1);
  if (failed) {
    await page.evaluate(() => window.eventStreams[1].controller.error(new Error('Lost replacement stream')));
    await page.locator('#connection-banner').waitFor();
  }
  await page.locator('#disconnect').click();
  const before = requests.length,
    writes = mutations.length;
  await page.evaluate(() => window.releaseTasks());
  if (!failed) await page.evaluate(() => {
    window.eventStreams[1].controller.enqueue(new TextEncoder().encode('data: {"id":99,"type":"task.renamed","task_id":"a","payload":{"to":"LATE TASK"}}\n\n'));
    window.eventStreams[1].controller.close();
  });
  await page.clock.runFor(65000);
  assert.equal(requests.length, before);
  assert.equal(await page.evaluate(() => window.heldReads), 1);
  assert.equal(mutations.length, writes);
  assert.equal(await page.evaluate(() => window.eventStreams.length), 2);
  assert.equal(await page.evaluate(() => window.eventStreams.every(stream => stream.signal.aborted)), true);
  assert.equal(await page.locator('.task-row').count(), 0);
  assert.equal(await page.locator('#connection-banner').isVisible(), false);
  assert.equal(await page.locator('#connect-dialog').evaluate(dialog => dialog.open), true);
  assert.doesNotMatch(await page.locator('body').textContent(), /LATE TASK/);
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
