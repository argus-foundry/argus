import { test } from '@playwright/test';
import { resetServer } from '../../_helpers';
import assert from 'node:assert/strict';
test('terminal overflow-live', async ({
  browser
}) => {
  await resetServer();
  const frontend = process.env.WORKSPACE_TEST_URL || 'http://127.0.0.1:17750';
  const daemon = process.env.WORKSPACE_TEST_API || 'http://127.0.0.1:17744';
  const headers = {
    Authorization: 'Bearer test-token'
  };
  const task = (await (await fetch(daemon + '/api/tasks?archived=all', {
    headers
  })).json()).tasks.find(task => task.name === 'echo-bash');
  assert.ok(task?.backend === 'bash-test' && task.worktree_path.includes('/argus-test-home-'), 'Refusing live resize outside the isolated harness');
  const page = await browser.newPage({
    viewport: {
      width: 1800,
      height: 1000
    }
  });
  const errors = [],
    resizes = [];
  page.on('pageerror', error => errors.push(error.message));
  page.on('request', request => {
    if (new URL(request.url()).pathname.endsWith('/resize')) resizes.push(request);
  });
  async function fits() {
    const value = await page.evaluate(() => {
      const root = document.scrollingElement;
      return {
        scroll: root.scrollWidth,
        client: root.clientWidth,
        left: root.scrollLeft,
        right: document.querySelector('#tools').getBoundingClientRect().right,
        viewport: innerWidth,
        main: document.querySelector('#main').clientWidth
      };
    });
    assert.ok(value.scroll <= value.client && value.right <= value.viewport + 1 && value.left === 0, JSON.stringify(value));
    assert.ok(value.main > 0, JSON.stringify(value));
    return value;
  }
  {
    await page.addInitScript(() => localStorage.setItem('argus-workspace:layout', JSON.stringify({
      nav: true,
      tools: false
    })));
    await page.route('**/api/tasks?archived=all', route => route.fulfill({
      json: {
        tasks: [{
          ...task,
          idle: false,
          needs_input: false,
          branch: 'argus/' + 'long-branch-name-'.repeat(30)
        }]
      }
    }));
    await page.route('**/api/tasks/' + task.id, route => route.fulfill({
      json: {
        ...task,
        idle: false,
        needs_input: false
      }
    }));
    await page.route('**/api/tasks/*/git/status', route => route.fulfill({
      json: {
        Status: Array.from({
          length: 67
        }, (_, index) => ` M long-directory/${index}-${'long-filename-'.repeat(30)}.js`).join('\n')
      }
    }));
    await page.goto(frontend);
    await page.locator('#token').fill('test-token');
    await page.locator('#connect-button').click();
    await page.waitForFunction(() => document.querySelector('#terminal-state').textContent === 'Live terminal');
    const resized = page.waitForResponse(response => new URL(response.url()).pathname.endsWith('/resize'));
    await page.locator('#fit-session').click();
    assert.equal((await resized).ok(), true);
    await page.waitForFunction(() => !document.querySelector('#fit-session').disabled);
    const wide = await (await fetch(`${daemon}/api/tasks/${task.id}/size`, {
      headers
    })).json();
    assert.ok(wide.cols > 120, 'Fit must create a genuinely wide live terminal');
    const proof = 'OVERFLOW_LIVE_' + 'x'.repeat(400);
    await page.locator('#draft').fill(`printf 'OVERFLOW_LIVE_%s\\n' '${'x'.repeat(400)}'`);
    await page.locator('#send').click();
    await page.waitForFunction(() => document.querySelector('#draft').value === '');
    const output = await fetch(`${daemon}/api/tasks/${task.id}/output?bytes=1048576`, {
      headers
    });
    assert.ok((await output.text()).split(/\r?\n/).includes(proof), 'Wide output must actually execute in bash');
    await page.locator('#toggle-tools').click();
    await page.locator('.file-row').first().waitFor();
    await fits();
    for (const width of [1440, 1100, 900, 390, 320, 1280]) {
      await page.setViewportSize({
        width,
        height: 900
      });
      await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
      if ((await page.locator('#toggle-tools').getAttribute('aria-expanded')) !== 'true') await page.locator('#toggle-tools').click();
      await fits();
      await page.locator('.cached-terminal').evaluate(element => {
        element.scrollLeft = element.scrollWidth;
      });
      await fits();
      await page.locator('.cached-terminal').evaluate(element => {
        element.scrollLeft = 0;
      });
      if (width >= 1100) {
        assert.ok(await page.locator('.cached-terminal').evaluate(element => element.scrollWidth > element.clientWidth), 'The old wide terminal must scroll locally');
        const before = await fits();
        await page.locator('#toggle-nav').click();
        assert.ok((await fits()).main > before.main);
        await page.locator('#toggle-nav').click();
        assert.equal((await fits()).main, before.main);
      }
      console.log(`PASS live wide Fit stays contained at ${width}px with 67 long paths`);
    }
    const final = await (await fetch(`${daemon}/api/tasks/${task.id}/size`, {
      headers
    })).json();
    assert.equal(final.cols, wide.cols);
    assert.equal(resizes.length, 1, 'Known busy layout changes must defer automatic PTY resize');
    await page.locator('#disconnect').click();
    assert.deepEqual(errors, []);
  }
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
