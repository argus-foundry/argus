import { test } from '@playwright/test';
import { resetServer } from '../../_helpers';
// Isolated clipboard events exercise diagnostics without reading the OS clipboard or contacting an agent.
import assert from 'node:assert/strict';
test('diagnostics wispr-check', async ({
  browser
}) => {
  await resetServer();
  const base = process.env.WORKSPACE_TEST_URL || 'http://127.0.0.1:17750';
  {
    const page = await browser.newPage(),
      requests = [];
    page.on('request', request => requests.push(new URL(request.url()).pathname));
    await page.goto(base + '/diagnostics/wispr-check.html');
    await page.locator('#dictation').fill('existing');
    await page.locator('#dictation').evaluate(field => {
      window.addEventListener('beforeinput', event => event.preventDefault(), {
        once: true
      });
      field.dispatchEvent(new InputEvent('beforeinput', {
        inputType: 'insertFromPaste',
        bubbles: true,
        cancelable: true
      }));
    });
    await page.waitForTimeout(10);
    await page.locator('#show').click();
    let report = JSON.parse(await page.locator('#diagnostics').textContent());
    assert.equal(report.events.findLast(event => event.event === 'beforeinput' && event.inputType === 'insertFromPaste').prevented, false);
    assert.equal(report.events.findLast(event => event.event === 'beforeinput' && event.inputType === 'insertFromPaste').preventedAfterDispatch, true);
    assert.doesNotMatch(JSON.stringify(report), /existing/);
    console.log('PASS diagnostics capture deferred cancellation without retaining text');
    await page.goto(base + '/diagnostics/wispr-check.html?paste=direct');
    const paste = async text => page.locator('#dictation').evaluate((field, value) => {
      const clipboardData = new DataTransfer();
      clipboardData.setData('text/plain', value);
      return field.dispatchEvent(new ClipboardEvent('paste', {
        bubbles: true,
        cancelable: true,
        clipboardData
      }));
    }, text);
    await page.locator('#dictation').fill('first');
    await page.locator('#dictation').focus();
    assert.equal(await paste(' second'), false);
    assert.equal(await paste(' third'), false);
    assert.equal(await page.locator('#dictation').inputValue(), 'first second third');
    await page.locator('#dictation').evaluate(field => field.setSelectionRange(0, 5));
    await paste('changed');
    assert.equal(await page.locator('#dictation').inputValue(), 'changed second third');
    await page.locator('#dictation').evaluate(field => field.setSelectionRange(0, field.value.length));
    await paste('A\nB');
    assert.equal(await page.locator('#dictation').inputValue(), 'A\nB');
    assert.equal(await paste(''), true);
    assert.equal(await page.locator('#dictation').inputValue(), 'A\nB');
    await page.waitForTimeout(10);
    await page.locator('#show').click();
    report = JSON.parse(await page.locator('#diagnostics').textContent());
    assert.equal(report.directPaste, true);
    const pastes = report.events.filter(event => event.event === 'paste');
    assert.deepEqual(pastes.map(event => event.plainTextLength), [7, 6, 7, 3, 0]);
    assert.deepEqual(pastes.at(-1).clipboardTypes, ['text/plain']);
    assert.doesNotMatch(JSON.stringify(report), /first second|changed second/);
    assert.equal(requests.some(value => value.startsWith('/api/')), false);
    console.log('PASS direct paste handles repeats, selection, multiline and empty input without duplication or API calls');
    await page.goto(base + '/diagnostics/wispr-check.html?terminal=1');
    await page.locator('#terminal .xterm').waitFor();
    await page.locator('#dictation').fill('private dictation sentinel');
    await page.locator('#dictation').evaluate(field => field.dispatchEvent(new InputEvent('beforeinput', {
      bubbles: true,
      inputType: 'insertText',
      data: 'secret input payload'
    })));
    await page.locator('#show').click();
    report = JSON.parse(await page.locator('#diagnostics').textContent());
    assert.equal(report.terminalMode, true);
    assert.ok(report.events.some(event => event.inputType === 'insertText'));
    assert.doesNotMatch(JSON.stringify(report), /private dictation sentinel|secret input payload/);
    assert.equal(requests.some(value => value.startsWith('/api/')), false);
    console.log('PASS mounted xterm dictation diagnostics retain metadata only and send no API or agent input');
  }
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
