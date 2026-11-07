import { test } from '@playwright/test';
import { resetServer } from '../../_helpers';
import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
import path from 'node:path';
import { railAppearance } from './rail/_rail-browser.ts';
test('workspace verify', async ({
  browser
}) => {
  await resetServer();
  const root = path.resolve(__dirname, "../../../..");
  const frontend = process.env.WORKSPACE_TEST_URL || 'http://127.0.0.1:17750';
  const daemon = process.env.WORKSPACE_TEST_API || 'http://127.0.0.1:17744';
  const headers = {
    Authorization: 'Bearer test-token',
    'Content-Type': 'application/json'
  };
  const initial = (await (await fetch(daemon + '/api/tasks?archived=all', {
    headers
  })).json()).tasks.find(task => task.name === 'echo-bash');
  assert.ok(initial?.backend === 'bash-test' && initial.worktree_path.includes('/argus-test-home-'), 'Refusing visual checks outside the isolated harness');
  const response = await fetch(daemon + '/api/tasks', {
    method: 'POST',
    headers,
    body: JSON.stringify({
      name: 'Review workspace polish',
      project: 'test-proj',
      backend: 'bash-test'
    })
  });
  assert.equal(response.ok, true);
  const other = await response.json();
  const page = await browser.newPage({
    viewport: {
      width: 1440,
      height: 900
    },
    deviceScaleFactor: 2
  });
  const errors = [],
    input = [];
  page.on('pageerror', error => errors.push(error.message));
  page.on('popup', () => errors.push('Unexpected popup'));
  page.on('request', request => {
    if (request.method() === 'POST' && new URL(request.url()).pathname.endsWith('/input')) input.push(request);
  });
  await mkdir(path.join(root, '.context'), {
    recursive: true
  });
  const capture = async (name, locator) => {
    const options = {
      path: path.join(root, '.context', `verify-${name}.png`)
    };
    if (locator) await locator.screenshot(options);else await page.screenshot(options);
    console.log(`PASS .context/verify-${name}.png`);
  };
  {
    await page.goto(frontend);
    assert.equal(await page.getByLabel('Remember on this device').isChecked(), true);
    await page.locator('#token').fill('test-token');
    await page.locator('#connect-button').click();
    await page.locator('#disconnect:visible').waitFor();
    await page.locator(`.task-row[data-task-id="${initial.id}"]`).click();
    await page.waitForFunction(() => document.querySelector('#terminal-state').textContent === 'Live terminal');
    await railAppearance(page, other);
    await capture('rail-hover', page.locator('#navigation'));
    await page.getByRole('button', {
      name: `Archive ${other.name}`,
      exact: true
    }).click();
    assert.equal(await page.locator('#archive-dialog-title').textContent(), `You're about to archive ${other.name}.`);
    await capture('archive-dialog', page.locator('#archive-dialog'));
    await page.getByRole('button', {
      name: 'Cancel',
      exact: true
    }).click();
    assert.equal(await page.evaluate(() => localStorage.getItem('argus-workspace-token')), 'test-token');
    await page.reload();
    await page.locator('#disconnect:visible').waitFor();
    assert.equal(await page.locator('#connect-dialog').evaluate(dialog => dialog.open), false);
    await page.waitForFunction(() => document.querySelector('#terminal-state').textContent === 'Live terminal');
    await capture('reload-connected');
    const prevented = await page.locator('#draft').evaluate(field => {
      const data = new DataTransfer();
      const bytes = Uint8Array.from(atob('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl6ZQAAAABJRU5ErkJggg=='), char => char.charCodeAt(0));
      data.items.add(new File([bytes], 'Screenshot.png', {
        type: 'image/png'
      }));
      return !field.dispatchEvent(new DragEvent('drop', {
        dataTransfer: data,
        bubbles: true,
        cancelable: true
      }));
    });
    assert.equal(prevented, true);
    await page.waitForFunction(() => document.querySelector('.attachment-chip')?.dataset.state === 'ready' && document.querySelector('.attachment-chip img')?.naturalWidth > 0);
    await page.locator('#draft').fill('Please review this screenshot.');
    assert.deepEqual(input, []);
    assert.equal(await page.locator('#send').isEnabled(), true);
    await capture('attachment-chip', page.locator('.composer-wrap'));
    await page.locator('#disconnect').click();
    assert.deepEqual(errors, []);
    console.log('Verified four visual states on the merged workspace.');
  }
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
