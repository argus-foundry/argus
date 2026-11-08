import { test } from '@playwright/test';
import { resetServer } from '../../_helpers';
import assert from 'node:assert/strict';
import { actorPage } from '../_lifecycle-fixture.ts';
let browser;
const cleanup = [];
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
for (const split of [false, true]) for (const mode of ['recovery', 'resume-tail', 'resume-replace']) {
  test(`archive fence survives held ${mode} in ${split ? 'split' : 'primary'} terminal`, async () => {
    const page = await actorPage(browser, cleanup, '<div id="main"></div><div id="split"></div>');
    await page.addScriptTag({
      url: '/vendor/xterm.js'
    });
    const result = await page.evaluate(async ({
      split,
      mode
    }) => {
      const {
        TaskTerminalCache
      } = await import('/terminal/task-terminal-cache.js');
      const {
        patchEvent
      } = await import('/workspace/events/workspace-events-policy.js');
      const live = {
        id: 'a',
        status: 'in_progress',
        live: true,
        idle: false,
        archived: false
      };
      let tasks = [live],
        release,
        held = false;
      const posts = [];
      window.EventSource = class {
        constructor() {
          queueMicrotask(() => this.onopen?.());
        }
        addEventListener() {}
        close() {}
      };
      const api = {
        inputTask: id => tasks.find(task => task.id === id),
        streamUrl: path => path,
        json: async path => path.endsWith('/size') ? {
          cols: mode === 'resume-replace' && held ? 81 : 80,
          rows: 24
        } : {
          ...live
        },
        request: async (path, options) => {
          if (path.endsWith('/input')) {
            posts.push(options.body);
            return new Response('{}');
          }
          if (held) await new Promise(resolve => {
            release = resolve;
          });
          return new Response('', {
            headers: {
              'X-Output-Total': '0'
            }
          });
        }
      };
      const cache = new TaskTerminalCache(api, document.querySelector('#main'));
      cache.show(false);
      if (split) {
        await cache.attach({
          ...live,
          id: 'b'
        });
        await cache.attachSecondary(live, document.querySelector('#split'));
      } else await cache.attach(live);
      const terminal = split ? cache.secondaryTerminal : cache.current;
      await terminal.input.raw('positive');
      posts.length = 0;
      if (mode !== 'recovery') terminal.pauseOutput();
      held = true;
      const pending = mode === 'recovery' ? terminal.stream.refresh({
        ...live
      }) : terminal.resumeOutput();
      while (!release) await new Promise(resolve => setTimeout(resolve, 0));
      tasks = patchEvent(tasks, {
        type: 'task.archived',
        task_id: 'a'
      }).tasks;
      await terminal.attach(tasks[0]);
      release();
      await pending;
      await new Promise(resolve => setTimeout(resolve, 0));
      let refused = false;
      try {
        await terminal.input.raw('x');
      } catch {
        refused = true;
      }
      const result = {
        posts,
        refused,
        pending: terminal.task.archive_pending,
        interactive: terminal.interactive
      };
      await new Promise(resolve => setTimeout(resolve, 100));
      cache.destroy();
      return result;
    }, {
      split,
      mode
    });
    assert.deepEqual(result.posts, [], 'No input POST before workspace reconciliation');
    assert.equal(result.refused, true);
    assert.equal(result.pending, true);
    assert.equal(result.interactive, false);
  });
}
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
