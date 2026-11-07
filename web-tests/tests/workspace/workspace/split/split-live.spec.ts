import { test } from '@playwright/test';
import { resetServer } from '../../../_helpers';
import assert from 'node:assert/strict';
test('workspace split split-live', async ({
  browser
}) => {
  await resetServer();
  const api = process.env.WORKSPACE_TEST_API || 'http://127.0.0.1:18744',
    frontend = process.env.WORKSPACE_TEST_URL || 'http://127.0.0.1:18750';
  const headers = {
    Authorization: 'Bearer test-token',
    'Content-Type': 'application/json'
  };
  const initial = (await (await fetch(api + '/api/tasks?archived=all', {
    headers
  })).json()).tasks.find(task => task.name === 'echo-bash');
  assert.ok(initial?.worktree_path.includes('/argus-test-home-'), 'Refusing a daemon without the isolated harness worktree');
  const second = await (await fetch(api + '/api/tasks', {
    method: 'POST',
    headers,
    body: JSON.stringify({
      project: 'test-proj',
      backend: 'bash-test',
      name: 'split-second-' + Date.now()
    })
  })).json();
  const page = await browser.newPage({
      viewport: {
        width: 1440,
        height: 900
      }
    }),
    writes = [];
  try {
    page.on('request', request => {
      if (request.method() === 'POST' && new URL(request.url()).pathname.endsWith('/input')) writes.push({
        path: new URL(request.url()).pathname,
        body: request.postData()
      });
    });
    await page.goto(frontend);
    await page.locator('#token').fill('test-token');
    await page.locator('#connect-button').click();
    await page.locator('.task-row').filter({
      hasText: 'echo-bash'
    }).click();
    await page.waitForFunction(() => document.querySelector('#terminal-state').textContent === 'Live terminal');
    await page.locator('#terminal .xterm-helper-textarea').focus();
    await page.keyboard.press('Meta+Shift+Enter');
    await page.locator('#secondary-pane').waitFor();
    assert.deepEqual(writes, []);
    assert.equal(await page.locator('#secondary-task-picker').inputValue(), '');
    await page.locator('#secondary-task-picker').selectOption(second.id);
    await page.waitForFunction(() => document.querySelector('#secondary-terminal-state').textContent === 'Live terminal');
    await page.locator('#secondary-terminal .xterm-helper-textarea').focus();
    await page.keyboard.type('printf "SPLIT_%s\\n" "RIGHT_DONE"');
    await page.keyboard.press('Enter');
    let output = '';
    const deadline = Date.now() + 15000;
    while (Date.now() < deadline) {
      output = await (await fetch(api + '/api/tasks/' + second.id + '/output', {
        headers
      })).text();
      if (output.includes('SPLIT_RIGHT_DONE')) break;
      await new Promise(resolve => setTimeout(resolve, 100));
    }
    assert.ok(output.includes('SPLIT_RIGHT_DONE'), 'Independent secondary task output must confirm execution');
    assert.ok(writes.length > 0);
    assert.ok(writes.every(write => write.path === '/api/tasks/' + second.id + '/input'));
    await page.evaluate(() => window.retainedRight = document.querySelector('#secondary-terminal .xterm'));
    const beforeToggle = writes.length;
    await page.locator('#secondary-terminal .xterm-helper-textarea').focus();
    await page.keyboard.press('Meta+Shift+Enter');
    assert.equal(await page.locator('#secondary-pane').isVisible(), false);
    assert.equal(writes.length, beforeToggle);
    assert.equal(await page.locator('#terminal .xterm-helper-textarea').evaluate(input => input === document.activeElement), true);
    await page.locator('#split-toggle').click();
    await page.waitForFunction(() => document.querySelector('#secondary-terminal .xterm') === window.retainedRight);
    assert.equal(await page.locator('#task-title').textContent(), 'echo-bash');
    console.log('PASS explicit split shortcut sends no PTY bytes; secondary input targets only its captured task and closing retains its terminal');
  } finally {
    const response = await fetch(api + '/api/tasks/' + second.id, {
      method: 'DELETE',
      headers
    });
    assert.equal(response.ok, true);
  }
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
