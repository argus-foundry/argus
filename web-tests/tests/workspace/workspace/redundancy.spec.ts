import { test } from '@playwright/test';
import { resetServer } from '../../_helpers';
import assert from 'node:assert/strict';
test('workspace redundancy', async ({
  browser
}) => {
  await resetServer();
  {
    const page = await browser.newPage();
    await page.goto(process.env.WORKSPACE_TEST_URL);
    await page.evaluate(async () => {
      document.querySelector('#connect-dialog').close();
      const {
        confirmDialog
      } = await import('/shared/confirm-dialog.js');
      const origin = document.createElement('button');
      origin.id = 'origin';
      origin.textContent = 'Open confirmation';
      document.body.append(origin);
      origin.focus();
      window.tries = 0;
      window.openConfirmation = () => confirmDialog({
        title: 'Captured task',
        message: 'Keep the captured target.',
        onConfirm: async () => {
          if (++window.tries === 1) throw new Error('Unconfirmed fixture write.');
        }
      });
      window.pending = window.openConfirmation();
    });
    const dialog = page.locator('#confirm-dialog');
    assert.equal(await dialog.getByRole('button', {
      name: 'Cancel',
      exact: true
    }).evaluate(node => node === document.activeElement), true);
    await dialog.getByRole('button', {
      name: 'Confirm',
      exact: true
    }).click();
    await dialog.locator('.error-text:not([hidden])').waitFor();
    assert.equal(await page.evaluate(() => window.tries), 1);
    await dialog.getByRole('button', {
      name: 'Confirm',
      exact: true
    }).click();
    await dialog.waitFor({
      state: 'detached'
    });
    assert.equal(await page.evaluate(() => window.pending), true);
    assert.equal(await page.locator('#origin').evaluate(node => node === document.activeElement), true);
    await page.evaluate(() => {
      window.pending = window.openConfirmation();
    });
    await page.keyboard.press('Escape');
    await dialog.waitFor({
      state: 'detached'
    });
    assert.equal(await page.evaluate(() => window.pending), false);
    assert.equal(await page.evaluate(() => window.tries), 2);
    await page.evaluate(async () => {
      const {
        confirmDialog
      } = await import('/shared/confirm-dialog.js');
      window.abort = new AbortController();
      window.pending = confirmDialog({
        title: 'Abort',
        signal: window.abort.signal
      });
      window.abort.abort();
    });
    assert.equal(await page.evaluate(() => window.pending), false);
    assert.equal(await dialog.count(), 0);
    await page.evaluate(async () => {
      const {
        copyNotice
      } = await import('/shared/clipboard.js');
      window.copies = [];
      Object.defineProperty(navigator, 'clipboard', {
        configurable: true,
        value: {
          writeText: async value => window.copies.push(value)
        }
      });
      await copyNotice('/tmp/task/src/a.go', 'Copied absolute path.');
    });
    assert.deepEqual(await page.evaluate(() => window.copies), ['/tmp/task/src/a.go']);
    assert.equal(await page.locator('#notice').textContent(), 'Copied absolute path.');
    await page.evaluate(async () => {
      const {
        copyNotice
      } = await import('/shared/clipboard.js');
      navigator.clipboard.writeText = async () => {
        throw new Error('denied');
      };
      await copyNotice('fixture');
    });
    assert.match(await page.locator('#notice').textContent(), /Copy failed/);
    console.log('PASS shared confirmation retries, Escape, abort, focus restore and clipboard success/failure');
  }
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
