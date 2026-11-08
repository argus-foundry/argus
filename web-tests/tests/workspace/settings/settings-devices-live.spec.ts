import { test } from '@playwright/test';
import { resetServer } from '../../_helpers';
import { confirmClick } from '../_confirmation-fixture.ts';
import assert from 'node:assert/strict';
test('settings settings-devices-live', async ({
  browser
}) => {
  await resetServer();
  const frontend = process.env.WORKSPACE_TEST_URL || 'http://127.0.0.1:17750',
    daemon = process.env.WORKSPACE_TEST_API || 'http://127.0.0.1:17744';
  const headers = {
    Authorization: 'Bearer test-token'
  };
  const tasks = (await (await fetch(daemon + '/api/tasks?archived=all', {
    headers
  })).json()).tasks;
  assert.ok(tasks.some(task => task.name === 'echo-bash' && task.backend === 'bash-test' && task.worktree_path.includes('/argus-test-home-')), 'Refusing token writes outside isolated harness');
  const label = 'workspace-device-' + Date.now();
  const page = await browser.newPage();
  let tokenId;
  try {
    await page.goto(frontend);
    await page.locator('#token').fill('test-token');
    await page.locator('#connect-button').click();
    await page.locator('#disconnect:visible').waitFor();
    await page.locator('#workspace-settings').click();
    await page.locator('.settings-nav').getByRole('button', {
      name: 'Notifications & devices',
      exact: true
    }).click();
    await page.getByLabel('Token label', {
      exact: true
    }).fill(label);
    await page.getByRole('button', {
      name: 'Create device token',
      exact: true
    }).click();
    await page.getByLabel('New device token', {
      exact: true
    }).waitFor();
    const token = await page.getByLabel('New device token', {
      exact: true
    }).inputValue();
    assert.match(token, /^[a-f0-9]{64}$/);
    const roster = await (await fetch(daemon + '/api/tokens', {
      headers
    })).json();
    tokenId = roster.tokens.find(item => item.label === label)?.id;
    assert.ok(tokenId);
    assert.equal((await fetch(daemon + '/api/tasks', {
      headers: {
        Authorization: 'Bearer ' + token
      }
    })).status, 200);
    await confirmClick(page, () => page.getByRole('button', {
      name: 'Revoke ' + label,
      exact: true
    }).click(), dialog => dialog.accept());
    await page.getByRole('button', {
      name: 'Revoke ' + label,
      exact: true
    }).waitFor({
      state: 'detached'
    });
    assert.equal((await fetch(daemon + '/api/tasks', {
      headers: {
        Authorization: 'Bearer ' + token
      }
    })).status, 401);
    console.log('PASS real isolated token mint, independent authentication, revoke and rejection');
    await page.getByLabel('Original Argus URL', {
      exact: true
    }).fill(daemon);
    await page.getByRole('button', {
      name: 'Generate share URL',
      exact: true
    }).click();
    const share = new URL(await page.getByLabel('iOS share URL', {
      exact: true
    }).inputValue());
    assert.equal(share.pathname, '/share');
    assert.equal(share.searchParams.get('token'), 'test-token');
    assert.equal(share.searchParams.get('text'), '[Shortcut Input]');
    await page.getByRole('button', {
      name: 'Hide share URL',
      exact: true
    }).click();
    console.log('PASS isolated-token share URL generation without navigating or sending it');
    assert.equal(await page.evaluate(async () => {
      const registration = await navigator.serviceWorker.register('/workspace-push.js', {
        scope: '/'
      });
      await navigator.serviceWorker.ready;
      const active = !!registration.active;
      await registration.unregister();
      return active;
    }), true);
    console.log('PASS real local push-worker registration and activation without permission or push traffic');
  } finally {
    if (tokenId) await fetch(daemon + '/api/tokens/' + tokenId, {
      method: 'DELETE',
      headers
    });
  }
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
