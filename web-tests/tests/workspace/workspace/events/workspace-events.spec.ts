import { test } from '@playwright/test';
import { resetServer } from '../../../_helpers';
import assert from 'node:assert/strict';
import { actorPage } from '../../_lifecycle-fixture.ts';
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
test('held baseline precedes buffered live patches and reconnect uses latest received cursor', async () => {
  const page = await actorPage(browser, cleanup, '<header class="workspace-header"></header>');
  const result = await page.evaluate(async () => {
    const {
      WorkspaceEvents
    } = await import('/workspace/events/workspace-events.js');
    const {
      patchEvent
    } = await import('/workspace/events/workspace-events-policy.js');
    const urls = [],
      renders = [],
      streams = [];
    let tasks = [],
      release,
      reconnectRelease;
    let baselineReads = 0;
    const baseline = new Promise(resolve => {
      release = resolve;
    });
    const events = new WorkspaceEvents({
      request: async url => {
        urls.push(url);
        return new Response(new ReadableStream({
          start(controller) {
            streams.push(controller);
          }
        }), {
          headers: {
            'Content-Type': 'text/event-stream'
          }
        });
      }
    }, {
      event: event => {
        tasks = patchEvent(tasks, event).tasks;
        renders.push(tasks[0]?.name || 'No task');
      },
      refresh: async () => {
        const loaded = ++baselineReads === 1 ? await baseline : await new Promise(resolve => {
          reconnectRelease = resolve;
        });
        tasks = events.snapshot(loaded);
        renders.push(tasks[0].name);
      }
    });
    const tick = () => new Promise(resolve => setTimeout(resolve, 0));
    const emit = async (stream, id, name) => {
      stream.enqueue(new TextEncoder().encode(`event: task.renamed\ndata: ${JSON.stringify({
        id,
        type: 'task.renamed',
        task_id: 'a',
        payload: {
          to: name
        }
      })}\n\n`));
      await tick();
    };
    await events.start();
    const initial = events.callbacks.refresh();
    await emit(streams[0], 31, 'Buffered first');
    const beforeBaseline = [...renders];
    release([{
      id: 'a',
      name: 'Old baseline'
    }]);
    await initial;
    await emit(streams[0], 44, 'Latest live');
    const reconnect = events.reconnect();
    await tick();
    await emit(streams[1], 46, 'Buffered reconnect');
    const duringReconnect = [...renders];
    reconnectRelease([{
      id: 'a',
      name: 'Reconnect baseline'
    }]);
    await reconnect;
    await emit(streams[1], 43, 'Duplicate replay');
    await emit(streams[1], 47, 'After reconnect');
    events.stop();
    streams.forEach(stream => stream.close());
    return {
      urls,
      renders,
      beforeBaseline,
      duringReconnect
    };
  });
  assert.deepEqual(result.beforeBaseline, []);
  assert.deepEqual(result.urls, ['/api/events/stream?since=0', '/api/events/stream?since=44']);
  assert.deepEqual(result.duringReconnect, ['Buffered first', 'Latest live']);
  assert.deepEqual(result.renders, ['Buffered first', 'Latest live', 'Buffered reconnect', 'After reconnect']);
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
