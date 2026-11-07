import { test, expect } from '@playwright/test';
import { resetServer } from '../../../_helpers';
import assert from 'node:assert/strict';
let browser, first, second, entry;
const cleanup = [];
async function fixture({
  worktree = '/tmp/task space',
  binary = false,
  many = false
} = {}) {
  const page = await browser.newPage({
    viewport: {
      width: 1440,
      height: 900
    }
  });
  page.setDefaultTimeout(8000);
  cleanup.push(() => page.close());
  const errors = [],
    reads = [];
  page.on('pageerror', e => errors.push(e.message));
  cleanup.push(() => assert.deepEqual(errors, []));
  await page.addInitScript(() => {
    window.copied = [];
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: {
        writeText: async value => window.copied.push(value)
      }
    });
  });
  const task = {
    id: 'a',
    name: 'Changes fixture',
    project: 'fixture',
    status: 'complete',
    worktree_path: worktree
  };
  await page.route('**/api/**', route => {
    const url = new URL(route.request().url()),
      path = url.pathname;
    let json = {};
    if (path === '/api/tasks') json = {
      tasks: [task]
    };else if (path === '/api/projects/full' || path === '/api/projects') json = {
      projects: [{
        name: 'fixture'
      }]
    };else if (path.endsWith('/size')) json = {
      cols: 80,
      rows: 24
    };else if (path.endsWith('/raw')) json = task;else if (path.endsWith('/git/status')) json = {
      BranchFiles: many ? Array.from({
        length: 225
      }, (_, i) => `M\tsrc/file-${String(i).padStart(3, '0')}.js`).join('\n') : `M\t${first}\nA\t${second}`,
      BranchDiff: ' 2 files changed, 3 insertions(+), 1 deletion(-)'
    };else if (path.endsWith('/git/diff')) {
      const file = url.searchParams.get('path');
      reads.push(file);
      json = {
        Diff: binary ? 'Binary files a/file and b/file differ' : file === first ? 'diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -1 +1,2 @@\n-old\n+new\n+extra' : 'diff --git a/f b/f\n--- /dev/null\n+++ b/f\n@@ -0,0 +1 @@\n+new'
      };
    }
    if (path.endsWith('/output')) return route.fulfill({
      body: '',
      headers: {
        'X-Output-Total': '0'
      }
    });
    return route.fulfill({
      json
    });
  });
  await page.goto(process.env.WORKSPACE_TEST_URL);
  await page.locator('#token').fill('fixture');
  await page.locator('#connect-button').click();
  await page.locator('.file-row').first().waitFor();
  return {
    page,
    reads
  };
}
test.beforeAll(async ({
  browser: workerBrowser
}) => {
  browser = workerBrowser;
  await resetServer();
  first = 'src/a/very/long/directory/prefix/component #?.js';
  second = 'test/helper.js';
  entry = (page, path = first) => page.locator('.file-entry').filter({
    has: page.locator(`.file-row[data-file-path=${JSON.stringify(path)}]`)
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
test('compact Changes rows retain intact filenames, exact counts and a small header toggle', async () => {
  const {
    page
  } = await fixture();
  assert.equal(await page.getByRole('button', {
    name: 'Copy relative path',
    exact: true
  }).count(), 0);
  assert.equal(await page.getByRole('menuitem', {
    name: 'Copy absolute path',
    exact: true
  }).isVisible(), false);
  await page.waitForFunction(() => document.querySelector('.changes-totals')?.textContent === '+3 −1');
  assert.match(await page.locator('.changes-heading').textContent(), /2 files changed/);
  const file = entry(page);
  assert.equal(await file.locator('.file-name').textContent(), 'component #?.js');
  assert.equal(await file.locator('.file-prefix').textContent(), 'src/a/very/long/directory/prefix/');
  await file.locator('.file-counts').filter({
    hasText: '+2 −1'
  }).waitFor();
  assert.equal(await file.locator('.file-counts').textContent(), '+2 −1');
  assert.equal(await file.locator('.file-status').textContent(), 'M');
  const bounds = await file.locator('.file-row').evaluate(row => ({
    height: row.getBoundingClientRect().height,
    prefix: row.querySelector('.file-prefix').scrollWidth > row.querySelector('.file-prefix').clientWidth,
    name: row.querySelector('.file-name').scrollWidth === row.querySelector('.file-name').clientWidth
  }));
  assert.ok(bounds.height <= 34);
  assert.equal(bounds.prefix, true);
  assert.equal(bounds.name, true);
  assert.ok((await page.getByRole('button', {
    name: 'Show tree view',
    exact: true
  }).boundingBox()).width <= 32);
  assert.equal(await page.getByRole('button', {
    name: 'Flat',
    exact: true
  }).count(), 0);
  assert.equal(await page.getByRole('button', {
    name: 'Tree',
    exact: true
  }).count(), 0);
  assert.doesNotMatch(await page.locator('#tool-panel').textContent(), /Discard|Finder|Xcode/);
  await page.screenshot({
    path: '.context/changes-compact.png'
  });
});
test('hover and keyboard menus use captured copy paths and encoded VS Code links without opening a diff', async () => {
  const {
      page
    } = await fixture(),
    file = entry(page),
    trigger = file.getByRole('button', {
      name: `File actions for ${first}`,
      exact: true
    });
  await page.locator('#task-title').hover();
  assert.equal(await trigger.evaluate(b => getComputedStyle(b).opacity), '0');
  await file.locator('.file-row').hover();
  assert.equal(await trigger.evaluate(b => getComputedStyle(b).opacity), '1');
  await file.locator('.file-row').focus();
  await page.keyboard.press('Tab');
  assert.equal(await trigger.evaluate(b => b === document.activeElement), true);
  await page.keyboard.press('ArrowDown');
  const menu = page.getByRole('menu');
  await page.screenshot({
    path: '.context/changes-menu.png'
  });
  assert.equal(await menu.isVisible(), true);
  assert.equal(await menu.getByRole('menuitem', {
    name: 'Copy path',
    exact: true
  }).evaluate(b => b === document.activeElement), true);
  await page.keyboard.press('Enter');
  assert.deepEqual(await page.evaluate(() => window.copied), [first]);
  await trigger.click();
  await page.keyboard.press('ArrowDown');
  await page.keyboard.press('Enter');
  assert.deepEqual(await page.evaluate(() => window.copied), [first, `/tmp/task space/${first}`]);
  await trigger.click();
  await page.keyboard.press('End');
  const editor = menu.getByRole('menuitem', {
    name: 'Open in VS Code',
    exact: true
  });
  assert.equal(await editor.evaluate(b => b === document.activeElement), true);
  assert.equal(await editor.getAttribute('href'), `vscode://file${encodeURI(`/tmp/task space/${first}`).replaceAll('#', '%23').replaceAll('?', '%3F')}`);
  await page.keyboard.press('Escape');
  assert.equal(await menu.isVisible(), false);
  assert.equal(await trigger.evaluate(b => b === document.activeElement), true);
  assert.equal(await page.locator('#tab-agent').getAttribute('aria-selected'), 'true');
  await file.locator('.file-row').click();
  await page.locator('#review-panel .diff-table').first().waitFor();
  assert.equal(await page.locator('#tab-review').getAttribute('aria-selected'), 'true');
});
test('flat and tree share compact row menus and counts without duplicate reads', async () => {
  const {
    page,
    reads
  } = await fixture();
  await page.waitForFunction(() => [...document.querySelectorAll('.file-counts')].length === 2 && [...document.querySelectorAll('.file-counts')].every(node => node.textContent.startsWith('+')));
  const before = reads.length;
  await page.getByRole('button', {
    name: 'Show tree view',
    exact: true
  }).click();
  assert.ok((await page.locator('#tool-panel summary').count()) > 0);
  assert.equal(await entry(page).locator('.file-name').textContent(), 'component #?.js');
  assert.equal(await entry(page).locator('.file-counts').textContent(), '+2 −1');
  await entry(page).getByRole('button', {
    name: `File actions for ${first}`,
    exact: true
  }).focus();
  await page.keyboard.press('Enter');
  await page.getByRole('menuitem', {
    name: 'Copy path',
    exact: true
  }).click();
  assert.deepEqual(await page.evaluate(() => window.copied), [first]);
  await page.getByRole('button', {
    name: 'Show flat view',
    exact: true
  }).click();
  assert.equal(await page.locator('#tool-panel summary').count(), 0);
  assert.equal(reads.length, before);
});
test('binary counts stay unknown and missing worktree disables absolute path actions', async () => {
  const {
    page
  } = await fixture({
    worktree: null,
    binary: true
  });
  await page.waitForFunction(() => document.querySelector('.file-counts')?.textContent === 'bin');
  assert.equal(await page.locator('.changes-totals').textContent(), '+3 −1');
  await entry(page).getByRole('button', {
    name: `File actions for ${first}`,
    exact: true
  }).click();
  assert.equal(await page.getByRole('menuitem', {
    name: 'Copy absolute path',
    exact: true
  }).isDisabled(), true);
  assert.equal(await page.getByRole('menuitem', {
    name: 'Open in VS Code',
    exact: true
  }).count(), 0);
});
test('a 225-path list loads counts only in view and keeps long filenames inside its own scroller', async () => {
  const {
    page,
    reads
  } = await fixture({
    many: true
  });
  await page.waitForFunction(() => [...document.querySelectorAll('.file-counts')].some(node => node.textContent.includes('+1')));
  assert.equal(await page.locator('.file-row').count(), 225);
  assert.ok(reads.length > 0 && reads.length < 60, `Only visible rows should load counts, got ${reads.length}`);
  assert.equal(reads.includes('src/file-224.js'), false);
  await page.locator('.file-row').last().scrollIntoViewIfNeeded();
  await page.waitForFunction(() => [...document.querySelectorAll('.file-counts')].at(-1).textContent === '+1 −0');
  assert.equal(reads.includes('src/file-224.js'), true);
  await page.locator('.file-name').last().evaluate(node => {
    node.textContent = 'exceptionally-long-'.repeat(40) + '.js';
  });
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
  assert.equal(await page.locator('.file-name').last().evaluate(node => node.scrollWidth === node.clientWidth), true);
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});

test('focused Changes row names include status and exact counts as counts settle', async () => {
  const {page}=await fixture();
  const row=entry(page).locator('.file-row');
  await expect(row).toHaveAccessibleName(`Open diff for ${first}, Modified, 2 lines added, 1 removed`);
  await row.focus();
  await page.evaluate(async path=>{
    const {paintCounts}=await import('/tasks/changes/task-files.js');
    const row=[...document.querySelectorAll('.file-row')].find(row=>row.dataset.filePath===path);
    paintCounts(row.querySelector('.file-counts'),undefined);
  },first);
  await expect(row).toHaveAccessibleName(`Open diff for ${first}, Modified, Loading line counts`);
  await page.evaluate(async path=>{
    const {paintCounts}=await import('/tasks/changes/task-files.js');
    const row=[...document.querySelectorAll('.file-row')].find(row=>row.dataset.filePath===path);
    paintCounts(row.querySelector('.file-counts'),{added:8,removed:2});
  },first);
  await expect(row).toHaveAccessibleName(`Open diff for ${first}, Modified, 8 lines added, 2 removed`);
  await expect(row).toBeFocused();
  await expect(row.locator('[tabindex]')).toHaveCount(0);
});
