import { test, expect } from '@playwright/test';
import { resetServer } from '../../_helpers';
import assert from 'node:assert/strict';
import { readFile, mkdir } from 'node:fs/promises';
import path from 'node:path';
let browser, root, frontend, daemon, headers, response, taskA, created, taskB, png, screenshot, note;
const cleanup = [];
async function fixture() {
  const page = await browser.newPage();
  cleanup.push(() => page.close());
  const errors = [],
    uploads = [];
  page.on('pageerror', error => errors.push(error.message));
  page.on('popup', () => errors.push('File drop opened a popup'));
  page.on('request', request => {
    if (new URL(request.url()).pathname.endsWith('/upload')) uploads.push(request);
  });
  cleanup.push(() => assert.deepEqual(errors, []));
  await page.route('**/attachment-fixture', route => route.fulfill({
    contentType: 'text/html',
    body: `<!doctype html><link rel="stylesheet" href="/workspace/styles.css">
    <input id="search"><div id="agent-panel" style="height:100px">Agent panel</div><div class="composer-wrap"><form id="composer"><textarea id="draft"></textarea><button id="send">Send</button></form><span id="composer-label"></span><span id="composer-status"></span></div>
    <script type="module">import { TaskComposer } from '/composer/composer.js'; import { Api } from '/core/api.js';
      const api = new Api(); api.setToken('test-token'); const sent=[];
      const composer = new TaskComposer(async (text,task) => { if(window.fixture.fail) throw new Error('Send failed'); sent.push({text,task:task.id}); },api);
      window.fixture={composer,sent,taskA:${JSON.stringify(taskA)},taskB:${JSON.stringify(taskB)},select(task){composer.select(task);composer.setInteractive(!!task);composer.focus();}};
      window.fixture.select(window.fixture.taskA);
    </script>`
  }));
  await page.goto(frontend + '/attachment-fixture');
  await page.waitForFunction(() => window.fixture);
  return {
    page,
    uploads
  };
}
async function transfer(page, type, target, files, text = '') {
  return page.locator(target).evaluate((element, {
    type,
    files,
    text
  }) => {
    const data = new DataTransfer();
    for (const file of files) data.items.add(new File([new Uint8Array(file.bytes)], file.name, {
      type: file.type
    }));
    if (text) data.setData('text/plain', text);
    const event = type === 'paste' ? new ClipboardEvent(type, {
      clipboardData: data,
      bubbles: true,
      cancelable: true
    }) : new DragEvent(type, {
      dataTransfer: data,
      bubbles: true,
      cancelable: true
    });
    return element.dispatchEvent(event);
  }, {
    type,
    files,
    text
  });
}
test.beforeAll(async ({
  browser: workerBrowser
}) => {
  browser = workerBrowser;
  await resetServer();
  root = path.resolve(__dirname, "../../../..");
  frontend = process.env.WORKSPACE_TEST_URL || 'http://127.0.0.1:17750';
  daemon = process.env.WORKSPACE_TEST_API || 'http://127.0.0.1:17744';
  headers = {
    Authorization: 'Bearer test-token',
    'Content-Type': 'application/json'
  };
  response = await fetch(daemon + '/api/tasks?archived=all', {
    headers
  });
  taskA = (await response.json()).tasks.find(task => task.name === 'echo-bash');
  assert.ok(taskA?.backend === 'bash-test' && taskA.worktree_path.includes('/argus-test-home-'), 'Refusing attachment writes outside the isolated harness');
  created = await fetch(daemon + '/api/tasks', {
    method: 'POST',
    headers,
    body: JSON.stringify({
      name: `attachment-test-${Date.now()}`,
      project: 'test-proj',
      backend: 'bash-test'
    })
  });
  assert.equal(created.ok, true);
  taskB = await created.json();
  png = [...Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl6ZQAAAABJRU5ErkJggg==', 'base64')];
  screenshot = name => ({
    name,
    type: 'image/png',
    bytes: png
  });
  note = name => ({
    name,
    type: 'text/plain',
    bytes: [...Buffer.from('isolated attachment contents')]
  });
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
test('file drops prevent navigation, upload files and show local image previews', async () => {
  const {
    page,
    uploads
  } = await fixture();
  const files = [screenshot(`screen-${Date.now()}.png`), note(`note-${Date.now()}.txt`)];
  assert.equal(await transfer(page, 'dragover', '#draft', files), false);
  assert.equal(await page.locator('.file-drag-over').count(), 1);
  assert.equal(await transfer(page, 'drop', '#agent-panel', files), false);
  await page.waitForFunction(() => document.querySelectorAll('.attachment-chip[data-state="ready"]').length === 2);
  assert.equal(uploads.length, 2);
  assert.ok(uploads.every(request => request.url().endsWith(`/api/tasks/${taskA.id}/upload`) && /multipart\/form-data; boundary=/.test(request.headers()['content-type'])));
  const paths = await page.evaluate(() => window.fixture.composer.attachments.store.ready(window.fixture.taskA.id).map(item => item.path));
  for (let index = 0; index < paths.length; index++) assert.deepEqual([...(await readFile(path.join(taskA.worktree_path, paths[index])))], files[index].bytes);
  assert.equal(await page.locator('.attachment-chip img').evaluate(image => image.src.startsWith('blob:') && image.complete && image.naturalWidth > 0), true);
  assert.equal(await page.evaluate(() => window.fixture.sent.length), 0);
  assert.equal(page.url(), frontend + '/attachment-fixture');
  assert.equal(await transfer(page, 'drop', '#search', [note('miss.txt')]), false);
  assert.equal(uploads.length, 2);
  assert.equal(await page.evaluate(() => {
    const data = new DataTransfer();
    data.items.add(new File(['window miss'], 'miss.txt'));
    return window.dispatchEvent(new DragEvent('drop', {
      dataTransfer: data,
      cancelable: true
    }));
  }), false);
  assert.equal(await transfer(page, 'drop', '#draft', [], 'plain text'), true);
});
test('image paste and repeated plaintext paste coexist without automatic send', async () => {
  const {
    page,
    uploads
  } = await fixture();
  assert.equal(await transfer(page, 'paste', '#draft', [screenshot(`paste-${Date.now()}.png`)]), false);
  await page.waitForFunction(() => document.querySelector('.attachment-chip')?.dataset.state === 'ready');
  await page.locator('#draft').fill('alpha omega');
  await page.locator('#draft').evaluate(field => field.setSelectionRange(6, 11));
  await transfer(page, 'paste', '#draft', [], 'bravo');
  await transfer(page, 'paste', '#draft', [], '\nnext line\n');
  assert.equal(await page.locator('#draft').inputValue(), 'alpha bravo\nnext line\n');
  assert.equal(await page.evaluate(() => window.fixture.sent.length), 0);
  await page.locator('#search').focus();
  assert.equal(await transfer(page, 'paste', '#search', [screenshot('protected.png')]), true);
  assert.equal(uploads.length, 1);
});
test('switching tasks during upload preserves chips and removal excludes paths from send', async () => {
  const {
    page,
    uploads
  } = await fixture();
  let release, held;
  const gate = new Promise(resolve => {
    release = resolve;
  });
  const reached = new Promise(resolve => {
    held = resolve;
  });
  await page.route('**/api/tasks/*/upload', async route => {
    held();
    await gate;
    await route.continue();
  });
  await transfer(page, 'drop', '#draft', [note(`switch-${Date.now()}.txt`)]);
  await reached;
  assert.equal(await page.locator('.attachment-chip').getAttribute('data-state'), 'uploading');
  assert.equal(await page.locator('#send').isDisabled(), true);
  await page.evaluate(() => window.fixture.select(window.fixture.taskB));
  await page.locator('#draft').fill('B stays here');
  release();
  await page.waitForFunction(() => window.fixture.composer.attachments.store.ready(window.fixture.taskA.id).length === 1);
  assert.equal(await page.locator('.attachment-chip').count(), 0);
  assert.equal(await page.locator('#draft').inputValue(), 'B stays here');
  assert.ok(uploads[0].url().includes(`/tasks/${taskA.id}/upload`));
  await page.evaluate(() => window.fixture.select(window.fixture.taskA));
  await page.locator('.attachment-chip button[aria-label^="Remove"]').click();
  await page.locator('#draft').fill('Only my text');
  await page.locator('#send').click();
  assert.deepEqual(await page.evaluate(() => window.fixture.sent), [{
    text: 'Only my text',
    task: taskA.id
  }]);
});
test('upload errors require explicit retry and send failures preserve ready attachments', async () => {
  const {
    page,
    uploads
  } = await fixture();
  let fail = true;
  await page.route('**/api/tasks/*/upload', route => fail ? route.fulfill({
    status: 500,
    json: {}
  }) : route.continue());
  await transfer(page, 'drop', '#draft', [note(`retry-${Date.now()}.txt`)]);
  await page.waitForFunction(() => document.querySelector('.attachment-chip')?.dataset.state === 'error');
  assert.equal(await page.locator('#send').isDisabled(), true);
  fail = false;
  await page.getByRole('button', {
    name: 'Retry upload',
    exact: true
  }).click();
  await page.waitForFunction(() => document.querySelector('.attachment-chip')?.dataset.state === 'ready');
  assert.equal(uploads.length, 2);
  const uploaded = await page.evaluate(() => window.fixture.composer.attachments.store.ready(window.fixture.taskA.id)[0].path);
  await page.evaluate(() => {
    window.fixture.fail = true;
  });
  await page.locator('#send').click();
  await page.waitForFunction(() => document.querySelector('#composer-status').textContent.includes('Send failed'));
  assert.equal(await page.locator('.attachment-chip').count(), 1);
  await page.evaluate(() => {
    window.fixture.fail = false;
  });
  await page.locator('#send').click();
  await page.waitForFunction(() => document.querySelectorAll('.attachment-chip').length === 0);
  assert.deepEqual(await page.evaluate(() => window.fixture.sent), [{
    text: uploaded,
    task: taskA.id
  }]);
});
test('delayed upload failure announces the filename without moving draft focus', async () => {
  const { page } = await fixture();
  const alert = page.getByRole('alert');
  await page.evaluate(() => { window.fixture.errorRegion = document.querySelector('[role="alert"]'); });
  let release;
  const gate = new Promise<void>(resolve => { release = resolve; });
  await page.route('**/api/tasks/*/upload', async route => {
    await gate;
    await route.fulfill({ status: 500, json: { error: 'Attachment service unavailable' } });
  });
  await transfer(page, 'drop', '#draft', [note('failed-report.txt')]);
  await page.locator('#draft').fill('Keep drafting');
  await expect(page.locator('#draft')).toBeFocused();
  assert.equal(await page.evaluate(() => window.fixture.errorRegion?.textContent || ''), '');
  release();
  await expect(alert).toContainText('failed-report.txt');
  await expect(alert).toContainText('Upload failed.');
  await expect(alert).toContainText('Argus request failed (500).');
  assert.equal(await alert.evaluate(element => element === window.fixture.errorRegion), true);
  await expect(page.locator('#draft')).toBeFocused();
  await expect(page.locator('#send')).toBeDisabled();
});

test('upload settlement retains the focused Remove control', async () => {
  const { page } = await fixture();
  let release;
  const gate = new Promise<void>(resolve => { release = resolve; });
  await page.route('**/api/tasks/*/upload', async route => {
    await gate;
    await route.fulfill({ status: 500, json: { error: 'Unavailable' } });
  });
  await transfer(page, 'drop', '#draft', [note('settlement.txt')]);
  const remove = page.getByRole('button', { name: 'Remove settlement.txt', exact: true });
  await remove.focus();
  release();
  await expect(page.locator('.attachment-chip')).toHaveAttribute('data-state', 'error');
  await expect(remove).toBeFocused();
});

test('keyboard retry moves focus to Remove and retains it after upload', async () => {
  const { page } = await fixture();
  let retry = false, release;
  const gate = new Promise<void>(resolve => { release = resolve; });
  await page.route('**/api/tasks/*/upload', async route => {
    if (retry) await gate;
    await route.fulfill(retry
      ? { json: { paths: ['./.context/retry.txt'] } }
      : { status: 500, json: { error: 'Unavailable' } });
  });
  await transfer(page, 'drop', '#draft', [note('retry.txt')]);
  const button = page.getByRole('button', { name: 'Retry upload', exact: true });
  await expect(button).toBeVisible();
  retry = true;
  await button.focus();
  await page.keyboard.press('Enter');
  const remove = page.getByRole('button', { name: 'Remove retry.txt', exact: true });
  await expect(remove).toBeFocused();
  release();
  await expect(page.locator('.attachment-chip')).toHaveAttribute('data-state', 'ready');
  await expect(remove).toBeFocused();
});

test('keyboard removal focuses a neighbour then the draft after the last attachment', async () => {
  const { page } = await fixture();
  await page.route('**/api/tasks/*/upload', route => route.fulfill({ json: { paths: ['./.context/ready.txt'] } }));
  await transfer(page, 'drop', '#draft', [note('first.txt'), note('second.txt'), note('third.txt')]);
  await expect(page.locator('.attachment-chip[data-state="ready"]')).toHaveCount(3);
  await page.getByRole('button', { name: 'Remove second.txt', exact: true }).focus();
  await page.keyboard.press('Enter');
  await expect(page.getByRole('button', { name: 'Remove third.txt', exact: true })).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(page.getByRole('button', { name: 'Remove first.txt', exact: true })).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(page.locator('#draft')).toBeFocused();
});

test('disabled targets reject files clearly while the window still blocks navigation', async () => {
  const {
    page,
    uploads
  } = await fixture();
  await page.evaluate(() => window.fixture.select(null));
  assert.equal(await transfer(page, 'drop', '#draft', [note('disabled.txt')]), false);
  assert.match(await page.locator('#attachment-status').textContent(), /Select a live task/);
  await page.evaluate(() => {
    window.fixture.select(window.fixture.taskA);
    window.fixture.composer.setInteractive(false);
  });
  assert.equal(await transfer(page, 'drop', '#agent-panel', [note('offline.txt')]), false);
  assert.match(await page.locator('#attachment-status').textContent(), /not accepting/);
  assert.equal(uploads.length, 0);
  assert.equal(page.url(), frontend + '/attachment-fixture');
});
test('real workspace terminal drop waits for explicit Send before writing agent input', async () => {
  const page = await browser.newPage({
    viewport: {
      width: 1440,
      height: 900
    }
  });
  cleanup.push(() => page.close());
  const input = [];
  page.on('request', request => {
    if (new URL(request.url()).pathname.endsWith('/input')) input.push(request.postData());
  });
  await page.goto(frontend);
  await page.locator('#token').fill('test-token');
  await page.locator('#connect-button').click();
  await page.locator('.task-row').filter({
    has: page.getByText('echo-bash', {
      exact: true
    })
  }).click();
  await page.waitForFunction(() => document.querySelector('#terminal-state').textContent === 'Live terminal');
  await transfer(page, 'drop', '.xterm-screen', [screenshot(`workspace-drop-${Date.now()}.png`)]);
  await page.waitForFunction(() => document.querySelector('.attachment-chip')?.dataset.state === 'ready');
  assert.deepEqual(input, []);
  await page.locator('#draft').fill("printf 'ATTACHMENT_SEND_PROOF\\n'; #");
  await mkdir(path.join(root, '.context/workspace'), {
    recursive: true
  });
  await page.screenshot({
    path: path.join(root, '.context/workspace/attachments.png')
  });
  await page.locator('#send').click();
  await page.waitForFunction(() => document.querySelectorAll('.attachment-chip').length === 0);
  assert.equal(input.length, 2);
  assert.match(input[0], /ATTACHMENT_SEND_PROOF.* \.\/\.context\/workspace-drop-/);
  assert.equal(input[1], '\r');
  await page.locator('#disconnect').click();
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
