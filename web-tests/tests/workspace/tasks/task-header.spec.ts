import { test } from '@playwright/test';
import { resetServer } from '../../_helpers';
import assert from 'node:assert/strict';
let browser, name, project, branch;
const cleanup = [];
async function fixture(width, {initialPR = true, metadataFailures = 0} = {}) {
  const expectMetadataFailure = metadataFailures > 0;
  const page = await browser.newPage({
    viewport: {
      width,
      height: 900
    }
  });
  cleanup.push(() => page.close());
  const errors = [],
    resizes = [];
  page.on('pageerror', e => errors.push(e.message));
  cleanup.push(() => assert.deepEqual(errors, []));
  const task = {
    id: 'header-a',
    name,
    project,
    branch,
    status: 'in_progress',
    live: true,
    idle: false,
    pr_state: initialPR ? 'awaiting-review' : ''
  };
  let prURL = initialPR ? 'https://github.com/example/repo/pull/7' : '';
  const reads = [];
  let size = {
    cols: 150,
    rows: 80
  };
  await page.addInitScript(() => {
    window.copied = [];
    Object.defineProperty(navigator, 'clipboard', {
      value: {
        writeText: async text => window.copied.push(text)
      }
    });
    window.sources = [];
    window.EventSource = class extends EventTarget {
      constructor(url) {
        super();
        this.url = url;
        window.sources.push(this);
        queueMicrotask(() => this.onopen?.({}));
      }
      close() {}
    };
  });
  await page.route('**/api/**', route => {
    const path = new URL(route.request().url()).pathname;
    if (path === '/api/tasks') return route.fulfill({
      json: {
        tasks: [task, {
          id: 'header-b',
          name: 'Second task',
          project,
          status: 'complete',
          live: false
        }]
      }
    });
    if (path === '/api/tasks/header-a') return route.fulfill({
      json: task
    });
    if (path === '/api/projects/full') return route.fulfill({
      json: {
        projects: []
      }
    });
    if (path === '/api/hera') return route.fulfill({
      json: {
        orchestrators: []
      }
    });
    if (path.startsWith('/api/tasks/header-a/') && (path.endsWith('/links') || path.endsWith('/meta') && new URL(route.request().url()).searchParams.get('namespace') === 'pr')) reads.push(path);
    if (path.endsWith('/meta') && new URL(route.request().url()).searchParams.get('namespace') === 'pr' && metadataFailures > 0) {
      metadataFailures--;
      return route.fulfill({status: 503, json: {}});
    }
    if (path.endsWith('/meta')) return route.fulfill({
      json: {
        entries: prURL ? [{
          namespace: 'pr',
          key: 'url',
          value: prURL
        }] : []
      }
    });
    if (path.endsWith('/links')) return route.fulfill({
      json: {
        links: []
      }
    });
    if (path.endsWith('/size')) return route.fulfill({
      json: size
    });
    if (path.endsWith('/output')) return route.fulfill({
      body: 'HEADER_OUTPUT\r\n',
      headers: {
        'X-Output-Total': '15'
      }
    });
    if (path.endsWith('/resize')) {
      size = route.request().postDataJSON();
      resizes.push(size);
      return route.fulfill({
        json: {
          rerendered: false
        }
      });
    }
    return route.fulfill({
      status: 404,
      json: {}
    });
  });
  await page.goto(process.env.WORKSPACE_TEST_URL || 'http://127.0.0.1:18750');
  await page.locator('#token').fill('fixture');
  await page.locator('#connect-button').click();
  if (width < 1100) await page.locator('#toggle-nav').click();
  await page.locator('.task-row').filter({
    hasText: name
  }).click();
  if (initialPR && !expectMetadataFailure) await page.locator('.task-header-links a').waitFor();
  else await page.waitForFunction(() => document.querySelector('.task-header-links span'));
  await page.locator('#fit-session:not([disabled])').waitFor();
  return {
    page,
    resizes,
    reads,
    publishPR(url) { prURL = url; task.pr_state = 'awaiting-review'; }
  };
}
test.beforeAll(async ({
  browser: workerBrowser
}) => {
  browser = workerBrowser;
  await resetServer();
  name = 'A task with a deliberately long descriptive name '.repeat(5);
  project = 'Project with a long name';
  branch = 'lzl/very-long-branch-name-'.repeat(5);
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
for (const width of [1280, 2544, 390]) test(`compact task header and long-label containment at ${width}px`, async () => {
  const {
    page
  } = await fixture(width);
  const geometry = await page.evaluate(() => {
    const header = document.querySelector('.task-header'),
      title = document.querySelector('#task-title'),
      panel = document.querySelector('#agent-panel');
    return {
      height: header.getBoundingClientRect().height,
      bottom: header.getBoundingClientRect().bottom,
      terminal: panel.getBoundingClientRect().top,
      scroll: header.scrollWidth,
      client: header.clientWidth,
      document: document.scrollingElement.scrollWidth,
      width: innerWidth,
      weight: getComputedStyle(title).fontWeight,
      ellipsis: getComputedStyle(title).textOverflow
    };
  });
  assert.ok(geometry.height <= (width === 390 ? 88 : 48), JSON.stringify(geometry));
  assert.equal(geometry.bottom, geometry.terminal, 'Terminal starts directly below the single header');
  assert.ok(geometry.scroll <= geometry.client && geometry.document <= geometry.width, JSON.stringify(geometry));
  assert.equal(geometry.weight, '400');
  assert.equal(geometry.ellipsis, 'ellipsis');
  assert.equal(await page.locator('#breadcrumb,#task-subtitle').count(), 0);
  assert.equal(await page.locator('.terminal-toolbar').count(), 0);
  assert.equal(await page.locator('#terminal-feedback').evaluate(el => getComputedStyle(el).clipPath), 'inset(50%)');
  assert.equal(await page.locator('#task-title').getAttribute('title'), `${name}\n${project}`);
  for (const id of ['tab-agent', 'tab-review', 'split-toggle', 'fit-session']) assert.equal(await page.locator(`#${id}`).evaluate(el => !!el.closest('.task-header')), true);
  await page.screenshot({
    path: `.context/task-header-${width}.png`
  });
});
test('compact header preserves keyboard actions, labeled icons, branch copy, PR and manual Fit', async () => {
  const {
    page,
    resizes
  } = await fixture(1280);
  await page.waitForFunction(() => document.querySelector('#fit-session').dataset.fitNeeded === 'true');
  assert.match(await page.locator('#fit-session').getAttribute('data-tooltip'), /^Fit to pane \(\d+ × \d+; current 150 × 80\)$/);
  const copy = page.locator('.task-header-links button');
  await copy.focus();
  await page.keyboard.press('Enter');
  assert.deepEqual(await page.evaluate(() => window.copied), [branch]);
  await page.keyboard.press('Tab');
  assert.equal(await page.locator('.task-header-links a').evaluate(el => el === document.activeElement), true);
  assert.equal(await page.locator('.task-header-links a').getAttribute('href'), 'https://github.com/example/repo/pull/7');
  await page.keyboard.press('Tab');
  assert.equal(await page.locator('#tab-agent').evaluate(el => el === document.activeElement), true);
  await page.keyboard.press('ArrowRight');
  assert.equal(await page.locator('#tab-review').evaluate(el => el === document.activeElement), true);
  await page.keyboard.press('ArrowLeft');
  await page.keyboard.press('Tab');
  assert.equal(await page.locator('#split-toggle').evaluate(el => el === document.activeElement), true);
  for (const [id, label] of [['split-toggle', 'Split view'], ['fit-session', 'Fit session']]) {
    assert.equal(await page.locator(`#${id}`).getAttribute('aria-label'), label);
    assert.ok(await page.locator(`#${id}`).getAttribute('data-tooltip'));
    assert.equal(await page.locator(`#${id}`).getAttribute('title'), null);
    assert.equal(await page.locator(`#${id}`).innerText(), '');
  }
  await page.keyboard.press('Enter');
  assert.equal(await page.locator('#split-toggle').getAttribute('aria-pressed'), 'true');
  await page.keyboard.press('Alt+Shift+D');
  assert.equal(await page.locator('#split-toggle').getAttribute('aria-pressed'), 'false');
  await page.locator('#split-toggle').focus();
  await page.keyboard.press('Tab');
  assert.equal(await page.locator('#fit-session').evaluate(el => el === document.activeElement), true);
  assert.equal(resizes.length, 0, 'Busy fixture must defer automatic fitting');
  await page.keyboard.press('Enter');
  await page.waitForFunction(() => document.querySelector('#fit-session').dataset.fitNeeded === 'false');
  assert.equal(resizes.length, 1);
  await page.locator('#tab-review').click();
  assert.equal(await page.locator('.task-header-links a').isVisible(), true);
  await page.locator('#tab-agent').click();
  assert.equal(await page.locator('#fit-session').isVisible(), true);
});
test('terminal failure reveals reconnect feedback and recovery removes its visual bar', async () => {
  const {
    page
  } = await fixture(1280);
  await page.evaluate(() => window.sources.findLast(source => source.url.includes('/stream?')).onmessage({
    data: '%%%'
  }));
  await page.getByRole('button', {
    name: 'Reconnect',
    exact: true
  }).waitFor();
  assert.match(await page.locator('#terminal-feedback').textContent(), /Invalid terminal stream/);
  assert.equal(await page.locator('#terminal-feedback').evaluate(el => getComputedStyle(el).clipPath), 'none');
  assert.equal(await page.locator('#fit-session').isDisabled(), true);
  await page.getByRole('button', {
    name: 'Reconnect',
    exact: true
  }).click();
  await page.waitForFunction(() => document.querySelector('#terminal-state').textContent === 'Live terminal');
  assert.equal(await page.locator('#terminal-feedback').evaluate(el => getComputedStyle(el).clipPath), 'inset(50%)');
  assert.equal(await page.locator('#reconnect-terminal').isVisible(), false);
  await page.locator('#disconnect').click();
  assert.equal(await page.locator('.task-header-links').textContent(), '');
  assert.equal(await page.locator('#split-toggle').isDisabled(), true);
  assert.equal(await page.locator('#fit-session').isDisabled(), true);
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});

test('selected task gains its PR link after a task refresh without another links read', async () => {
  const {page, reads, publishPR} = await fixture(1280, {initialPR: false});
  await page.waitForFunction(() => document.querySelector('.task-header-links span')?.childNodes.length === 0);
  assert.equal(await page.locator('.task-header-links a').count(), 0);
  const url = 'https://github.com/example/repo/pull/8';
  publishPR(url);
  await page.evaluate(() => document.dispatchEvent(new Event('visibilitychange')));
  await page.locator('.task-header-links a').waitFor();
  assert.equal(await page.locator('.task-header-links a').getAttribute('href'), url);
  assert.equal(reads.filter(path => path.endsWith('/links')).length, 1);
  assert.equal(reads.filter(path => path.endsWith('/meta')).length, 2);
});

for (const recovery of ['task refresh', 'Retry connection']) test(`selected task recovers PR metadata through ${recovery} with unchanged PR state`, async () => {
  const {page, reads} = await fixture(1280, {metadataFailures: 1});
  await page.locator('.task-header-links [role=alert]').waitFor();
  assert.equal(await page.locator('.task-header-links a').count(), 0);
  if (recovery === 'task refresh') await page.evaluate(() => document.dispatchEvent(new Event('visibilitychange')));
  else await page.getByRole('button', {name: 'Retry connection', exact: true}).click();
  await page.locator('.task-header-links a').waitFor();
  assert.equal(await page.locator('.task-header-links a').getAttribute('href'), 'https://github.com/example/repo/pull/7');
  assert.equal(await page.locator('.task-header-links [role=alert]').count(), 0);
  assert.equal(reads.filter(path => path.endsWith('/links')).length, 1);
  assert.equal(reads.filter(path => path.endsWith('/meta')).length, 2);
});
