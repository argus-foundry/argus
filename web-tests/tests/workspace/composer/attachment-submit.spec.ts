import { pathToFileURL } from 'node:url';
import { test } from '@playwright/test';
import { resetServer } from '../../_helpers';
import assert from 'node:assert/strict';
import { readFile, writeFile, mkdir } from 'node:fs/promises';
import path from 'node:path';
let browser, frontend, daemon, task, directory, png;
const cleanup = [];
async function api(resource, options = {}) {
  const response = await fetch(daemon + resource, {
    ...options,
    headers: {
      Authorization: 'Bearer test-token',
      ...options.headers
    }
  });
  assert.equal(response.ok, true, `Isolated API ${resource}: ${response.status}`);
  return response;
}
async function fixture(mode = 'normal', draft = 'Please inspect this image', name = 'delayed-image.png') {
  const log = path.join(directory, `submit-${mode}.jsonl`);
  await writeFile(log, '');
  const events = async () => {
    const data = await readFile(log, 'utf8');
    return data.trim().split('\n').filter(Boolean).map(line => JSON.parse(line));
  };
  const until = async (event, count = 1) => {
    const end = Date.now() + 6000;
    while (Date.now() < end) {
      const rows = await events();
      if (rows.filter(row => row.event === event).length >= count) return rows;
      await new Promise(resolve => setTimeout(resolve, 20));
    }
    throw new Error(`Fake agent never reported ${event}`);
  };
  await api(`/api/tasks/${task.id}/input`, {
    method: 'POST',
    body: `python3 -u ./.context/submit-agent.py ${mode} ./.context/submit-${mode}.jsonl\r`
  });
  await until('started');
  const page = await browser.newPage();
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  cleanup.push(async () => {
    await page.close();
    await api(`/api/tasks/${task.id}/input`, {
      method: 'POST',
      body: '\x04'
    });
    await until('stopped');
    await writeFile(new URL(`../../../../.context/attachment-submit-${mode}${draft ? '' : '-image-only'}.json`, pathToFileURL(__filename).href), JSON.stringify(await events(), null, 2));
    assert.deepEqual(errors, []);
  });
  await page.goto(frontend);
  await page.locator('#token').fill('test-token');
  await page.locator('#connect-button').click();
  await page.locator('.task-row').filter({
    hasText: 'echo-bash'
  }).click();
  await page.waitForFunction(() => document.querySelector('#terminal-state').textContent === 'Live terminal');
  await page.locator('#attach-files').click({
    trial: true
  });
  const picker = page.waitForEvent('filechooser');
  await page.locator('#attach-files').click();
  await (await picker).setFiles({
    name,
    mimeType: 'image/png',
    buffer: png
  });
  await page.locator('.attachment-chip[data-state="ready"]').waitFor();
  await page.locator('#draft').fill(draft);
  return {
    page,
    events,
    until
  };
}
test.beforeAll(async ({
  browser: workerBrowser
}) => {
  browser = workerBrowser;
  await resetServer();
  await mkdir(path.resolve(__dirname, '../../../../.context'), {
    recursive: true
  });
  frontend = process.env.WORKSPACE_TEST_URL;
  daemon = process.env.WORKSPACE_TEST_API;
  assert.equal(new URL(daemon).hostname, '127.0.0.1');
  assert.notEqual(new URL(daemon).port, '7743');
  task = (await (await api('/api/tasks?archived=all')).json()).tasks.find(task => task.name === 'echo-bash');
  assert.ok(task?.backend === 'bash-test' && task.worktree_path.includes('/argus-test-home-'), 'Refusing writes outside isolated harness');
  directory = path.join(task.worktree_path, '.context');
  await mkdir(directory, {
    recursive: true
  });
  await writeFile(path.join(directory, 'submit-agent.py'), await readFile(new URL("./_attachment-agent.py", pathToFileURL(__filename).href)));
  png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl6ZQAAAABJRU5ErkJggg==', 'base64');
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
test('delayed attachment ingestion receives one real submit after paste settles', async () => {
  const {
    page,
    events,
    until
  } = await fixture();
  await page.locator('#send').click();
  await until('image_ready');
  await page.waitForFunction(() => document.querySelectorAll('.attachment-chip').length === 0);
  const received = await events();
  assert.equal(received.filter(row => row.event === 'submitted').length, 1, JSON.stringify(received));
  assert.equal(received.filter(row => row.event === 'early_cr').length, 0, 'Submit must follow actual image ingestion');
  const bytes = Buffer.concat(received.filter(row => row.event === 'bytes').map(row => Buffer.from(row.value, 'base64'))).toString();
  assert.match(bytes, /^\x1b\[200~Please inspect this image \.\/\.context\/[^\r]+\x1b\[201~\r$/);
  await page.locator('#draft').fill('Plain message remains unchanged');
  await page.locator('#send').click();
  await page.waitForFunction(() => document.querySelector('#draft').value === '');
  const after = await until('submitted', 2);
  assert.equal(after.filter(row => row.event === 'submitted').length, 2);
  const tail = Buffer.concat(after.filter(row => row.event === 'bytes').map(row => Buffer.from(row.value, 'base64'))).toString().slice(bytes.length);
  assert.equal(tail, '\x1b[200~Plain message remains unchanged\x1b[201~\r');
});
test('accepted Enter without terminal acknowledgment clears and never retries', async () => {
  const {
    page,
    events
  } = await fixture('swallow-all');
  await page.locator('#send').click();
  await page.waitForFunction(() => document.querySelectorAll('.attachment-chip').length === 0 && document.querySelector('#draft').value === '');
  await new Promise(resolve => setTimeout(resolve, 4500));
  const rows = await events();
  assert.equal(rows.filter(row => row.event === 'submitted').length, 0);
  assert.equal(rows.filter(row => row.event === 'ignored_cr').length, 1);
  assert.equal(rows.filter(row => row.event === 'paste').length, 1);
});
test('accepted submission with stale terminal echo is never submitted twice', async () => {
  const {
    page,
    events,
    until
  } = await fixture('stale-echo');
  await page.locator('#send').click();
  await page.waitForFunction(() => document.querySelectorAll('.attachment-chip').length === 0 && document.querySelector('#draft').value === '');
  await until('submitted');
  await new Promise(resolve => setTimeout(resolve, 4500));
  const rows = await events();
  assert.equal(rows.filter(row => row.event === 'submitted').length, 1);
  assert.equal(rows.filter(row => row.event === 'paste').length, 1);
});
test('disconnect during delayed image ingestion prevents any later Enter', async () => {
  const {
    page,
    events,
    until
  } = await fixture('cancel');
  await page.locator('#send').click();
  await until('paste');
  await page.locator('#disconnect').click();
  await until('image_ready');
  const rows = await events();
  assert.equal(rows.filter(row => row.event === 'submitted').length, 0);
  assert.equal(rows.filter(row => row.event === 'early_cr').length, 0);
  const bytes = Buffer.concat(rows.filter(row => row.event === 'bytes').map(row => Buffer.from(row.value, 'base64'))).toString();
  assert.equal(bytes.includes('\r'), false);
});
test('an image-only literal path submits once and clears', async () => {
  const {
    page,
    events,
    until
  } = await fixture('literal-path', '');
  await page.locator('#send').click();
  await page.waitForFunction(() => document.querySelectorAll('.attachment-chip').length === 0 && document.querySelector('#draft').value === '');
  const rows = await until('submitted');
  assert.equal(rows.filter(row => row.event === 'submitted').length, 1);
  assert.equal(rows.filter(row => row.event === 'early_cr').length, 0);
  assert.match(rows.find(row => row.event === 'submitted').text, /^\.\/\.context\//);
});
test('an image-only marker conversion also submits once and clears', async () => {
  const {
    page,
    events,
    until
  } = await fixture('image-marker', '');
  await page.locator('#send').click();
  await page.waitForFunction(() => document.querySelectorAll('.attachment-chip').length === 0 && document.querySelector('#draft').value === '');
  const rows = await until('submitted');
  assert.equal(rows.filter(row => row.event === 'submitted').length, 1);
  assert.equal(rows.filter(row => row.event === 'early_cr').length, 0);
  assert.equal(rows.find(row => row.event === 'submitted').text, '[Image #1]');
});
test('Claude mixed text and image path remains literal, submits once and clears composer', async () => {
  const {
    page,
    events,
    until
  } = await fixture('literal-path', 'Please inspect this image', 'Screenshot 2026-11-06 at 8.34.43\u202fPM.png');
  await page.locator('#send').click();
  await until('image_ready');
  await page.waitForFunction(() => document.querySelectorAll('.attachment-chip').length === 0 && document.querySelector('#draft').value === '');
  await until('submitted');
  await new Promise(resolve => setTimeout(resolve, 1700));
  const rows = await events();
  const pasted = rows.find(row => row.event === 'paste').value;
  assert.match(pasted, /Screenshot 2026-11-06 at 8\.34\.43\u202fPM(?:-\d+)?\.png$/);
  assert.equal(rows.find(row => row.event === 'submitted').text, pasted);
  assert.equal(rows.filter(row => row.event === 'bytes').map(row => Buffer.from(row.value, 'base64').toString()).join(''), `\x1b[200~${pasted}\x1b[201~\r`);
  assert.deepEqual({
    draft: await page.locator('#draft').inputValue(),
    chips: await page.locator('.attachment-chip').count(),
    submits: rows.filter(row => row.event === 'submitted').length
  }, {
    draft: '',
    chips: 0,
    submits: 1
  }, JSON.stringify(rows));
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
