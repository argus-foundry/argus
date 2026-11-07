import { test } from '@playwright/test';
import { resetServer } from '../../_helpers';
import assert from 'node:assert/strict';
let browser;
const cleanup = [];
async function fixture(width = 1280) {
  const page = await browser.newPage({
    viewport: {
      width,
      height: 900
    }
  });
  cleanup.push(() => page.close());
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  cleanup.push(() => assert.deepEqual(errors, []));
  const task = {
    id: 'controls',
    name: 'Control alignment',
    backend: 'claude',
    project: 'p',
    branch: 'lzl/controls',
    status: 'in_progress',
    live: true,
    idle: false
  };
  await page.route('**/api/**', route => {
    const path = new URL(route.request().url()).pathname;
    if (path === '/api/tasks') return route.fulfill({
      json: {
        tasks: [task]
      }
    });
    if (path === '/api/tasks/controls/raw') return route.fulfill({
      json: {
        ...task,
        model: 'opus'
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
    if (path.endsWith('/meta')) return route.fulfill({
      json: {
        entries: [{
          namespace: 'pr',
          key: 'url',
          value: 'https://github.com/example/repo/pull/7'
        }]
      }
    });
    if (path.endsWith('/bridge')) return route.fulfill({
      json: {}
    });
    return route.fulfill({
      status: 404,
      json: {}
    });
  });
  await page.goto(process.env.WORKSPACE_TEST_URL);
  await page.locator('#token').fill('fixture');
  await page.locator('#connect-button').click();
  if (width < 1100) await page.locator('#toggle-nav').click();
  await page.locator('.task-row').first().click();
  await page.waitForFunction(() => document.querySelector('#composer-label').textContent === 'claude · opus');
  await page.locator('.task-header-links a').waitFor();
  return page;
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
for (const width of [1280, 2544, 390]) test(`composer controls share one axis and equal hit targets at ${width}px`, async () => {
  const page = await fixture(width);
  const geometry = await page.evaluate(() => {
    const box = el => {
      const r = el.getBoundingClientRect();
      return {
        height: r.height,
        center: r.y + r.height / 2,
        width: r.width
      };
    };
    return {
      items: ['composer-label', 'context-toggle', 'attach-files', 'send'].map(id => box(document.getElementById(id))),
      glyphs: [...document.querySelectorAll('.composer-actions svg,.context-ring')].map(box),
      border: getComputedStyle(document.querySelector('.composer-footer')).borderTopWidth,
      overflow: document.scrollingElement.scrollWidth > innerWidth
    };
  });
  assert.ok(Math.max(...geometry.items.map(x => x.center)) - Math.min(...geometry.items.map(x => x.center)) <= 1, JSON.stringify(geometry));
  for (const box of geometry.items) assert.equal(box.height, 32, JSON.stringify(geometry));
  for (const box of geometry.glyphs) {
    assert.equal(box.height, 16);
    assert.equal(box.width, 16);
    assert.ok(Math.abs(box.center - geometry.items[1].center) <= 1);
  }
  assert.equal(await page.locator('#draft').evaluate(el => getComputedStyle(el).outlineStyle), 'none');
  assert.equal(geometry.glyphs.length, 3);
  assert.equal(geometry.border, '0px');
  assert.equal(geometry.overflow, false);
  await page.screenshot({
    path: `.context/composer-controls-${width}.png`
  });
});
test('composer, header and terminal controls expose delegated tooltip attributes without native titles', async () => {
  const page = await fixture();
  for (const selector of ['#context-toggle', '#attach-files', '#send', '#split-toggle', '#fit-session', '.task-header-links button', '.task-header-links a', '#reconnect-terminal', '.secondary-toolbar button']) {
    assert.ok(await page.locator(selector).count(), selector);
    for (const control of await page.locator(selector).all()) {
      assert.ok(await control.getAttribute('data-tooltip'), selector);
      assert.ok(await control.getAttribute('aria-label'), selector);
      assert.equal(await control.getAttribute('title'), null, selector);
    }
  }
  assert.equal(await page.locator('#context-toggle').getAttribute('data-tooltip'), await page.locator('#context-toggle').getAttribute('aria-label'));
  assert.equal(await page.locator('#split-toggle').getAttribute('data-tooltip-shortcut'), '⌥⇧D');
  await page.evaluate(() => {
    document.querySelector('#split-toggle').disabled = false;
  });
  await page.locator('#split-toggle').click();
  assert.equal(await page.locator('#split-toggle').getAttribute('data-tooltip'), 'Close split view');
  assert.equal(await page.locator('#split-toggle').getAttribute('title'), null);
});
test('pending send preserves the same target and glyph geometry', async () => {
  const page = await fixture();
  await page.evaluate(async () => {
    const {
      TaskComposer
    } = await import('/composer/composer.js');
    window.pendingComposer = new TaskComposer(() => new Promise(() => {}));
    window.pendingComposer.select({
      id: 'controls',
      backend: 'claude'
    });
    window.pendingComposer.setInteractive(true);
  });
  await page.locator('#draft').fill('Hold this send');
  await page.locator('#send').click();
  await page.waitForFunction(() => document.querySelector('#send').getAttribute('aria-label') === 'Sending message');
  const boxes = await Promise.all(['#context-toggle', '#attach-files', '#send'].map(id => page.locator(id).boundingBox()));
  for (const box of boxes) {
    assert.equal(box.width, 32);
    assert.equal(box.height, 32);
    assert.ok(Math.abs(box.y - boxes[0].y) <= 1);
  }
  assert.equal(await page.locator('#send svg').evaluate(el => el.getBoundingClientRect().height), 16);
  assert.equal(await page.locator('#send').getAttribute('data-tooltip'), 'Sending message');
});
for (const width of [1280, 2544, 390]) test(`shared tooltips render for composer and header controls at ${width}px`, async () => {
  const page = await fixture(width),
    bubble = page.locator('#workspace-tooltip');
  for (const selector of ['#context-toggle', '#attach-files', '#send', '.task-header-links button', '.task-header-links a', '#split-toggle', '#fit-session', '#reconnect-terminal']) {
    const control = page.locator(selector);
    await control.hover();
    await bubble.waitFor({
      state: 'visible'
    });
    assert.equal(await bubble.locator('span').textContent(), await control.getAttribute('data-tooltip'), selector);
    assert.match(await control.getAttribute('aria-describedby'), /workspace-tooltip/);
    const box = await bubble.boundingBox();
    assert.ok(box.x >= 0 && box.y >= 0 && box.x + box.width <= width && box.y + box.height <= 900, JSON.stringify(box));
    if (selector === '#split-toggle') assert.equal(await bubble.locator('kbd').textContent(), '⌥⇧D');
    await page.keyboard.press('Escape');
    assert.equal(await bubble.isVisible(), false);
  }
  await page.keyboard.press('Tab');
  await page.locator('#context-toggle').focus();
  await bubble.waitFor({
    state: 'visible'
  });
  assert.equal(await bubble.locator('span').textContent(), await page.locator('#context-toggle').getAttribute('data-tooltip'));
  await bubble.evaluate(el => Promise.all(el.getAnimations().map(animation => animation.finished)));
  await page.screenshot({
    path: `.context/composer-tooltip-${width}.png`
  });
  await page.keyboard.press('Escape');
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
