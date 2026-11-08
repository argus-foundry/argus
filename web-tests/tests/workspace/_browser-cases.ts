import assert from 'node:assert/strict';
export async function interruptedSend(page, initial, second, choose, mutations) {
  const inputPath = `/api/tasks/${initial.id}/input`;
  let release, markHeld;
  const gate = new Promise(resolve => {
    release = resolve;
  });
  const held = new Promise(resolve => {
    markHeld = resolve;
  });
  const text = "printf 'this send should be canceled'";
  const handler = async route => {
    if (route.request().postData() !== text) return route.continue();
    markHeld();
    await gate;
    try {
      await route.continue();
    } catch {/* A switched task aborts this request. */}
  };
  await page.route('**' + inputPath, handler);
  const baseline = mutations.length;
  await page.locator('#draft').fill(text);
  await page.locator('#send').click();
  await held;
  await choose(second.name);
  release();
  await choose(initial.name);
  await page.waitForFunction(() => document.querySelector('#composer-status').textContent.includes('not confirmed'));
  assert.equal(await page.locator('#draft').inputValue(), text);
  assert.equal(mutations.slice(baseline).some(value => value.path === `/api/tasks/${second.id}/input`), false);
  assert.equal(mutations.slice(baseline).some(value => value.path === inputPath && value.body === '\r'), false);
  await page.unroute('**' + inputPath, handler);
  await page.waitForFunction(() => document.querySelector('#terminal-state').textContent === 'Live terminal');
  // An aborted request can have reached the existing backend. Clear its unsubmitted line.
  const cleared = page.waitForResponse(response => new URL(response.url()).pathname === inputPath && response.request().postData() === '\x15');
  await page.locator('.xterm-helper-textarea').focus();
  await page.keyboard.press('Control+u');
  await cleared;
}
export async function browserHistory(page, initial) {
  let state = new URL(page.url());
  assert.equal(state.searchParams.get('task'), initial.id);
  assert.equal(state.searchParams.get('view'), 'review');
  assert.equal(state.searchParams.get('file'), 'workspace-check.md');
  assert.equal(state.searchParams.get('mode'), 'split');
  await page.goBack();
  await page.locator('.diff-table.unified').waitFor();
  await page.goForward();
  await page.locator('.diff-table.split').waitFor();
  state = new URL(page.url());
  assert.equal(state.searchParams.has('token'), false);
}
