import { test } from '@playwright/test';
import { resetServer } from '../../_helpers';
import assert from 'node:assert/strict';
let browser, frontend, longPath, task;
const cleanup = [];
async function bounds(page) {
  return page.evaluate(() => {
    const root = document.scrollingElement,
      tools = document.querySelector('#tools'),
      main = document.querySelector('#main');
    return {
      scroll: root.scrollWidth,
      client: root.clientWidth,
      left: root.scrollLeft,
      viewport: innerWidth,
      toolsRight: tools.getBoundingClientRect().right,
      mainWidth: main.getBoundingClientRect().width,
      panelWidth: tools.clientWidth,
      panelScroll: tools.scrollWidth
    };
  });
}
async function fits(page) {
  const value = await bounds(page);
  assert.ok(value.scroll <= value.client, JSON.stringify(value));
  assert.ok(value.toolsRight <= value.viewport + 1, JSON.stringify(value));
  assert.equal(value.left, 0, JSON.stringify(value));
}
test.beforeAll(async ({
  browser: workerBrowser
}) => {
  browser = workerBrowser;
  await resetServer();
  frontend = process.env.WORKSPACE_TEST_URL || 'http://127.0.0.1:17750';
  longPath = 'nested/' + 'long-file-name-'.repeat(60) + '.md';
  task = {
    id: 'overflow-fixture',
    name: 'Long paths',
    project: 'fixture',
    branch: 'feature/' + 'long-branch-'.repeat(80),
    status: 'complete',
    live: false
  };
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
for (const width of [320, 390, 900, 1100, 1280]) test(`workspace contains long paths at viewport ${width}`, async () => {
  const page = await browser.newPage({
    viewport: {
      width,
      height: 900
    }
  });
  cleanup.push(() => page.close());
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.addInitScript(() => localStorage.setItem('argus-workspace:layout', JSON.stringify({
    nav: true,
    tools: true,
    navWidth: 380,
    toolsWidth: 520
  })));
  await page.route('**/api/**', route => route.fulfill({
    status: 404,
    json: {}
  }));
  await page.route('**/api/tasks?archived=all', route => route.fulfill({
    json: {
      tasks: [task]
    }
  }));
  await page.route('**/api/projects/full', route => route.fulfill({
    json: {
      projects: [{
        name: 'fixture'
      }]
    }
  }));
  await page.route('**/api/tasks/*/size', route => route.fulfill({
    json: {
      cols: 240,
      rows: 40
    }
  }));
  await page.route('**/api/tasks/*/output?*', route => route.fulfill({
    body: 'Recorded terminal output '.repeat(100),
    headers: {
      'X-Output-Total': '2500'
    }
  }));
  await page.route('**/api/tasks/*/git/status', route => route.fulfill({
    json: {
      Status: Array.from({
        length: 67
      }, (_, index) => `?? ${index}-${longPath}`).join('\n')
    }
  }));
  await page.route('**/api/tasks/*/git/diff?*', route => route.fulfill({
    json: {
      Diff: `diff --git a/a b/a\n@@ -1 +1 @@\n-${'before'.repeat(200)}\n+${'after'.repeat(200)}`
    }
  }));
  await page.goto(frontend);
  await page.locator('#token').fill('fixture-token');
  await page.locator('#connect-button').click();
  await page.locator('#disconnect:visible').waitFor();
  if (width < 1100) await page.locator('#toggle-tools').click();
  await page.locator('.file-row').first().waitFor();
  await fits(page);
  await page.locator('#focus-agent').click();
  await fits(page);
  await page.locator('#focus-agent').click();
  await fits(page);
  await page.locator('.file-row').first().click();
  if (width < 1100) await page.locator('#toggle-tools').click();
  await page.locator('.diff-table').waitFor();
  await fits(page);
  if (width < 1100) await page.locator('#toggle-tools').click();
  await page.getByRole('button', {
    name: 'Split',
    exact: true
  }).click();
  await fits(page);
  await page.locator('.diff-content').evaluate(element => {
    element.scrollLeft = element.scrollWidth;
  });
  await fits(page);
  for (const nextWidth of [1440, 1100, 900, 390, width]) {
    await page.setViewportSize({
      width: nextWidth,
      height: 900
    });
    await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
    if ((await page.locator('#toggle-tools').getAttribute('aria-expanded')) !== 'true') await page.locator('#toggle-tools').click();
    await fits(page);
    if (nextWidth >= 1100) {
      for (const separator of ['#tools-resize', '#nav-resize']) {
        await page.locator(separator).focus();
        await page.keyboard.press('End');
        await fits(page);
      }
      const before = await bounds(page);
      await page.locator('#toggle-nav').click();
      const navHidden = await bounds(page);
      assert.ok(navHidden.mainWidth > before.mainWidth, `Hiding navigation must grow the terminal column: ${JSON.stringify({
        before,
        navHidden
      })}`);
      assert.ok(Math.abs(navHidden.toolsRight - navHidden.viewport) <= 1, JSON.stringify(navHidden));
      await fits(page);
      await page.locator('#toggle-nav').click();
      assert.equal((await bounds(page)).mainWidth, before.mainWidth);
      await page.locator('#toggle-tools').click();
      await page.locator('#toggle-tools').click();
      await fits(page);
      assert.equal((await bounds(page)).mainWidth, before.mainWidth);
      await page.locator('#focus-agent').click();
      assert.equal((await bounds(page)).mainWidth, nextWidth);
      await fits(page);
      await page.locator('#focus-agent').click();
      assert.equal((await bounds(page)).mainWidth, before.mainWidth);
    }
  }
  assert.deepEqual(await page.locator('.task-header-links button').first().evaluate(element => {
    const style = getComputedStyle(element);
    return {
      whiteSpace: style.whiteSpace,
      overflow: style.overflowX,
      textOverflow: style.textOverflow
    };
  }), {
    whiteSpace: 'nowrap',
    overflow: 'hidden',
    textOverflow: 'ellipsis'
  });
  assert.equal(await page.locator('.branch-label').count(), 0);
  const line = page.locator('.file-line').first(),
    name = page.locator('.file-name').first();
  assert.ok(await line.evaluate(element => element.scrollWidth > element.clientWidth));
  assert.equal(await name.textContent(), longPath.split('/').at(-1));
  assert.equal(await name.evaluate(element => element.scrollWidth === element.clientWidth), true);
  assert.ok((await line.boundingBox()).x + (await line.boundingBox()).width <= (await bounds(page)).toolsRight);
  assert.deepEqual(errors, []);
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
