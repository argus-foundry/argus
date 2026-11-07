import { test, expect } from '@playwright/test';
import { resetServer } from '../../../_helpers';
import assert from 'node:assert/strict';
import { writeFile } from 'node:fs/promises';
let browser, idle;
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
  await page.goto(process.env.WORKSPACE_TEST_URL);
  await page.clock.install();
  await page.evaluate(async () => {
    document.querySelector('#connect-dialog').close();
    const {
      TaskTools
    } = await import('/tasks/tools.js');
    window.state = {
      result: {
        BranchFiles: 'M\tsrc/a.js\nM\tother/b.js',
        BranchDiff: ' src/a.js | 1 +\n other/b.js | 1 +\n 2 files changed, 2 insertions(+)'
      },
      reads: 0,
      diffs: 0,
      diffPaths: [],
      fail: false,
      pending: [],
      hold: false
    };
    window.tools = new TaskTools({
      json: async (path, {
        signal
      } = {}) => {
        if (path.endsWith('/git/status') || path.endsWith('/artifacts')) {
          state.reads++;
          if (state.hold) return new Promise(resolve => state.pending.push({
            resolve,
            signal
          }));
          if (state.fail) throw new Error('offline');
          return structuredClone(state.result);
        }
        if (path.includes('/git/diff')) {
          state.diffs++;
          state.diffPaths.push(new URL(path, location.href).searchParams.get('path'));
          return {
            Diff: 'diff --git a/a b/a\n@@ -0,0 +1 @@\n+new'
          };
        }
        return {};
      }
    }, {
      openDiff: (task, path) => {
        window.opened = path;
      }
    });
    tools.selectTask({
      id: 'a',
      worktree_path: '/tmp/fixture'
    });
  });
  await page.locator('.file-row').first().waitFor();
  return page;
}
test.beforeAll(async ({
  browser: workerBrowser
}) => {
  browser = workerBrowser;
  await resetServer();
  idle = page => page.evaluate(() => tools.event({
    task_id: 'a',
    type: 'session.idle'
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
test('idle and fallback update visible Changes without Refresh; failures offer Retry', async () => {
  const page = await fixture();
  assert.equal(await page.locator('.changes-refresh').count(), 0);
  await page.evaluate(() => {
    state.result = {
      BranchFiles: 'M\tsrc/a.js\nA\tnew-file.txt'
    };
  });
  await idle(page);
  await page.locator('[data-file-path="new-file.txt"].file-row').waitFor();
  await page.evaluate(() => {
    state.result = {
      BranchFiles: 'A\tpoll-file.txt'
    };
  });
  await page.clock.runFor(10001);
  await page.locator('[data-file-path="poll-file.txt"].file-row').waitFor();
  await page.evaluate(() => {
    state.fail = true;
  });
  await idle(page);
  await page.getByRole('button', {
    name: 'Retry',
    exact: true
  }).click();
  await page.evaluate(() => {
    state.fail = false;
    state.result = {};
  });
  await idle(page);
  await page.getByText('No changes yet', {
    exact: true
  }).waitFor();
  assert.equal(await page.getByRole('button', {
    name: 'Retry',
    exact: true
  }).count(), 0);
});
test('unchanged reads retain DOM, menu, expansion, scroll, counts and opened diff; changes retain view state', async () => {
  const page = await fixture();
  await page.getByRole('button', {
    name: 'Show tree view',
    exact: true
  }).click();
  await page.locator('.file-row[data-file-path="src/a.js"]').click();
  await page.getByRole('button', {
    name: 'File actions for src/a.js',
    exact: true
  }).click();
  await page.evaluate(() => {
    window.original = document.querySelector('.file-row');
    window.beforeDiffs = state.diffs;
    [...document.querySelectorAll('.file-directory')].find(node => node.querySelector('summary').textContent === 'other').open = false;
  });
  await idle(page);
  assert.equal(await page.evaluate(() => original === document.querySelector('.file-row')), true);
  assert.equal(await page.locator('.task-menu:popover-open').isVisible(), true);
  await page.evaluate(() => {
    state.result.BranchFiles += '\nA\tanother.txt';
  });
  await idle(page);
  await page.locator('[data-file-path="another.txt"].file-row').waitFor();
  assert.equal(await page.locator('.file-directory').filter({
    has: page.locator('summary', {
      hasText: 'other'
    })
  }).evaluate(node => node.open), false);
  assert.equal(await page.locator('.task-menu:popover-open').isVisible(), true);
  assert.equal(await page.evaluate(() => opened), 'src/a.js');
  await page.waitForFunction(() => state.diffs - beforeDiffs === 1);
  await page.evaluate(() => {
    state.result.BranchFiles = state.result.BranchFiles.replace('M\tsrc/a.js', 'A\tsrc/a.js');
  });
  await idle(page);
  assert.equal(await page.locator('.task-menu:popover-open').isVisible(), true);
  assert.equal(await page.locator('.file-row[data-file-path="src/a.js"] .file-status').textContent(), 'A');
});
test('hidden pane/document do not read; wake refreshes; task switches abort and ignore late results', async () => {
  const page = await fixture();
  const before = await page.evaluate(() => state.reads);
  await page.evaluate(() => document.querySelector('#workspace').classList.add('tools-hidden'));
  await idle(page);
  await page.clock.runFor(10001);
  assert.equal(await page.evaluate(() => state.reads), before);
  await page.evaluate(() => document.querySelector('#workspace').classList.remove('tools-hidden'));
  await page.waitForFunction(n => state.reads > n, before);
  await page.evaluate(() => {
    Object.defineProperty(document, 'hidden', {
      configurable: true,
      value: true
    });
    document.dispatchEvent(new Event('visibilitychange'));
  });
  const hidden = await page.evaluate(() => state.reads);
  await idle(page);
  await page.clock.runFor(10001);
  assert.equal(await page.evaluate(() => state.reads), hidden);
  await page.evaluate(() => {
    Object.defineProperty(document, 'hidden', {
      configurable: true,
      value: false
    });
    document.dispatchEvent(new Event('visibilitychange'));
  });
  await page.waitForFunction(n => state.reads > n, hidden);
  const blurred = await page.evaluate(() => {
    Object.defineProperty(document, 'hasFocus', {
      configurable: true,
      value: () => false
    });
    window.dispatchEvent(new Event('blur'));
    return state.reads;
  });
  await idle(page);
  await page.clock.runFor(10001);
  assert.equal(await page.evaluate(() => state.reads), blurred);
  await page.evaluate(() => {
    delete document.hasFocus;
    window.dispatchEvent(new Event('focus'));
  });
  await page.waitForFunction(n => state.reads > n, blurred);
  await page.evaluate(() => {
    state.hold = true;
    tools.event({
      task_id: 'a',
      type: 'session.idle'
    });
    tools.event({
      task_id: 'a',
      type: 'session.idle'
    });
  });
  assert.equal(await page.evaluate(() => state.pending.length), 1);
  await page.evaluate(() => {
    tools.selectTask({
      id: 'b'
    });
    state.pending[0].resolve({
      BranchFiles: 'A\tSTALE'
    });
  });
  assert.equal(await page.evaluate(() => state.pending[0].signal.aborted), true);
  await page.evaluate(() => state.pending[1].resolve({
    BranchFiles: 'A\tCURRENT'
  }));
  await page.locator('[data-file-path="CURRENT"].file-row').waitFor();
  assert.equal(await page.locator('[data-file-path="STALE"].file-row').count(), 0);
});
test('a real worktree file appears after session.idle through the app SSE wiring without polling', async () => {
  const response = await fetch(process.env.WORKSPACE_TEST_API + '/api/tasks?archived=all', {
    headers: {
      Authorization: 'Bearer test-token'
    }
  });
  const task = (await response.json()).tasks.find(task => task.name === 'echo-bash');
  assert.ok(task?.worktree_path.includes('/argus-test-home-'), 'isolated harness worktree required');
  const page = await browser.newPage({
    viewport: {
      width: 1440,
      height: 900
    }
  });
  cleanup.push(() => page.close());
  await page.addInitScript(() => {
    const original = window.fetch;
    window.fetch = (path, options) => String(path).startsWith('/api/events/stream') ? Promise.resolve(new Response(new ReadableStream({
      start(controller) {
        window.eventBytes = controller;
      }
    }), {
      headers: {
        'Content-Type': 'text/event-stream'
      }
    })) : original(path, options);
  });
  await page.goto(process.env.WORKSPACE_TEST_URL);
  await page.clock.install();
  await page.locator('#token').fill('test-token');
  await page.locator('#connect-button').click();
  await page.locator('.task-row').filter({
    hasText: 'echo-bash'
  }).click();
  await page.locator('.changes-heading').waitFor();
  const name = 'idle-proof-' + Date.now() + '.txt';
  await writeFile(task.worktree_path + '/' + name, 'real idle proof\n');
  await page.evaluate(({
    id
  }) => eventBytes.enqueue(new TextEncoder().encode('data: ' + JSON.stringify({
    type: 'session.idle',
    task_id: id
  }) + '\n\n')), task);
  await page.locator('.file-row').filter({
    hasText: name
  }).waitFor();
});
test('scroll remains stable across identical and changed results', async () => {
  const page = await fixture();
  await page.evaluate(() => {
    state.result = {
      BranchFiles: Array.from({
        length: 120
      }, (_, i) => 'M\tsrc/file-' + String(i).padStart(3, '0') + '.js').join('\n')
    };
  });
  await idle(page);
  await page.waitForFunction(() => document.querySelectorAll('.file-row').length === 120);
  await page.evaluate(() => document.querySelector('#tool-panel').scrollTop = 400);
  await idle(page);
  assert.equal(await page.locator('#tool-panel').evaluate(node => node.scrollTop), 400);
  await page.evaluate(() => {
    state.result.BranchFiles += '\nA\tzz-added.txt';
  });
  await idle(page);
  await page.waitForFunction(() => document.querySelectorAll('.file-row').length === 121);
  assert.equal(await page.locator('#tool-panel').evaluate(node => node.scrollTop), 400);
});
test('only a changed path summary invalidates counts; other paths retain their cache', async () => {
  const page = await fixture();
  await page.waitForFunction(() => state.diffs === 2);
  await page.evaluate(() => {
    state.diffPaths = [];
    window.stable = tools.changes.counts.results.get('other/b.js');
    state.result.BranchDiff = ' src/a.js | 2 ++\n other/b.js | 1 +\n 2 files changed, 3 insertions(+)';
  });
  await idle(page);
  await page.waitForFunction(() => state.diffPaths.length === 1);
  assert.deepEqual(await page.evaluate(() => state.diffPaths), ['src/a.js']);
  assert.equal(await page.evaluate(() => stable === tools.changes.counts.results.get('other/b.js')), true);
  assert.equal(await page.locator('.changes-totals').textContent(), '+3 −0');
});
test('initial failure retries and Artifacts uses the same visible live refresh lifecycle', async () => {
  const page = await fixture();
  await page.evaluate(() => {
    state.fail = true;
    tools.selectTask({
      id: 'failed'
    });
  });
  await page.getByRole('button', {
    name: 'Retry',
    exact: true
  }).waitFor();
  await page.evaluate(() => {
    state.fail = false;
    state.result = {};
  });
  await page.getByRole('button', {
    name: 'Retry',
    exact: true
  }).click();
  await page.getByText('No changes yet', {
    exact: true
  }).waitFor();
  await page.evaluate(() => {
    state.result = {
      artifacts: []
    };
  });
  await page.locator('#tool-artifacts').click();
  await page.getByText('No registered artifacts yet. Artifacts saved by this task appear here.', {
    exact: true
  }).waitFor();
  await page.evaluate(() => {
    state.result = {
      artifacts: [{
        name: 'Live artifact',
        filename: 'live.txt',
        type: 'text',
        size: 1
      }]
    };
    tools.event({
      task_id: 'failed',
      type: 'session.exited'
    });
  });
  await page.getByRole('button', {
    name: 'Live artifact',
    exact: false
  }).waitFor();
  await page.locator('#tool-status').click();
  const before = await page.evaluate(() => state.reads);
  await page.clock.runFor(10001);
  assert.equal(await page.evaluate(() => state.reads), before);
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});

for(const control of ['row','trigger','folder'])test(`live Changes restores focused ${control} identity`,async()=>{
  const page=await fixture();
  if(control==='folder')await page.getByRole('button',{name:'Show tree view',exact:true}).click();
  const target=control==='folder'?page.locator('summary[aria-label="src"]'):control==='trigger'?page.getByRole('button',{name:'File actions for src/a.js',exact:true}):page.locator('.file-row[data-file-path="src/a.js"]');
  await target.focus();
  await page.evaluate(()=>{state.result.BranchFiles='A\taaa.txt\nA\tsrc/a.js\nM\tother/b.js\nA\tsrc/new.js';});
  await idle(page);
  await expect(page.locator('.file-row[data-file-path="src/a.js"] .file-status')).toHaveText('A');
  await expect(target).toBeFocused();
  await page.keyboard.press('Enter');
  if(control==='row')await expect.poll(()=>page.evaluate(()=>opened)).toBe('src/a.js');
  if(control==='trigger')await expect(page.locator('.task-menu:popover-open')).toBeVisible();
  if(control==='folder')assert.equal(await target.locator('..').evaluate(node=>node.open),false);
});

test('live Changes uses neighbour and tab fallbacks without taking outside focus',async()=>{
  const page=await fixture();
  await page.locator('.file-row[data-file-path="src/a.js"]').focus();
  await page.evaluate(()=>{state.result={BranchFiles:'M\tother/b.js'};});await idle(page);
  await expect(page.locator('.file-row[data-file-path="other/b.js"]')).toBeFocused();
  await page.evaluate(()=>{state.result={};});await idle(page);
  await expect(page.locator('#tool-changes')).toBeFocused();
  await page.locator('#tab-agent').focus();
  await page.evaluate(()=>{state.result={BranchFiles:'A\tsrc/a.js'};});await idle(page);
  await expect(page.locator('.file-row')).toHaveCount(1);
  await expect(page.locator('#tab-agent')).toBeFocused();
});
