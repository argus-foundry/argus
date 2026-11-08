import { test } from '@playwright/test';
import { resetServer } from '../../../_helpers';
import assert from 'node:assert/strict';
test('workspace shortcuts mac-keyboard-live', async ({
  browser
}) => {
  await resetServer();
  const frontend = process.env.WORKSPACE_TEST_URL,
    daemon = process.env.WORKSPACE_TEST_API;
  const headers = {
    Authorization: 'Bearer test-token'
  };
  const tasks = (await (await fetch(daemon + '/api/tasks?archived=all', {
    headers
  })).json()).tasks;
  const task = tasks.find(task => task.name === 'echo-bash');
  assert.ok(task?.worktree_path.includes('/argus-test-home-'), 'Isolated harness required');
  const second = await (await fetch(daemon + '/api/tasks', {
    method: 'POST',
    headers: {
      ...headers,
      'Content-Type': 'application/json'
    },
    body: JSON.stringify({
      name: 'keyboard-cache-' + Date.now(),
      project: 'test-proj',
      backend: 'bash-test'
    })
  })).json();
  const page = await browser.newPage({
    viewport: {
      width: 1440,
      height: 900
    }
  });
  const writes = [],
    reads = [];
  try {
    await page.addInitScript(() => {
      const Original = window.EventSource;
      window.activeTerminalStreams = 0;
      window.maxTerminalStreams = 0;
      window.EventSource = class extends Original {
        constructor(...args) {
          super(...args);
          this.counted = true;
          window.activeTerminalStreams++;
          window.maxTerminalStreams = Math.max(window.maxTerminalStreams, window.activeTerminalStreams);
        }
        close() {
          if (this.counted) {
            this.counted = false;
            window.activeTerminalStreams--;
          }
          super.close();
        }
      };
    });
    page.on('request', request => {
      if (new URL(request.url()).pathname.endsWith('/output')) reads.push(new URL(request.url()).pathname);
      if (request.method() === 'POST' && new URL(request.url()).pathname.endsWith('/input')) writes.push(request.postData());
    });
    await page.goto(frontend);
    await page.locator('#token').fill('test-token');
    await page.locator('#connect-button').click();
    await page.locator('.task-row').filter({
      hasText: 'echo-bash'
    }).click();
    await page.waitForFunction(() => document.querySelector('#terminal-state').textContent === 'Live terminal');
    await page.evaluate(() => {
      window.retainedTerminal = document.querySelector('#terminal .xterm');
    });
    await page.locator('.task-row').filter({
      hasText: second.name
    }).click();
    await page.waitForFunction(() => document.querySelector('#terminal-state').textContent === 'Live terminal');
    await page.locator('.task-row').filter({
      hasText: 'echo-bash'
    }).click();
    await page.waitForFunction(() => document.querySelector('#terminal-state').textContent === 'Live terminal');
    assert.equal(await page.evaluate(() => window.retainedTerminal === document.querySelector('#terminal .xterm')), true);
    assert.equal(reads.filter(path => path === '/api/tasks/' + task.id + '/output').length, 2);
    assert.equal(await page.evaluate(() => window.maxTerminalStreams), 1);
    await page.locator('.xterm-helper-textarea').focus();
    for (const key of ['Shift+ArrowUp', 'Shift+ArrowDown', 'Shift+PageUp', 'Shift+PageDown', 'Shift+End']) await page.keyboard.press(key);
    assert.deepEqual(writes, []);
    await page.keyboard.press('Meta+ArrowLeft');
    assert.equal(await page.locator('#tool-status').getAttribute('aria-selected'), 'true');
    assert.deepEqual(writes, []);
    await page.locator('.xterm-helper-textarea').focus();
    await page.keyboard.type('printf "KEYBOARD_%s\\n" "SENTINEL"');
    await page.keyboard.press('Enter');
    let output = '';
    const deadline = Date.now() + 15000;
    while (Date.now() < deadline) {
      output = await (await fetch(daemon + '/api/tasks/' + task.id + '/output', {
        headers
      })).text();
      if (output.includes('KEYBOARD_SENTINEL')) break;
      await new Promise(resolve => setTimeout(resolve, 100));
    }
    assert.ok(output.includes('KEYBOARD_SENTINEL'), 'Independent daemon output must contain executed shell result');
    assert.ok(writes.length > 0);
    assert.equal(writes.join('').includes('printf'), true);
    console.log('PASS actual xterm shortcuts produce zero input requests; ordinary typing produces independently verified shell output');
  } finally {
    const removed = await fetch(daemon + '/api/tasks/' + encodeURIComponent(second.id), {
      method: 'DELETE',
      headers
    });
    assert.equal(removed.ok, true, 'Remove only the isolated task this test created');
  }
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
