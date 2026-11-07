import { pathToFileURL } from 'node:url';
import { test } from '@playwright/test';
import { resetServer } from '../../_helpers';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
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
test('real xterms stay bounded, dispose listeners and rebuild evicted output at a fresh cursor', async () => {
  const page = await actorPage(browser, cleanup, '<div id="main"></div><div id="split"></div>');
  await page.addScriptTag({
    url: '/vendor/xterm.js'
  });
  const result = await page.evaluate(async () => {
    const {
      TaskTerminalCache
    } = await import('/terminal/task-terminal-cache.js');
    const NativeTerminal = window.Terminal,
      instances = [],
      writes = new Map(),
      controllers = [],
      visibility = new Set();
    window.Terminal = class extends NativeTerminal {
      constructor(options) {
        super(options);
        instances.push(this);
        writes.set(this, []);
      }
      write(bytes, callback) {
        writes.get(this).push(...bytes);
        return super.write(bytes, callback);
      }
      dispose() {
        this.disposed = true;
        return super.dispose();
      }
    };
    const add = document.addEventListener.bind(document),
      remove = document.removeEventListener.bind(document);
    document.addEventListener = (kind, fn, ...args) => {
      if (kind === 'visibilitychange') visibility.add(fn);
      return add(kind, fn, ...args);
    };
    document.removeEventListener = (kind, fn, ...args) => {
      if (kind === 'visibilitychange') visibility.delete(fn);
      return remove(kind, fn, ...args);
    };
    window.EventSource = class {
      constructor(url) {
        this.url = url;
        queueMicrotask(() => this.onopen?.());
      }
      addEventListener() {}
      close() {
        this.closed = true;
      }
    };
    const bytes = new TextEncoder(),
      snapshots = new Map();
    let history = 'old\r\n';
    const api = {
      json: async path => path.endsWith('/size') ? {
        cols: 80,
        rows: 24
      } : {
        id: 'a',
        status: 'in_progress',
        live: true,
        idle: false
      },
      streamUrl: path => path,
      request: async path => {
        const id = path.split('/')[3],
          text = id === 'a' ? history : `${id}\r\n`;
        snapshots.set(id, (snapshots.get(id) || 0) + 1);
        return new Response(bytes.encode(text), {
          headers: {
            'X-Output-Total': String(bytes.encode(text).length)
          }
        });
      }
    };
    const {
      AgentTerminal
    } = await import('/terminal/terminal.js');
    const cache = new TaskTerminalCache(api, document.querySelector('#main'), () => {}, () => {}, (...args) => {
      const terminal = new AgentTerminal(...args);
      controllers.push(terminal);
      return terminal;
    });
    cache.show(false);
    const task = id => ({
      id,
      status: 'in_progress',
      live: true,
      idle: false
    });
    await cache.attach(task('a'));
    const old = cache.current,
      oldTerm = old.term,
      oldStream = old.stream,
      oldSource = old.stream.source,
      oldOutput = old.output,
      oldInput = old.input;
    await cache.attachSecondary(task('protected'), document.querySelector('#split'));
    const protectedTerm = cache.secondaryTerminal;
    const liveCounts = [],
      protectedAlive = [];
    for (let i = 0; i < 20; i++) {
      await cache.attach(task('t' + i));
      liveCounts.push(instances.filter(term => !term.disposed).length);
      protectedAlive.push(!!protectedTerm.term);
    }
    const evicted = !cache.records.has('a'),
      disposed = oldTerm.disposed === true,
      cleaned = !oldStream.active && oldSource.closed && !oldOutput.active && old.geometry === null && old.visibilityChanged === null && oldInput.signal.aborted;
    history = '\u001b[31mnew café\u001b[0m\r\nlast';
    await cache.attach(task('a'));
    const fresh = cache.current;
    const replay = [...writes.get(fresh.term)],
      expected = [...bytes.encode(history)];
    const cursor = fresh.stream.cursor,
      source = fresh.stream.source.url;
    const tail = bytes.encode('\r\nstream Ω');
    fresh.stream.source.onmessage({
      data: btoa(String.fromCharCode(...tail))
    });
    await new Promise(resolve => fresh.term.write(new Uint8Array(), resolve));
    const streamed = [...writes.get(fresh.term)],
      streamExpected = [...expected, ...tail],
      streamCursor = fresh.stream.cursor;
    const lines = Array.from({
      length: 3
    }, (_, i) => fresh.term.buffer.active.getLine(i).translateToString(true));
    const listenersBefore = visibility.size;
    cache.destroy();
    return {
      liveCounts,
      protectedAlive,
      evicted,
      disposed,
      cleaned,
      replay,
      expected,
      cursor,
      source,
      streamed,
      streamExpected,
      streamCursor,
      lines,
      snapshots: snapshots.get('a'),
      listenersBefore,
      listenersAfter: visibility.size,
      remaining: instances.filter(term => !term.disposed).length
    };
  });
  assert.ok(result.liveCounts.every(count => count <= 8), JSON.stringify(result.liveCounts));
  assert.ok(result.protectedAlive.every(Boolean));
  assert.equal(result.evicted, true);
  assert.equal(result.disposed, true);
  assert.equal(result.cleaned, true);
  assert.deepEqual(result.replay, result.expected);
  assert.equal(result.cursor, result.expected.length);
  assert.match(result.source, new RegExp('since=' + result.expected.length + '$'));
  assert.deepEqual(result.streamed, result.streamExpected);
  assert.equal(result.streamCursor, result.streamExpected.length);
  assert.deepEqual(result.lines, ['new café', 'last', 'stream Ω']);
  assert.equal(result.snapshots, 2);
  assert.equal(result.listenersBefore, 8);
  assert.equal(result.listenersAfter, 0);
  assert.equal(result.remaining, 0);
});
test('evicting a terminal leaves its unsent composer draft intact', async () => {
  const page = await actorPage(browser, cleanup, await readFile(new URL("./../../../../web-workspace/index.html", pathToFileURL(__filename).href), 'utf8'));
  const tasks = Array.from({
    length: 12
  }, (_, i) => ({
    id: 'draft-' + i,
    name: 'Draft task ' + i,
    project: 'fixture',
    status: 'complete',
    live: false
  }));
  const snapshots = new Map();
  await page.route('**/api/**', route => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith('/output')) {
      const id = path.split('/')[3];
      snapshots.set(id, (snapshots.get(id) || 0) + 1);
      return route.fulfill({
        body: 'output',
        headers: {
          'X-Output-Total': '6'
        }
      });
    }
    const payload = path === '/api/tasks' ? {
      tasks
    } : path === '/api/projects/full' ? {
      projects: [{
        name: 'fixture'
      }]
    } : path.endsWith('/size') ? {
      cols: 80,
      rows: 24
    } : tasks.find(task => path === '/api/tasks/' + task.id) || {};
    return route.fulfill({
      json: payload
    });
  });
  await page.locator('#token').fill('fixture');
  await page.locator('#connect-button').click();
  const choose = async i => {
    await page.locator('.task-row').filter({
      has: page.getByText(tasks[i].name, {
        exact: true
      })
    }).click();
    await page.waitForFunction(() => document.querySelector('#terminal-state').textContent === 'Recorded output');
  };
  await choose(0);
  await page.locator('#draft').fill('unsent draft survives eviction');
  for (let i = 1; i < 12; i++) await choose(i);
  await choose(0);
  assert.equal(snapshots.get('draft-0'), 2, 'revisit must create a fresh terminal snapshot');
  assert.equal(await page.locator('#draft').inputValue(), 'unsent draft survives eviction');
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
