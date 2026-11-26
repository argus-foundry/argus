import { test, expect } from '@playwright/test';
import { resetServer } from './_helpers';

test.beforeEach(async () => { await resetServer(); });

async function login(page) {
  await page.addInitScript(() => localStorage.setItem('argus-token', 'test-token'));
  await page.goto('/');
  await expect(page.locator('#main-app')).toBeVisible();
}

test.describe('terminal', () => {
  test('opens task and renders xterm.js', async ({ page }) => {
    await login(page);
    await page.locator('.task-item').first().click();
    await expect(page.locator('#term-wrap')).toBeVisible();
    await expect(page.locator('#term .xterm')).toBeVisible();
    await expect(page.locator('#term .xterm-rows')).toBeVisible();
    // FitAddon should produce a non-zero grid.
    const cols = await page.evaluate(() => (window as any).term?.cols ?? 0);
    const rows = await page.evaluate(() => (window as any).term?.rows ?? 0);
    expect(cols).toBeGreaterThan(0);
    expect(rows).toBeGreaterThan(0);
  });

  test('SSE stream connects and shows live status', async ({ page }) => {
    await login(page);
    await page.locator('.task-item').first().click();
    await expect(page.locator('.term-status.live')).toBeVisible({ timeout: 5000 });
    await expect(page.locator('#term-status-text')).toHaveText('live');
  });

  test('terminal waits between stale replacement snapshots', async ({ page }) => {
    const requestedAt: number[] = [];
    await page.route(/\/api\/tasks\/[^/]+\/output(?:\?|$)/, async route => {
      requestedAt.push(Date.now());
      if (requestedAt.length <= 2) {
        await route.fulfill({ status: 409, json: { code: 'stale_session' } });
      } else {
        await route.continue();
      }
    });
    await login(page);
    await page.locator('.task-item').first().click();
    await expect(page.locator('.term-status.live')).toBeVisible({ timeout: 5000 });
    expect(requestedAt.length).toBeGreaterThanOrEqual(3);
    expect(requestedAt[2] - requestedAt[1]).toBeGreaterThanOrEqual(200);
  });

  test('a superseded terminal recovery leaves the reopened terminal attached', async ({ page }) => {
    let requests = 0;
    let release: () => void;
    const held = new Promise<void>(resolve => { release = resolve; });
    await page.route(/\/api\/tasks\/[^/]+\/output(?:\?|$)/, async route => {
      switch (++requests) {
      case 1:
        await route.fulfill({ status: 409, json: { code: 'stale_session' } });
        return;
      case 2:
        await held;
        await route.fulfill({ status: 409, json: { code: 'stale_session' } }).catch(() => {});
        return;
      default:
        await route.continue();
      }
    });
    try {
      await login(page);
      await page.locator('.task-item').first().click();
      await expect.poll(() => requests).toBe(2);
      await page.locator('#detail-view .detail-back').click();
      await page.locator('.task-item').first().click();
      await expect(page.locator('.term-status.live')).toBeVisible({ timeout: 5000 });
      release!();
      await page.waitForTimeout(350);
      expect(requests).toBe(3);
      await expect(page.locator('.term-status.live')).toBeVisible();
    } finally {
      release!();
    }
  });

  test('leaving the terminal tab cancels a pending recovery snapshot', async ({ page }) => {
    let requests = 0;
    let streams = 0;
    page.on('request', request => {
      if (/\/api\/tasks\/[^/]+\/stream(?:\?|$)/.test(request.url())) streams++;
    });
    let release: () => void;
    const held = new Promise<void>(resolve => { release = resolve; });
    await page.route(/\/api\/tasks\/[^/]+\/output(?:\?|$)/, async route => {
      if (++requests === 1) {
        await route.fulfill({ status: 409, json: { code: 'stale_session' } });
        return;
      }
      await held;
      await route.continue().catch(() => {});
    });
    try {
      await login(page);
      await page.locator('.task-item').first().click();
      await expect.poll(() => requests).toBe(2);
      await page.evaluate(() => (window as any).switchTab('settings'));
      release!();
      await page.waitForTimeout(350);
      await expect(page.locator('#settings-view')).toBeVisible();
      expect(await page.evaluate(() => (window as any).term == null)).toBe(true);
      expect(requests).toBe(2);
      expect(streams).toBe(0);
    } finally {
      release!();
    }
  });

  test('an unauthorized recovery snapshot logs out and detaches the terminal', async ({ page }) => {
    let snapshots = 0;
    let streams = 0;
    page.on('request', request => {
      if (/\/api\/tasks\/[^/]+\/stream(?:\?|$)/.test(request.url())) streams++;
    });
    await page.route(/\/api\/tasks\/[^/]+\/output(?:\?|$)/, async route => {
      if (++snapshots === 1) {
        await route.fulfill({ status: 409, json: { code: 'stale_session' } });
      } else {
        await route.fulfill({ status: 401, json: { error: 'unauthorized' } });
      }
    });

    await login(page);
    await page.locator('.task-item').first().click();
    await expect(page.locator('#auth-screen')).toBeVisible();
    await expect(page.locator('#detail-view')).not.toHaveClass(/\bopen\b/);
    expect(await page.evaluate(() => [(window as any).term, (window as any).currentTask])).toEqual([null, null]);
    expect(snapshots).toBe(2);
    expect(streams).toBe(0);
  });

  test('signing in again keeps Settings as the only visible tab', async ({ page }) => {
    await login(page);
    await page.locator('.tab[data-tab="settings"]').click();
    await expect(page.locator('#settings-view')).toBeVisible();
    await page.getByRole('button', { name: 'Forget token' }).click();
    await expect(page.locator('#auth-screen')).toBeVisible();
    await page.locator('#token-input').fill('test-token');
    await page.locator('#auth-screen button').click();
    await expect(page.locator('#main-app')).toBeVisible();
    await expect(page.locator('#settings-view')).toBeVisible();
    await expect(page.locator('#tasks-view')).toBeHidden();
  });

  test('resume completion cannot replace a reopened terminal view', async ({ page }) => {
    let requested = false;
    let release: () => void;
    const held = new Promise<void>(resolve => { release = resolve; });
    await page.route(/\/api\/tasks\/[^/]+\/resume$/, async route => {
      requested = true;
      await held;
      await route.fulfill({ status: 200, json: {} }).catch(() => {});
    });
    try {
      await login(page);
      await page.locator('.task-item').first().click();
      await expect(page.locator('.term-status.live')).toBeVisible();
      await page.evaluate(() => { (window as any).pendingResume = (window as any).resumeTask(); });
      await expect.poll(() => requested).toBe(true);
      await page.locator('#detail-view .detail-back').click();
      await page.locator('.task-item').first().click();
      await expect(page.locator('.term-status.live')).toBeVisible();
      await expect(page.locator('#btn-overflow')).toBeEnabled();
      await page.evaluate(() => { (window as any).reopenedTerm = (window as any).term; });
      release!();
      const sameTerminal = await page.evaluate(async () => {
        await (window as any).pendingResume;
        return (window as any).term === (window as any).reopenedTerm;
      });
      expect(sameTerminal).toBe(true);
    } finally {
      release!();
    }
  });

  for (const action of ['fork', 'rename', 'archive', 'delete']) test(`late ${action} leaves the reopened terminal view alone`, async ({ page }) => {
    let requested = false;
    let serverName: string | undefined;
    let deletedID: string | undefined;
    const movesOutOfList = action === 'delete' || action === 'archive';
    let release: () => void;
    const held = new Promise<void>(resolve => { release = resolve; });
    await page.route(/\/api\/tasks(?:\?|$)/, async route => {
      if (!movesOutOfList && !serverName) return route.continue();
      const response = await route.fetch();
      const body = await response.json();
      if (serverName) body.tasks[0].name = serverName;
      if (deletedID) body.tasks = body.tasks.filter(task => task.id !== deletedID);
      if (movesOutOfList) {
        body.tasks.push({ id: 'unrelated-task', name: 'Other task', project: 'test-proj', status: 'pending', live: false });
      }
      await route.fulfill({ response, json: body });
    });
    await page.route(/\/api\/tasks\/[^/?]+(?:\/(?:fork|rename|archive))?$/, async route => {
      const request = route.request();
      if (request.method() === 'GET' || !['POST', 'DELETE'].includes(request.method())) return route.continue();
      requested = true;
      await held;
      if (action === 'rename') serverName = 'Renamed by server';
      if (action === 'delete') deletedID = new URL(request.url()).pathname.split('/').at(-1);
      if (action === 'archive') deletedID = new URL(request.url()).pathname.split('/').at(-2);
      await route.fulfill({ status: 200, json: { id: 'forked-task' } }).catch(() => {});
    });
    page.on('dialog', dialog => dialog.type() === 'prompt' ? dialog.accept('Action result') : dialog.accept());
    try {
      await login(page);
      await page.locator('.task-item').filter({ hasText: 'echo-bash' }).first().click();
      await expect(page.locator('.term-status.live')).toBeVisible();
      await expect(page.locator('#btn-overflow')).toBeEnabled();
      await page.evaluate(action => {
        const app = window as any;
        const mutate = { fork: app.forkTask, rename: app.renameTask, archive: () => app.setArchive(true), delete: app.deleteTask };
        app.pendingAction = mutate[action]();
      }, action);
      await expect.poll(() => requested).toBe(true);
      await page.locator('#detail-view .detail-back').click();
      if (movesOutOfList) await page.locator('.task-item').filter({ hasText: 'Other task' }).click();
      else await page.locator('.task-item').first().click();
      await expect(page.locator('#term .xterm')).toBeVisible();
      await expect(page.locator('#btn-overflow')).toBeEnabled();
      const title = await page.locator('#detail-title').textContent();
      await page.evaluate(() => { (window as any).reopenedTerm = (window as any).term; });
      release!();
      const sameTerminal = await page.evaluate(async () => {
        await (window as any).pendingAction;
        return (window as any).term === (window as any).reopenedTerm;
      });
      expect(sameTerminal).toBe(true);
      await expect(page.locator('#detail-title')).toHaveText(serverName ?? title!);
    } finally {
      release!();
    }
  });

  for (const archive of [true, false]) test(`successful ${archive ? 'Archive' : 'Unarchive'} closes a reopened task outside the selected list`, async ({ page }) => {
    let archived = !archive;
    let requested = false;
    let release: () => void;
    const held = new Promise<void>(resolve => { release = resolve; });
    await page.route(/\/api\/tasks(?:\?|$)/, async route => {
      const url = new URL(route.request().url());
      const selectedArchive = url.searchParams.get('archived') === '1';
      url.search = '';
      const response = await route.fetch({ url: String(url) });
      const body = await response.json();
      body.tasks = selectedArchive === archived ? body.tasks.map(task => ({ ...task, archived })) : [];
      await route.fulfill({ response, json: body });
    });
    await page.route(/\/api\/tasks\/[^/]+\/(?:archive|unarchive)$/, async route => {
      requested = true;
      await held;
      archived = archive;
      await route.fulfill({ status: 200, json: { archived } }).catch(() => {});
    });
    try {
      await login(page);
      if (!archive) await page.evaluate(() => (window as any).setListFilter('archived'));
      await page.locator('.task-item').filter({ hasText: 'echo-bash' }).click();
      await page.evaluate(archive => { (window as any).pendingArchive = (window as any).setArchive(archive); }, archive);
      await expect.poll(() => requested).toBe(true);
      await page.locator('#detail-view .detail-back').click();
      await page.locator('.task-item').filter({ hasText: 'echo-bash' }).click();
      release!();
      await page.evaluate(() => (window as any).pendingArchive);
      await expect(page.locator('#detail-view')).not.toHaveClass(/\bopen\b/);
      await expect(page.locator('.task-item')).toHaveCount(0);
    } finally {
      release!();
    }
  });

  test('successful Delete closes a reopened view of the deleted task', async ({ page }) => {
    let deletedID: string | undefined;
    let requested = false;
    let release: () => void;
    const held = new Promise<void>(resolve => { release = resolve; });
    await page.route(/\/api\/tasks(?:\?|$)/, async route => {
      if (!deletedID) return route.continue();
      const response = await route.fetch();
      const body = await response.json();
      body.tasks = body.tasks.filter(task => task.id !== deletedID);
      await route.fulfill({ response, json: body });
    });
    await page.route(/\/api\/tasks\/[^/?]+$/, async route => {
      if (route.request().method() !== 'DELETE') return route.continue();
      requested = true;
      await held;
      deletedID = new URL(route.request().url()).pathname.split('/').at(-1);
      await route.fulfill({ status: 200, json: { status: 'deleted' } }).catch(() => {});
    });
    page.on('dialog', dialog => dialog.accept());
    try {
      await login(page);
      await page.locator('.task-item').first().click();
      await expect(page.locator('.term-status.live')).toBeVisible();
      await page.evaluate(() => { (window as any).pendingDelete = (window as any).deleteTask(); });
      await expect.poll(() => requested).toBe(true);
      await page.locator('#detail-view .detail-back').click();
      await page.locator('.task-item').first().click();
      await expect(page.locator('#btn-overflow')).toBeEnabled();
      release!();
      await page.evaluate(() => (window as any).pendingDelete);
      await expect(page.locator('#detail-view')).not.toHaveClass(/\bopen\b/);
      await expect(page.locator('#term .xterm')).toHaveCount(0);
    } finally {
      release!();
    }
  });

  for (const stage of ['body', 'render']) test(`a stalled replacement snapshot ${stage} reaches the terminal recovery deadline`, async ({ page }) => {
    await page.addInitScript(stage => {
      const original = window.fetch.bind(window);
      let snapshots = 0;
      window.fetch = async (...args) => {
        const response = await original(...args);
        if (/\/api\/tasks\/[^/]+\/output(?:\?|$)/.test(response.url) && ++snapshots === 2) {
          if (stage === 'body') {
            const bytes = await response.arrayBuffer();
            response.arrayBuffer = () => new Promise(resolve => {
              (window as any).releaseSnapshot = () => resolve(bytes);
            });
          } else {
            const terminal = (window as any).term;
            const write = terminal.write.bind(terminal);
            terminal.write = (data: unknown, callback?: () => void) => {
              if (data instanceof Uint8Array && callback) {
                (window as any).releaseSnapshot = callback;
              } else {
                write(data, callback);
              }
            };
          }
        }
        return response;
      };
    }, stage);
    let streams = 0;
    page.on('request', request => {
      if (/\/api\/tasks\/[^/]+\/stream(?:\?|$)/.test(request.url())) streams++;
    });
    let initial = true;
    await page.route(/\/api\/tasks\/[^/]+\/output(?:\?|$)/, async route => {
      if (initial) {
        initial = false;
        await route.fulfill({ status: 409, json: { code: 'stale_session' } });
      } else {
        await route.continue();
      }
    });
    await login(page);
    await page.locator('.task-item').first().click();
    await expect.poll(() => page.evaluate(() => typeof (window as any).releaseSnapshot)).toBe('function');
    await expect(page.locator('#term-status-text')).toHaveText('session ended', { timeout: 17_000 });
    await page.evaluate(() => (window as any).releaseSnapshot());
    await expect(page.locator('#term-status-text')).toHaveText('session ended');
    expect(streams).toBe(0);
  });

  test('list refresh keeps an active recovery deadline through stream admission', async ({ page }) => {
    await page.addInitScript(() => {
      window.EventSource = new Proxy(window.EventSource, {
        construct(target, args) {
          const source = Reflect.construct(target, args);
          if (String(args[0]).includes('/stream?')) (window as any).recoverySource = source;
          return source;
        },
      });
    });
    let detailPolled = false;
    let refreshLive = true;
    let snapshots = 0;
    let streamRequests = 0;
    let detailReleased: () => void;
    let streamReleased: () => void;
    const detailHeld = new Promise<void>(resolve => { detailReleased = resolve; });
    const streamHeld = new Promise<void>(resolve => { streamReleased = resolve; });
    await page.route(/\/api\/tasks(?:\?|$)/, async route => {
      const response = await route.fetch();
      const body = await response.json();
      for (const task of body.tasks) task.live = refreshLive;
      await route.fulfill({ response, json: body });
    });
    await page.route(/\/api\/tasks\/[^/?]+$/, async route => {
      detailPolled = true;
      await detailHeld;
      await route.continue().catch(() => {});
    });
    await page.route(/\/api\/tasks\/[^/]+\/output(?:\?|$)/, async route => {
      if (++snapshots === 1) {
        await route.fulfill({ status: 409, json: { code: 'stale_session' } });
      } else {
        await route.continue();
      }
    });
    await page.route(/\/api\/tasks\/[^/]+\/stream(?:\?|$)/, async route => {
      streamRequests++;
      await streamHeld;
      await route.fulfill({ status: 200, contentType: 'text/event-stream', body: 'data: late\n\n' }).catch(() => {});
    });

    try {
      await login(page);
      await page.locator('.task-item').first().click();
      await expect.poll(() => detailPolled).toBe(true);
      for (const live of [false, true]) {
        refreshLive = live;
        await page.evaluate(() => (window as any).refresh());
        expect(await page.evaluate(() => (window as any).currentTask.live)).toBe(live);
      }
      expect(snapshots).toBe(1);
      detailReleased!();
      await expect.poll(() => streamRequests).toBe(1);
      await expect(page.locator('#term-status-text')).toHaveText('session ended', { timeout: 17_000 });
      expect(await page.evaluate(() => (window as any).recoverySource?.readyState)).toBe(2);
      streamReleased!();
      await expect(page.locator('#term-status-text')).toHaveText('session ended');
    } finally {
      detailReleased!();
      streamReleased!();
    }
  });

  test('terminal waits between refused replacement streams', async ({ page }) => {
    const requestedAt: number[] = [];
    await page.route(/\/api\/tasks\/[^/]+\/stream(?:\?|$)/, async route => {
      requestedAt.push(Date.now());
      if (requestedAt.length <= 3) {
        await route.fulfill({ status: 409, json: { code: 'stale_session' } });
      } else {
        await route.continue();
      }
    });
    await login(page);
    await page.locator('.task-item').first().click();
    await expect(page.locator('.term-status.live')).toBeVisible({ timeout: 5000 });
    expect(requestedAt.length).toBeGreaterThanOrEqual(4);
    expect(requestedAt[2] - requestedAt[1]).toBeGreaterThanOrEqual(200);
    expect(requestedAt[3] - requestedAt[2]).toBeGreaterThanOrEqual(200);
  });

  test('typing forwards to PTY and echoes back', async ({ page }) => {
    await login(page);
    await page.locator('.task-item').first().click();
    await expect(page.locator('.term-status.live')).toBeVisible({ timeout: 5000 });

    // Click into terminal viewport to focus, then type a unique marker.
    await page.locator('#term').click();
    const marker = 'PWMARK' + Date.now();
    await page.keyboard.type(`echo ${marker}`);
    await page.keyboard.press('Enter');

    // Wait for the echo response in the terminal grid.
    await expect.poll(async () => {
      return await page.evaluate(() => {
        const term = (window as any).term;
        if (!term) return '';
        const buf = term.buffer.active;
        let out = '';
        for (let y = 0; y < buf.length; y++) {
          const line = buf.getLine(y);
          if (line) out += line.translateToString(true) + '\n';
        }
        return out;
      });
    }, { timeout: 5000 }).toContain(marker);
  });

  test('resize endpoint is called when viewport changes', async ({ page, context }) => {
    await login(page);
    await page.locator('.task-item').first().click();
    await expect(page.locator('.term-status.live')).toBeVisible({ timeout: 5000 });

    // Listen for the resize POST.
    const resizePromise = page.waitForRequest(req =>
      req.url().includes('/resize') && req.method() === 'POST',
      { timeout: 5000 }
    );

    // Simulate orientation change (landscape).
    await page.setViewportSize({ width: 800, height: 390 });

    const req = await resizePromise;
    const body = JSON.parse(req.postData() || '{}');
    expect(body.cols).toBeGreaterThan(0);
    expect(body.rows).toBeGreaterThan(0);
  });

  test('detail view layout fits within phone viewport', async ({ page, viewport }) => {
    // Phone-only: at desktop widths the fixed #detail-view is trivially within
    // the viewport (left:0; right:0), so the assertions would pass vacuously.
    test.skip((viewport?.width ?? 0) > 500, 'phone-viewport regression only');
    await login(page);
    await page.locator('.task-item').first().click();
    await expect(page.locator('#term-wrap')).toBeVisible();
    await expect(page.locator('.term-status.live')).toBeVisible({ timeout: 5000 });

    // The detail view is position:fixed; the term flex chain must shrink to
    // viewport on phones. Verify the key boxes don't claim more layout width
    // than the viewport, and the document itself isn't horizontally scrollable
    // (catches stray width:100vw / overflow rules on the body).
    const layout = await page.evaluate(() => {
      const vw = window.innerWidth;
      // -1 sentinel on a missing node produces a readable assertion failure
      // ("-1 <= vw+1" passes; we'd never see a -1 in practice for real boxes)
      // instead of a TypeError from a non-null assertion.
      const right = (id: string) =>
        document.getElementById(id)?.getBoundingClientRect().right ?? -1;
      return {
        vw,
        detailView: right('detail-view'),
        termWrap: right('term-wrap'),
        term: right('term'),
        docScrollWidth: document.documentElement.scrollWidth,
        docClientWidth: document.documentElement.clientWidth,
      };
    });
    // +1 px tolerance for subpixel rounding at devicePixelRatio > 1.
    expect(layout.detailView).toBeLessThanOrEqual(layout.vw + 1);
    expect(layout.termWrap).toBeLessThanOrEqual(layout.vw + 1);
    expect(layout.term).toBeLessThanOrEqual(layout.vw + 1);
    expect(layout.docScrollWidth).toBeLessThanOrEqual(layout.docClientWidth + 1);
  });

  test('opening detail resets stale viewport geometry', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== 'iphone', 'phone viewport regression only');
    await login(page);
    const listScrollY = await page.evaluate(() => {
      document.documentElement.style.setProperty('--app-height', '123px');
      document.getElementById('detail-view')!.style.transform = 'translateY(280px)';
      document.body.style.minHeight = '200vh';
      window.scrollTo(0, 300);
      const scrollY = window.scrollY;
      // Dispatch the same click handler without Playwright auto-scrolling the
      // row back into view; the stale list scroll must still be present.
      document.querySelector('.task-item')!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      return scrollY;
    });
    expect(listScrollY).toBeGreaterThan(0);
    await expect.poll(() => page.evaluate(() => ({
      height: document.documentElement.style.getPropertyValue('--app-height'),
      top: document.getElementById('detail-view')!.getBoundingClientRect().top,
    }))).toEqual({ height: await page.evaluate(() => `${window.visualViewport!.height}px`), top: 0 });
  });

  test('foreground return resyncs detail without a viewport event', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== 'iphone', 'phone viewport regression only');
    await login(page);
    await page.locator('.task-item').first().click();
    await page.evaluate(() => {
      const vv = window.visualViewport!;
      const state = { height: window.innerHeight - 300, top: 250 };
      (window as any).fakeViewport = state;
      Object.defineProperty(vv, 'height', { configurable: true, get: () => state.height });
      Object.defineProperty(vv, 'offsetTop', { configurable: true, get: () => state.top });
      vv.dispatchEvent(new Event('resize'));
    });
    await expect(page.locator('#detail-view')).toHaveCSS('transform', 'matrix(1, 0, 0, 1, 0, 250)');
    await page.evaluate(() => {
      Object.defineProperty(document, 'hidden', { configurable: true, get: () => true });
      document.dispatchEvent(new Event('visibilitychange'));
    });
    expect(await page.evaluate(() => document.getElementById('detail-view')!.style.transform)).toBe('translateY(250px)');
    await page.evaluate(() => {
      Object.defineProperty(document, 'hidden', { configurable: true, get: () => false });
      const state = (window as any).fakeViewport;
      state.height = window.innerHeight;
      state.top = 0;
      window.dispatchEvent(new Event('pageshow'));
    });
    await expect.poll(() => page.evaluate(() => ({
      height: document.documentElement.style.getPropertyValue('--app-height'),
      transform: document.getElementById('detail-view')!.style.transform,
    }))).toEqual({ height: await page.evaluate(() => `${window.innerHeight}px`), transform: '' });
  });

  test('buffers SSE writes while scrolled into history', async ({ page }) => {
    await login(page);
    await page.locator('.task-item').first().click();
    await expect(page.locator('.term-status.live')).toBeVisible({ timeout: 5000 });

    // Populate scrollback directly so the test is independent of viewport
    // size and shell timing.
    await page.evaluate(() => {
      const term = (window as any).term;
      const enc = new TextEncoder();
      let blob = '';
      for (let i = 0; i < 200; i++) blob += `fillerline${i}\r\n`;
      term.write(enc.encode(blob));
    });
    // Wait for xterm's writer queue to drain so baseY reflects the writes.
    await expect.poll(async () =>
      page.evaluate(() => (window as any).term.buffer.active.baseY)
    , { timeout: 5000 }).toBeGreaterThan(20);

    // Scroll up into history.
    await page.evaluate(() => (window as any).term.scrollLines(-30));
    await expect(page.locator('#jump-bottom.shown')).toBeVisible();
    const scrolledY = await page.evaluate(() => (window as any).term.buffer.active.viewportY);

    // Simulate live output arriving via the SSE path. bufferOrWrite is the
    // single entry point the SSE handler uses, so this exercises the same
    // branch.
    await page.evaluate(() => {
      const enc = new TextEncoder();
      (window as any).bufferOrWrite(enc.encode('LIVE_MARKER_AAA\r\n'));
      (window as any).bufferOrWrite(enc.encode('LIVE_MARKER_BBB\r\n'));
    });

    // Pending should be queued, viewport must not have moved.
    const pending = await page.evaluate(() => (window as any).argusPending());
    expect(pending.chunks).toBe(2);
    expect(pending.bytes).toBeGreaterThan(0);
    await expect(page.locator('#jump-bottom.has-pending')).toBeVisible();

    const stillY = await page.evaluate(() => (window as any).term.buffer.active.viewportY);
    expect(stillY).toEqual(scrolledY);

    // Tap jump-bottom to flush. Pending should drain and indicators clear.
    await page.locator('#jump-bottom').click();
    await expect(page.locator('#jump-bottom.has-pending')).toHaveCount(0);
    await expect(page.locator('#jump-bottom.shown')).toHaveCount(0);
    const drained = await page.evaluate(() => (window as any).argusPending());
    expect(drained.chunks).toBe(0);
    expect(drained.bytes).toBe(0);

    // Buffered markers should now be in the terminal buffer. xterm.write
    // queues bytes asynchronously, so poll instead of asserting on a
    // single snapshot — the markers land within a few rAFs of flushPending.
    await expect.poll(async () =>
      page.evaluate(() => {
        const buf = (window as any).term.buffer.active;
        let s = '';
        for (let y = 0; y < buf.length; y++) {
          const line = buf.getLine(y);
          if (line) s += line.translateToString(true) + '\n';
        }
        return s;
      }),
    { timeout: 3000 }).toMatch(/LIVE_MARKER_AAA[\s\S]*LIVE_MARKER_BBB/);
  });

  test('buffers SSE writes while finger is on the term (iOS momentum guard)', async ({ page }) => {
    await login(page);
    await page.locator('.task-item').first().click();
    await expect(page.locator('.term-status.live')).toBeVisible({ timeout: 5000 });

    // Synthesize a touchstart on #term. The production handler reads no
    // TouchEvent fields (just toggles `termTouching`), so a plain Event
    // exercises the same code path without relying on Playwright's touch
    // emulation, which differs between desktop/iphone profiles.
    await page.evaluate(() => {
      document.getElementById('term')!.dispatchEvent(new Event('touchstart'));
    });
    const touchState = await page.evaluate(() => (window as any).argusTouchState());
    expect(touchState.touching).toBe(true);

    // Live SSE chunk arriving mid-touch must be buffered, not written —
    // termIsAtBottom() is still true here (no scroll yet), so the touch
    // gate is what catches this.
    await page.evaluate(() => {
      const enc = new TextEncoder();
      (window as any).bufferOrWrite(enc.encode('TOUCH_BUFFERED_AAA\r\n'));
    });
    const duringTouch = await page.evaluate(() => (window as any).argusPending());
    expect(duringTouch.chunks).toBe(1);

    // touchend without a scroll-up leaves us at the bottom — drainIfSettled
    // runs synchronously on touchend (since !isTermScrolling), flushing the
    // pending bytes immediately.
    await page.evaluate(() => {
      document.getElementById('term')!.dispatchEvent(new Event('touchend'));
    });
    await expect.poll(async () =>
      page.evaluate(() => (window as any).argusPending().chunks)
    , { timeout: 2000 }).toBe(0);

    const dump = await page.evaluate(() => {
      const buf = (window as any).term.buffer.active;
      let s = '';
      for (let y = 0; y < buf.length; y++) {
        const line = buf.getLine(y);
        if (line) s += line.translateToString(true) + '\n';
      }
      return s;
    });
    expect(dump).toContain('TOUCH_BUFFERED_AAA');
  });

  test('visualViewport.resize during touch defers ancestor-mutating sync', async ({ page }) => {
    await login(page);
    await page.locator('.task-item').first().click();
    await expect(page.locator('.term-status.live')).toBeVisible({ timeout: 5000 });

    // Before any touch, syncVisualViewport must run inline. Confirm the
    // baseline so a later "no mutations" assertion isn't a vacuous pass
    // because the listener never fired at all.
    const baseline = await page.evaluate(() => {
      const before = (window as any).argusTouchState();
      window.visualViewport?.dispatchEvent(new Event('resize'));
      const after = (window as any).argusTouchState();
      return { before: before.pendingViewportSync, after: after.pendingViewportSync };
    });
    expect(baseline.before).toBe(false);
    expect(baseline.after).toBe(false); // ran inline, didn't queue

    // Pre-set --app-height to a sentinel so the deferred sync's setProperty
    // call has a different value to write — without this, the browser
    // dedupes same-value writes and no mutation appears.
    await page.evaluate(() => {
      document.documentElement.style.setProperty('--app-height', '1px');
    });

    // Simulate a touch in progress and fire visualViewport.resize while a
    // MutationObserver watches the .xterm-viewport ancestor chain. The
    // queued resize must NOT produce ancestor mutations (which is what
    // kills iOS momentum), and the queued sync must actually run on
    // touchend (proven by a mutation overwriting --app-height back to the
    // real vv.height).
    const result = await page.evaluate(async () => {
      const viewport = document.querySelector('.xterm-viewport')!;
      const ancestors = new Set<Element>();
      let p: Element | null = viewport.parentElement;
      while (p) { ancestors.add(p); p = p.parentElement; }
      let duringTouchMutations = 0;
      let afterSettleMutations = 0;
      let phase: 'touch' | 'settle' = 'touch';
      const obs = new MutationObserver(rs => {
        for (const r of rs) {
          if (ancestors.has(r.target as Element) || r.target === viewport) {
            if (phase === 'touch') duringTouchMutations++;
            else afterSettleMutations++;
          }
        }
      });
      obs.observe(document.documentElement, { childList: true, subtree: true, attributes: true });

      document.getElementById('term')!.dispatchEvent(new Event('touchstart'));
      window.visualViewport?.dispatchEvent(new Event('resize'));
      const queued = (window as any).argusTouchState();
      await new Promise(res => setTimeout(res, 30));

      phase = 'settle';
      document.getElementById('term')!.dispatchEvent(new Event('touchend'));
      // touchend with !isTermScrolling drains synchronously; allow a microtask
      // tick for the MutationObserver to surface the resulting setProperty.
      await new Promise(res => setTimeout(res, 50));
      obs.disconnect();
      const final = (window as any).argusTouchState();
      const finalAppHeight = document.documentElement.style.getPropertyValue('--app-height');
      return { queued, final, duringTouchMutations, afterSettleMutations, finalAppHeight };
    });
    expect(result.queued.touching).toBe(true);
    expect(result.queued.pendingViewportSync).toBe(true);
    // The resize fired mid-touch must NOT have mutated ancestors —
    // otherwise iOS would have killed momentum.
    expect(result.duringTouchMutations).toBe(0);
    // After settle, the deferred sync must actually run: setProperty
    // overwrites our '1px' sentinel with the real vv.height, recording
    // a mutation on <html>[style].
    expect(result.afterSettleMutations).toBeGreaterThan(0);
    expect(result.finalAppHeight).not.toBe('1px');
    expect(result.final.pendingViewportSync).toBe(false);
  });

  test('xterm-viewport is the topmost element so touches reach the scroll target', async ({ page }) => {
    await login(page);
    await page.locator('.task-item').first().click();
    await expect(page.locator('.term-status.live')).toBeVisible({ timeout: 5000 });

    // Without the z-index fix, .xterm-screen paints on top of .xterm-viewport
    // and elementFromPoint at the visual centre of the term returns the
    // canvas / screen layer — touches go there and never hit the scrollable
    // viewport. Confirm the lift worked: the topmost element at the term's
    // centre must be inside .xterm-viewport's subtree.
    const result = await page.evaluate(() => {
      const term = document.getElementById('term')!;
      const rect = term.getBoundingClientRect();
      const cx = rect.left + rect.width / 2;
      const cy = rect.top + rect.height / 2;
      const top = document.elementFromPoint(cx, cy);
      const viewport = document.querySelector('.xterm-viewport')!;
      return {
        topTag: top?.tagName,
        topClass: top?.className,
        topInViewport: top !== null && (top === viewport || viewport.contains(top)),
        viewportZIndex: getComputedStyle(viewport).zIndex,
      };
    });
    expect(result.viewportZIndex).toBe('1');
    expect(result.topInViewport).toBe(true);
  });

  // Note: a previous version of this file had an "isTermScrolling gate
  // blocks writes until scrollend fires" test. It synthetically dispatched
  // scroll/scrollend events on .xterm-viewport, but xterm.js fires its own
  // scroll events from internal rAF callbacks (auto-snap-to-bottom, render
  // sync) at unpredictable times that overlap with the test's `await`
  // microtask boundaries — making the test deeply flaky regardless of what
  // we asserted. The gate behavior is covered transitively: the
  // "buffers SSE writes while finger is on the term" test exercises
  // termTouching, and the "visualViewport.resize during touch" test
  // exercises pendingViewportSync deferral. Don't reintroduce a dispatch-
  // synthetic-scroll-events test on .xterm-viewport without first solving
  // the xterm-rAF interference (e.g., via a pause/resume API).

  test('back button cleans up stream and term', async ({ page }) => {
    await login(page);
    await page.locator('.task-item').first().click();
    await expect(page.locator('#term-wrap')).toBeVisible();
    // The prompt modal's overlay has its own .detail-back link, so the
    // unscoped locator now hits two elements — scope to the open detail view.
    await page.locator('#detail-view .detail-back').click();
    await expect(page.locator('#tasks-view')).toBeVisible();
    // term should be disposed.
    const termInstance = await page.evaluate(() => (window as any).term);
    expect(termInstance).toBeNull();
  });

  // Ctrl+Z guard: a bare Ctrl+Z emits 0x1a (SIGTSTP); reaching Claude Code's
  // stdin it backgrounds the session into the CLI's own supervisor, orphaning
  // the argus worker permanently. The TUI already traps Ctrl+Z on every
  // surface; the PWA terminal swallows it at the xterm key layer so 0x1a never
  // reaches /input. See gotchas/web-remote.md.
  test('Ctrl+Z is swallowed and never reaches the PTY input path', async ({ page }) => {
    await login(page);
    await page.locator('.task-item').first().click();
    await expect(page.locator('.term-status.live')).toBeVisible({ timeout: 5000 });

    // Capture every byte the SPA POSTs to the input endpoint.
    const inputBodies: string[] = [];
    page.on('request', req => {
      if (req.method() === 'POST' && /\/api\/tasks\/[^/]+\/input$/.test(req.url())) {
        inputBodies.push(req.postData() ?? '');
      }
    });

    // Focus xterm's helper textarea directly (works on desktop AND the iphone
    // profile's compose-redirect, since we bypass tap-to-focus routing).
    await page.evaluate(() => (window as any).term?.focus());
    await page.keyboard.press('Control+z');

    // The explanatory toast appears (never a silent dead key).
    await expect(page.locator('.toast')).toContainText('background the agent');

    // A control keystroke still forwards — proves the guard is specific to
    // Ctrl+Z, not a blanket input block.
    await page.keyboard.press('x');
    await expect.poll(async () => inputBodies.some(b => b.includes('x')), { timeout: 3000 }).toBe(true);

    // No 0x1a (SUB / SIGTSTP) byte was ever POSTed to the PTY.
    const SUB = String.fromCharCode(0x1a); // 0x1a = SUB / the Ctrl+Z byte
    expect(inputBodies.some(b => b.includes(SUB))).toBe(false);
  });

  test('Cmd+Z (metaKey) is not intercepted by the Ctrl+Z guard', async ({ page }) => {
    await login(page);
    await page.locator('.task-item').first().click();
    await expect(page.locator('.term-status.live')).toBeVisible({ timeout: 5000 });

    await page.evaluate(() => (window as any).term?.focus());
    await page.keyboard.press('Meta+z');

    // Cmd+Z is the browser/textarea undo — the guard must leave it alone, so
    // no "background the agent" toast is shown.
    await page.waitForTimeout(200);
    await expect(page.locator('.toast', { hasText: 'background the agent' })).toHaveCount(0);
  });
});
