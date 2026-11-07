import { test } from '@playwright/test';
import { resetServer } from '../../_helpers';
import assert from 'node:assert/strict';
let browser, frontend, task;
const cleanup = [];
async function fixture(viewport) {
  const page = await browser.newPage({
    viewport
  });
  cleanup.push(() => page.close());
  const errors = [],
    resizes = [];
  page.on('pageerror', e => errors.push(e.message));
  page.on('request', r => {
    if (new URL(r.url()).pathname.endsWith('/resize')) resizes.push(r.url());
  });
  cleanup.push(() => {
    assert.deepEqual(errors, []);
    assert.deepEqual(resizes, [], 'Recorded terminal layouts must never resize the PTY');
  });
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
      projects: []
    }
  }));
  await page.route('**/api/hera', route => route.fulfill({
    json: {
      orchestrators: []
    }
  }));
  await page.route('**/api/tasks/*/size', route => route.fulfill({
    json: {
      cols: 80,
      rows: 24
    }
  }));
  await page.route('**/api/tasks/*/output?*', route => route.fulfill({
    body: 'W'.repeat(80) + '\r\n',
    headers: {
      'X-Output-Total': '32'
    }
  }));
  await page.goto(frontend);
  await page.locator('#token').fill('fixture-token');
  await page.locator('#connect-button').click();
  await page.locator('.xterm-screen').waitFor();
  return page;
}
async function measure(page) {
  await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
  return page.evaluate(() => {
    const rect = id => {
      const r = document.querySelector(id).getBoundingClientRect();
      return {
        x: r.x,
        y: r.y,
        width: r.width,
        height: r.height,
        right: r.right,
        bottom: r.bottom
      };
    };
    const terminal = document.querySelector('#terminal');
    return {
      viewport: innerWidth,
      document: document.scrollingElement.scrollWidth,
      client: document.scrollingElement.clientWidth,
      columns: ['#navigation', '#main', '#tools'].filter(id => document.querySelector(id).getClientRects().length).map(rect),
      header: rect('.workspace-header'),
      disconnect: rect('#disconnect'),
      pane: rect('#terminal'),
      host: rect('.terminal-host'),
      screen: rect('.xterm-screen'),
      terminalClient: terminal.clientWidth,
      terminalScroll: terminal.scrollWidth,
      fit: document.querySelector('#fit-session').dataset.fitNeeded
    };
  });
}
function contained(value) {
  assert.ok(value.document <= value.client, JSON.stringify(value));
  for (const box of [...value.columns, value.header, value.disconnect]) assert.ok(box.right <= value.viewport + 1, JSON.stringify(value));
}
test.beforeAll(async ({
  browser: workerBrowser
}) => {
  browser = workerBrowser;
  await resetServer();
  frontend = process.env.WORKSPACE_TEST_URL || 'http://127.0.0.1:17750';
  task = {
    id: 'responsive',
    name: 'Wide display task',
    project: 'fixture',
    backend: 'fixture-shell',
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
for (const viewport of [{
  width: 1280,
  height: 800
}, {
  width: 1920,
  height: 1080
}, {
  width: 2560,
  height: 1440
}, {
  width: 3440,
  height: 1440
}]) {
  test(`fluid columns and terminal host at ${viewport.width}x${viewport.height}`, async () => {
    const page = await fixture(viewport);
    for (const action of [null, '#toggle-tools', '#toggle-tools', '#toggle-nav', '#toggle-nav', '#focus-agent', '#focus-agent']) {
      if (action) await page.locator(action).click();
      const value = await measure(page);
      contained(value);
      assert.ok(value.terminalScroll <= value.terminalClient + 1, 'A narrower PTY must not create a horizontal scrollbar: ' + JSON.stringify(value));
      assert.ok(value.host.width >= value.pane.width - 50, 'Client host must fill available pane width: ' + JSON.stringify(value));
      assert.ok(value.host.height >= value.pane.height - 30, 'Client host must fill available pane height: ' + JSON.stringify(value));
      assert.equal(value.fit, 'true', 'Mismatched PTY must expose a visible fit hint');
    }
    if (viewport.width === 2560) await page.screenshot({
      path: '.context/responsive-2560.png'
    });
    await page.setViewportSize({
      width: 1280,
      height: 800
    });
    contained(await measure(page));
    await page.locator('#disconnect').click();
    await page.setViewportSize({
      width: viewport.width,
      height: viewport.height
    });
    assert.equal(await page.locator('#fit-session').getAttribute('data-fit-needed'), 'false');
  });
}
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
