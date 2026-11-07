import { test } from '@playwright/test';
import { resetServer } from '../../_helpers';
import { refreshWorkspace } from '../_workspace-controls-fixture.ts';
import assert from 'node:assert/strict';
let browser;
const cleanup = [];
async function fixture({
  width = 1728,
  idle = false
} = {}) {
  const page = await browser.newPage({
    viewport: {
      width,
      height: 1000
    }
  });
  cleanup.push(() => page.close());
  const errors = [];
  page.on('pageerror', e => errors.push(e.message));
  cleanup.push(() => assert.deepEqual(errors, []));
  await page.addInitScript(() => {
    window.terminalSources = [];
    window.EventSource = class extends EventTarget {
      constructor(url) {
        super();
        this.url = url;
        window.terminalSources.push(this);
        queueMicrotask(() => {
          if (!this.closed) this.onopen?.({});
        });
      }
      close() {
        this.closed = true;
      }
    };
  });
  const state = {
    tasks: [{
      id: 'auto-a',
      name: 'Auto A',
      project: 'fixture',
      status: 'in_progress',
      live: true,
      idle
    }, {
      id: 'record-b',
      name: 'Record B',
      project: 'fixture',
      status: 'complete',
      live: false
    }],
    size: {
      cols: 150,
      rows: 80
    },
    calls: [],
    waiters: [],
    inFlight: 0,
    maxInFlight: 0
  };
  state.wait = count => state.calls.length >= count ? Promise.resolve() : new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error('Expected ' + count + ' resize calls; got ' + state.calls.length)), 6000);
    state.waiters.push(() => {
      if (state.calls.length >= count) {
        clearTimeout(timer);
        resolve();
      }
    });
  });
  await page.route('**/api/**', async route => {
    const request = route.request(),
      path = new URL(request.url()).pathname;
    if (path === '/api/tasks') return route.fulfill({
      json: {
        tasks: state.tasks
      }
    });
    if (path === '/api/projects/full') return route.fulfill({
      json: {
        projects: []
      }
    });
    if (path === '/api/hera') return route.fulfill({
      json: {
        orchestrators: []
      }
    });
    if (path.endsWith('/size')) {
      const size = state.attachSizeOverride || state.size;
      state.attachSizeOverride = null;
      return route.fulfill({
        json: size
      });
    }
    if (path.endsWith('/output')) {
      const body = state.output ?? Array.from({
        length: 300
      }, (_, i) => String(i).padStart(3, '0') + 'W'.repeat(146) + 'Z').join('\r\n');
      state.outputReads = (state.outputReads || 0) + 1;
      return route.fulfill({
        body,
        headers: {
          'X-Output-Total': String(Buffer.byteLength(body))
        }
      });
    }
    if (path === '/api/tasks/auto-a') {
      state.detailStarted?.();
      if (state.detailGate) await state.detailGate;
      return route.fulfill({
        json: {
          ...state.tasks[0],
          ...(state.detailBusy ? {
            idle: false,
            needs_input: false
          } : {})
        }
      });
    }
    if (path.endsWith('/input')) {
      (state.inputs ??= []).push(request.postData());
      return route.fulfill({
        json: {}
      });
    }
    if (path.endsWith('/resize')) {
      state.inFlight++;
      state.maxInFlight = Math.max(state.maxInFlight, state.inFlight);
      const size = request.postDataJSON();
      state.calls.push(size);
      state.waiters.forEach(notify => notify());
      if (state.resizeGate) await state.resizeGate;
      state.size = size;
      await route.fulfill({
        json: {
          ...size,
          rerendered: state.rerendered || false
        }
      });
      state.inFlight--;
      return;
    }
    return route.fulfill({
      status: 404,
      json: {}
    });
  });
  await page.goto(process.env.WORKSPACE_TEST_URL || 'http://127.0.0.1:17750');
  await page.locator('#token').fill('fixture');
  await page.locator('#connect-button').click();
  await page.locator('.task-row').filter({
    hasText: 'Auto A'
  }).click();
  await page.waitForFunction(() => document.querySelector('#terminal-state').textContent === 'Live terminal');
  return {
    page,
    state
  };
}
async function geometry(page, selector = '.cached-terminal') {
  await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
  return page.evaluate(selector => {
    const cache = document.querySelector(selector),
      viewport = cache.querySelector('.xterm-viewport'),
      screen = cache.querySelector('.xterm-screen');
    const box = element => element.getBoundingClientRect(),
      bar = viewport.offsetWidth - viewport.clientWidth;
    const ancestors = [];
    for (let parent = cache.parentElement; parent && parent.id !== 'main'; parent = parent.parentElement) ancestors.push(parent);
    const owners = [...ancestors, cache, ...cache.querySelectorAll('.terminal-host,.xterm,.xterm-viewport')].filter(element => /auto|scroll/.test(getComputedStyle(element).overflowY) && element.scrollHeight > element.clientHeight + 1).map(element => element.id || element.className);
    const glyphs = [...cache.querySelectorAll('.xterm-rows>div')].flatMap(row => {
      const walker = document.createTreeWalker(row, NodeFilter.SHOW_TEXT);
      let node, last;
      while (node = walker.nextNode()) if (node.textContent.trim()) last = node;
      if (!last) return [];
      const range = document.createRange();
      range.setStart(last, last.textContent.length - 1);
      range.setEnd(last, last.textContent.length);
      return [{
        right: range.getBoundingClientRect().right,
        rowRight: box(row).right
      }];
    });
    return {
      glyphEdge: Math.max(...glyphs.map(glyph => glyph.right)),
      clippedGlyph: glyphs.some(glyph => glyph.right > glyph.rowRight + .5),
      owners,
      helperRight: box(cache.querySelector('.xterm-helper-textarea')).right,
      scrollWidth: cache.scrollWidth,
      clientWidth: cache.clientWidth,
      edge: box(screen).right,
      available: Math.min(box(cache).left + cache.clientWidth, box(viewport).left + viewport.clientWidth) - Math.max(0, 16 - bar),
      root: document.scrollingElement.scrollWidth,
      width: innerWidth
    };
  }, selector);
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
for (const width of [1280, 1728, 2544, 3440]) test(`one vertical scrollbar and an unobscured final column before/after Fit at ${width}`, async () => {
  const {
    page,
    state
  } = await fixture({
    width
  });
  for (const fitted of [false, true]) {
    if (fitted) {
      const response = page.waitForResponse(r => new URL(r.url()).pathname.endsWith('/resize'));
      await page.locator('#fit-session').click();
      await response;
      await page.waitForFunction(() => document.querySelector('#fit-session').dataset.fitNeeded === 'false');
    }
    await page.locator('.cached-terminal').evaluate(element => {
      element.scrollLeft = element.scrollWidth;
    });
    const result = await geometry(page);
    assert.deepEqual(result.owners, ['xterm-viewport'], JSON.stringify(result));
    assert.equal(result.clippedGlyph, false, 'Last glyph must clear its renderer row: ' + JSON.stringify(result));
    assert.ok(result.glyphEdge <= result.available - 3, 'Painted glyph clears scrollbar and gutter: ' + JSON.stringify(result));
    assert.ok(result.edge <= result.available - 3, JSON.stringify(result));
    assert.ok(result.root <= result.width, JSON.stringify(result));
    if (fitted) {
      assert.ok(result.scrollWidth <= result.clientWidth, 'Fitting must remove horizontal overflow: ' + JSON.stringify(result));
      assert.ok(result.helperRight <= result.edge + 1, 'Input anchor follows resized cells: ' + JSON.stringify(result));
    }
  }
  assert.equal(state.calls.length, 1, 'Busy state permits only the explicit manual fit');
});
for (const order of ['response before delayed exit', 'exit before response']) test(`rerendered Fit recovers replacement with ${order}`, async () => {
  const {
    page,
    state
  } = await fixture();
  const terminalState = page.locator('#terminal-state'),
    draft = page.locator('#draft');
  await draft.fill('held while replacing');
  const oldCursor = await page.evaluate(() => new URL(window.terminalSources.find(source => source.url.includes('/auto-a/stream')).url, location.href).searchParams.get('since'));
  assert.ok(Number(oldCursor) > 1000);
  let release;
  state.resizeGate = new Promise(resolve => {
    release = resolve;
  });
  await page.locator('#fit-session').click();
  await state.wait(1);
  state.rerendered = true;
  state.tasks[0].live = false;
  const exit = () => page.evaluate(() => {
    const source = window.terminalSources.find(source => source.url.includes('/auto-a/stream'));
    if (!source.closed) source.dispatchEvent(new MessageEvent('exit', {
      data: JSON.stringify({
        rerendering: true
      })
    }));
  });
  const response = page.waitForResponse(r => new URL(r.url()).pathname.endsWith('/resize'));
  if (order === 'exit before response') await exit();
  const offline = page.waitForResponse(async r => new URL(r.url()).pathname === '/api/tasks/auto-a' && (await r.json()).live === false);
  release();
  await response;
  await page.waitForFunction(() => document.querySelector('#terminal-state').textContent === 'Waiting for terminal rerender…');
  assert.equal(await page.locator('#send').isDisabled(), true, 'Recovery disables Send before the old exit is delivered');
  await draft.press('Enter');
  await page.locator('.xterm-helper-textarea').press('x');
  await page.waitForFunction(() => window.terminalSources.find(source => source.url.includes('/auto-a/stream')).closed);
  // Wait for a real offline recovery poll, without refreshing workspace metadata.
  await offline;
  assert.deepEqual(state.inputs || [], [], 'Neither composer submission nor raw typing reaches the replacing process');
  assert.equal(state.outputReads, 1, 'Offline replacement cannot replay old output');
  assert.equal(await terminalState.textContent(), 'Waiting for terminal rerender…');
  if (order === 'response before delayed exit') await exit();
  const replacement = 'REPLACEMENT ✓';
  state.output = replacement;
  state.tasks[0].live = true;
  await page.waitForFunction(() => document.querySelector('#terminal-state').textContent === 'Live terminal');
  const painted = await page.locator('.xterm-rows').innerText();
  assert.equal(painted.trim(), replacement, 'Replacement clears old process history without duplicating new bytes');
  assert.equal(await page.locator('#send').isDisabled(), false);
  assert.equal(await draft.inputValue(), 'held while replacing');
  const input = page.waitForRequest(request => new URL(request.url()).pathname.endsWith('/input'));
  await page.locator('.xterm-helper-textarea').press('y');
  assert.equal((await input).postData(), 'y');
  const sources = await page.evaluate(() => window.terminalSources.filter(source => source.url.includes('/auto-a/stream')).map(source => ({
    url: source.url,
    closed: !!source.closed
  })));
  assert.equal(sources.length, 2, 'Resize response and old exit share one recovery');
  assert.equal(new URL(sources[1].url, 'http://fixture').searchParams.get('since'), String(Buffer.byteLength(replacement)), 'Replacement resets the byte cursor, including UTF-8 bytes');
  assert.equal(state.outputReads, 2);
  assert.equal(state.calls.length, 1);
  await page.evaluate(() => {
    const source = window.terminalSources.filter(source => source.url.includes('/auto-a/stream')).at(-1);
    source.onmessage({
      data: btoa(' TAIL')
    });
  });
  await page.waitForFunction(() => document.querySelector('.xterm-rows').innerText.includes('REPLACEMENT ✓ TAIL'));
});
test('automatic fitting debounces a burst, fits the latest pane once and ignores unchanged cells', async () => {
  const {
    page,
    state
  } = await fixture({
    idle: true
  });
  await state.wait(1);
  await page.waitForFunction(() => document.querySelector('#fit-session').dataset.fitNeeded === 'false');
  for (const width of [1850, 1900, 2000]) {
    await page.setViewportSize({
      width,
      height: 1000
    });
    await page.waitForTimeout(120);
  }
  assert.equal(state.calls.length, 1);
  await state.wait(2);
  await page.waitForTimeout(650);
  assert.equal(state.calls.length, 2);
  assert.equal(await page.locator('#draft').evaluate(element => document.activeElement === element), true, 'Automatic fit must not steal composer focus');
  await refreshWorkspace(page);
  await page.waitForTimeout(650);
  assert.equal(state.calls.length, 2);
  assert.equal(state.maxInFlight, 1);
});
test('hidden and cached terminals stay untouched and refit when shown in changed space', async () => {
  const {
    page,
    state
  } = await fixture({
    idle: true
  });
  await state.wait(1);
  await page.locator('#tab-review').click();
  await page.setViewportSize({
    width: 2100,
    height: 1000
  });
  await page.waitForTimeout(650);
  assert.equal(state.calls.length, 1);
  await page.locator('.task-row').filter({
    hasText: 'Record B'
  }).click();
  await page.setViewportSize({
    width: 2300,
    height: 1000
  });
  await page.waitForTimeout(650);
  assert.equal(state.calls.length, 1);
  await page.locator('.task-row').filter({
    hasText: 'Auto A'
  }).click();
  await page.locator('#tab-agent').click();
  await state.wait(2);
  await page.waitForTimeout(500);
  assert.equal(state.calls.length, 2);
});
test('fresh REST busy state suppresses a stale idle resize and later idle metadata resumes it', async () => {
  const {
    page,
    state
  } = await fixture();
  await page.waitForTimeout(600);
  assert.equal(state.calls.length, 0);
  state.tasks[0].idle = true;
  state.detailBusy = true;
  await refreshWorkspace(page);
  await page.waitForTimeout(650);
  assert.equal(state.calls.length, 0);
  assert.equal(await page.locator('#fit-session').getAttribute('data-fit-needed'), 'true');
  state.detailBusy = false;
  await refreshWorkspace(page);
  await state.wait(1);
  assert.equal(state.maxInFlight, 1);
});
test('an in-flight fit coalesces later geometry without concurrent resize requests', async () => {
  const {
    page,
    state
  } = await fixture();
  let release;
  state.resizeGate = new Promise(resolve => {
    release = resolve;
  });
  state.tasks[0].idle = true;
  await refreshWorkspace(page);
  await state.wait(1);
  await page.setViewportSize({
    width: 2000,
    height: 1000
  });
  await page.setViewportSize({
    width: 2400,
    height: 1000
  });
  await page.waitForTimeout(650);
  assert.equal(state.calls.length, 1);
  state.resizeGate = null;
  release();
  await state.wait(2);
  await page.waitForTimeout(650);
  assert.equal(state.calls.length, 2);
  assert.equal(state.maxInFlight, 1);
});
test('hiding during fresh task preflight cancels the pending mutation', async () => {
  const {
    page,
    state
  } = await fixture();
  let release, reached;
  state.detailGate = new Promise(resolve => {
    release = resolve;
  });
  const started = new Promise(resolve => {
    reached = resolve;
  });
  state.detailStarted = reached;
  state.tasks[0].idle = true;
  await refreshWorkspace(page);
  await started;
  await page.locator('#tab-review').click();
  release();
  await page.waitForTimeout(650);
  assert.equal(state.calls.length, 0);
});
test('background tabs defer fitting and refit on visibility restoration', async () => {
  const {
    page,
    state
  } = await fixture({
    idle: true
  });
  await state.wait(1);
  await page.evaluate(() => {
    Object.defineProperty(document, 'hidden', {
      configurable: true,
      get: () => true
    });
    document.dispatchEvent(new Event('visibilitychange'));
  });
  await page.setViewportSize({
    width: 2250,
    height: 1000
  });
  await page.waitForTimeout(650);
  assert.equal(state.calls.length, 1);
  await page.evaluate(() => {
    Object.defineProperty(document, 'hidden', {
      configurable: true,
      get: () => false
    });
    document.dispatchEvent(new Event('visibilitychange'));
  });
  await state.wait(2);
  assert.equal(state.calls.length, 2);
});
test('runtime data without a busy signal still uses settled automatic fitting', async () => {
  const {
    page,
    state
  } = await fixture();
  delete state.tasks[0].live;
  delete state.tasks[0].idle;
  await refreshWorkspace(page);
  await state.wait(1);
  await page.waitForTimeout(500);
  assert.equal(state.calls.length, 1);
});
test('nonvisual historical accessibility text cannot create a phantom horizontal scrollbar', async () => {
  const {
    page,
    state
  } = await fixture({
    width: 1280,
    idle: true
  });
  await state.wait(1);
  await page.waitForFunction(() => document.querySelector('#fit-session').dataset.fitNeeded === 'false');
  await page.waitForFunction(() => document.querySelector('.xterm-screen').getBoundingClientRect().width + 20 <= document.querySelector('.cached-terminal').clientWidth);
  // xterm debounces accessibility row refreshes for one second.
  await page.waitForTimeout(1100);
  const text = 'HISTORY_MARKER ' + 'old-width-history '.repeat(80);
  await page.locator('.xterm-accessibility-tree [role="listitem"]').first().evaluate((row, text) => {
    row.textContent = text;
  }, text);
  await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
  const overlay = page.locator('.xterm-accessibility');
  await overlay.evaluate(element => {
    element.style.contain = 'none';
  });
  assert.ok(await page.locator('.cached-terminal').evaluate(cache => cache.scrollWidth > cache.clientWidth), 'Negative control must reproduce historical-text overflow');
  await overlay.evaluate(element => element.style.removeProperty('contain'));
  assert.ok(await page.locator('.cached-terminal').evaluate(cache => cache.scrollWidth <= cache.clientWidth), 'Transparent history must not enlarge the visual scroll range');
  assert.equal(await page.locator('.xterm-accessibility-tree [role="listitem"]').first().textContent(), text);
  const cdp = await page.context().newCDPSession(page),
    tree = await cdp.send('Accessibility.getFullAXTree');
  assert.ok(tree.nodes.some(node => !node.ignored && node.role?.value === 'StaticText' && node.name?.value === text), 'Full historical text remains exposed in Chromium accessibility tree');
  await cdp.detach();
});
test('fresh authoritative size avoids resizing a PTY already fitted by another client', async () => {
  const {
    page,
    state
  } = await fixture();
  const response = page.waitForResponse(r => new URL(r.url()).pathname.endsWith('/resize'));
  await page.locator('#fit-session').click();
  await response;
  await page.waitForFunction(() => document.querySelector('.xterm-screen').getBoundingClientRect().width + 20 <= document.querySelector('.cached-terminal').clientWidth);
  const fittedWidth = await page.locator('.xterm-screen').evaluate(element => element.getBoundingClientRect().width);
  state.attachSizeOverride = {
    cols: 150,
    rows: 80
  };
  state.tasks[0].idle = true;
  await page.reload();
  await page.waitForFunction(() => document.querySelector('#terminal-state').textContent === 'Live terminal');
  await page.waitForTimeout(800);
  assert.equal(state.calls.length, 1, 'An already matching server PTY must not receive another resize');
  assert.equal(await page.locator('.xterm-screen').evaluate(element => element.getBoundingClientRect().width), fittedWidth, 'The stale client reconciles to authoritative dimensions');
});
test('split terminals retain one scroll owner and reserve the final glyph in both slots', async () => {
  const {
    page,
    state
  } = await fixture({
    idle: true
  });
  await state.wait(1);
  await page.locator('#split-toggle').click();
  await page.locator('#secondary-task-picker').selectOption('record-b');
  await page.locator('#secondary-terminal .xterm-screen').waitFor();
  await page.setViewportSize({
    width: 1728,
    height: 700
  });
  await page.waitForTimeout(1000);
  for (const selector of ['#terminal .cached-terminal', '#secondary-terminal .cached-terminal']) {
    await page.locator(selector).evaluate(cache => {
      cache.scrollLeft = cache.scrollWidth;
    });
    const result = await geometry(page, selector);
    assert.deepEqual(result.owners, ['xterm-viewport'], JSON.stringify(result));
    assert.equal(result.clippedGlyph, false, JSON.stringify(result));
    assert.ok(result.glyphEdge <= result.available - 3, JSON.stringify(result));
  }
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
