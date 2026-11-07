import { test } from '@playwright/test';
import { resetServer } from '../../_helpers';
import assert from 'node:assert/strict';
let browser, diff, focused;
const cleanup = [];
async function fixture({
  endpoint,
  failureStatus = 500
} = {}) {
  const page = await browser.newPage({
    viewport: {
      width: 1280,
      height: 800
    }
  });
  cleanup.push(() => page.close());
  page.setDefaultTimeout(4000);
  const errors = [];
  page.on('pageerror', e => errors.push(e.message));
  cleanup.push(() => assert.deepEqual(errors, []));
  await page.route('**/api/**', route => {
    const path = new URL(route.request().url()).pathname;
    let json = {};
    if (path === '/api/tasks') json = {
      tasks: [{
        id: 'a',
        name: 'A11y fixture',
        project: 'demo',
        status: 'complete'
      }]
    };else if (path === '/api/projects/full') json = {
      projects: [{
        name: 'demo',
        path: '/tmp/demo'
      }]
    };else if (path === '/api/settings') json = {
      sandbox: {
        available: true,
        enabled: true
      }
    };else if (path.endsWith('/git/status')) json = {
      BranchFiles: 'M\tfile.js\n??\tdirectory/'
    };else if (path.endsWith('/git/diff')) json = {
      Diff: diff
    };else if (path.endsWith('/artifacts')) json = {
      artifacts: [{
        filename: 'note.txt',
        name: 'Note',
        size: 12,
        type: 'text'
      }]
    };
    return route.fulfill({
      json
    });
  });
  if (endpoint) await page.route('**/api/' + endpoint, route => route.fulfill({
    status: failureStatus,
    json: {}
  }));
  await page.goto(process.env.WORKSPACE_TEST_URL);
  await page.locator('#token').fill('fixture');
  await page.locator('#connect-button').click();
  await page.locator('.file-row').first().waitFor();
  return page;
}
async function announced(error) {
  await error.waitFor();
  assert.equal(await error.evaluate(e => !!e.closest('[role="alert"],[role="status"],[aria-live="polite"],[aria-live="assertive"]')), true, 'async error must be announced');
}
test.beforeAll(async ({
  browser: workerBrowser
}) => {
  browser = workerBrowser;
  await resetServer();
  diff = 'diff --git a/file.js b/file.js\n--- a/file.js\n+++ b/file.js\n@@ -1 +1 @@\n-old\n+new';
  focused = locator => locator.evaluate(e => e === document.activeElement);
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
for (const [section, endpoint, text] of [['general', 'settings', 'General settings unavailable'], ['add-repository', 'projects/full', 'Projects unavailable'], ['sandbox', 'settings', 'Sandbox unavailable'], ['sandbox', 'sandbox/proposals?status=pending', 'Proposals unavailable'], ['agents', 'backends', 'Backends unavailable'], ['agents', 'accounts', 'Accounts unavailable'], ['schedules', 'schedules', 'Schedules unavailable'], ['notifications', 'tokens', 'Tokens unavailable'], ['notifications', 'push/subscriptions', 'Device list unavailable']]) test(`async ${text} is announced without stealing focus`, async () => {
  const page = await fixture({
    endpoint
  });
  await page.locator('#workspace-settings').click();
  if (section !== 'general') await page.locator(`.settings-nav-entry[data-section="${section}"]`).click();
  const owner = page.locator(section === 'general' ? '.settings-back' : `.settings-nav-entry[data-section="${section}"]`);
  await announced(page.locator('.error-text').filter({
    hasText: text
  }).first());
  assert.equal(await focused(owner), true);
});
for (const [tool, endpoint, text] of [['changes', 'git/status', 'Changes unavailable'], ['artifacts', 'artifacts', 'Artifacts unavailable'], ['status', 'links', 'Links unavailable'], ['status', 'raw', 'Identity unavailable']]) test(`async ${text} is announced without stealing focus`, async () => {
  const page = await fixture(tool === 'status' ? {
    endpoint: 'tasks/a/links'
  } : {});
  await page.route('**/api/tasks/a/' + endpoint, route => route.fulfill({
    status: 500,
    json: {}
  }));
  if (tool === 'changes') {
    await page.locator('#tool-status').click();
  }
  const tab = page.locator('#tool-' + tool);
  await tab.focus();
  await page.keyboard.press('Enter');
  await announced(page.locator('#tool-panel').getByText(text, {
    exact: false
  }).last());
  assert.equal(await focused(tab), true);
});
test('async header link error is announced', async () => {
  const page = await fixture({
    endpoint: 'tasks/a/links',
    failureStatus: 403
  });
  await announced(page.locator('.task-header-links span').filter({
    hasText: 'Links unavailable'
  }));
});
for (const operation of ['load', 'switch']) test(`async Claude session ${operation} error is announced`, async () => {
  const page = await fixture();
  await page.route('**/api/tasks/a/claude-sessions', route => route.fulfill(operation === 'load' ? {
    status: 500,
    json: {}
  } : {
    json: {
      sessions: [{
        id: 'other',
        title: 'Other session'
      }]
    }
  }));
  await page.route('**/api/tasks/a/claude-session', route => route.fulfill({
    status: 500,
    json: {}
  }));
  await page.evaluate(async () => {
    const {
        ClaudeSessions
      } = await import('/tasks/settings/claude-sessions.js'),
      {
        Api
      } = await import('/core/api.js');
    window.a11ySessions = new ClaudeSessions(new Api(), () => {});
    void window.a11ySessions.open({
      id: 'a',
      name: 'Session fixture'
    });
  });
  const dialog = page.getByRole('dialog', {
    name: 'Switch Claude session',
    exact: true
  });
  if (operation === 'switch') await dialog.getByRole('button', {
    name: 'Other session',
    exact: false
  }).click();
  await announced(dialog.locator('p').filter({
    hasText: operation === 'load' ? 'Sessions unavailable' : 'Switch was not confirmed'
  }));
  if (operation === 'load') assert.equal(await focused(dialog.getByRole('button', {
    name: 'Cancel',
    exact: true
  })), true);
});
test('async directory error is announced without moving focus', async () => {
  const page = await fixture();
  let release;
  const gate = new Promise(resolve => {
    release = resolve;
  });
  cleanup.push(() => release());
  await page.route('**/api/tasks/a/files?dir=**', async route => {
    await gate;
    await route.fulfill({
      status: 500,
      json: {}
    });
  });
  const row = page.locator('.file-row[data-file-path="directory/"]');
  await row.focus();
  await page.keyboard.press('Enter');
  const before = await page.evaluateHandle(() => document.activeElement);
  release();
  await announced(page.getByText('Directory unavailable.', {
    exact: false
  }));
  assert.equal(await before.evaluate(e => e === document.activeElement), true);
});
test('async review error is announced without stealing focus', async () => {
  const page = await fixture();
  await page.route('**/api/tasks/a/git/diff?path=file.js', route => route.fulfill({
    status: 500,
    json: {}
  }));
  const row = page.locator('.file-row[data-file-path="file.js"]');
  await row.focus();
  await page.keyboard.press('Enter');
  await announced(page.locator('#review-panel .empty-state').filter({
    hasText: 'Diff unavailable'
  }));
  assert.equal(await focused(row), true);
});
for (const kind of ['preview', 'manifest', 'missing']) test(`async artifact ${kind} error is announced`, async () => {
  const page = await fixture();
  if (kind === 'preview') {
    await page.route('**/api/tasks/a/artifacts/note.txt', route => route.fulfill({
      status: 500,
      json: {}
    }));
    await page.locator('#tool-artifacts').click();
    await page.locator('.artifact-row').focus();
    await page.keyboard.press('Enter');
    await announced(page.locator('.artifact-preview .empty-state').filter({
      hasText: 'Preview unavailable'
    }));
    assert.equal(await focused(page.locator('.artifact-row')), true);
  } else {
    if (kind === 'manifest') await page.route('**/api/tasks/a/artifacts', route => route.fulfill({
      status: 500,
      json: {}
    }));
    await page.goto(process.env.WORKSPACE_TEST_URL + '?task=a&view=review&artifact=missing.txt');
    await announced(page.locator('#review-panel .empty-state').filter({
      hasText: 'Artifact unavailable'
    }));
  }
});
test('Review layout preserves the focused equivalent toggle', async () => {
  const page = await fixture();
  await page.locator('.file-row[data-file-path="file.js"]').click();
  for (const name of ['Split', 'Unified']) {
    const toggle = page.getByRole('button', {
      name,
      exact: true
    });
    await toggle.focus();
    await page.keyboard.press('Enter');
    assert.equal(await focused(toggle), true, `${name} must retain focus`);
    assert.equal(await toggle.getAttribute('aria-pressed'), 'true');
  }
  await page.locator('#tool-changes').focus();
  await page.evaluate(async () => {
    const {
      renderDiff
    } = await import('/review/review-diff.js');
    renderDiff(document.querySelector('#review-panel'), {
      path: 'file.js',
      mode: 'split',
      diff: ''
    }, () => {});
  });
  assert.equal(await focused(page.locator('#tool-changes')), true, 'background render must not steal focus');
});
test('Changes layout preserves the focused equivalent toggle', async () => {
  const page = await fixture();
  for (const [before, after] of [['tree', 'flat'], ['flat', 'tree']]) {
    await page.getByRole('button', {
      name: `Show ${before} view`,
      exact: true
    }).focus();
    await page.keyboard.press('Enter');
    assert.equal(await focused(page.getByRole('button', {
      name: `Show ${after} view`,
      exact: true
    })), true);
  }
});
for (const compact of [false, true]) test(`Settings section change retains ${compact ? 'select' : 'navigation button'} focus`, async () => {
  const page = await fixture();
  await page.locator('#workspace-settings').click();
  if (compact) await page.setViewportSize({
    width: 700,
    height: 800
  });
  assert.equal(await focused(page.locator('.settings-back')), true, 'initial entry focuses Back');
  if (compact) {
    const selector = page.getByLabel('Settings section', {
      exact: true
    });
    await selector.focus();
    for (const section of ['system', 'agents']) {
      await selector.selectOption(section);
      assert.equal(await focused(selector), true);
    }
  } else {
    for (const section of ['system', 'agents']) {
      const control = page.locator(`.settings-nav-entry[data-section="${section}"]`);
      await control.focus();
      await page.keyboard.press('Enter');
      assert.equal(await focused(control), true);
    }
  }
});
test('tooltip is hoverable across its gap, persists for focus, and Escape dismisses it', async () => {
  const page = await fixture();
  const owner = page.locator('.changes-view-toggle');
  await owner.hover();
  const bubble = page.getByRole('tooltip');
  await bubble.waitFor();
  const box = await bubble.boundingBox(),
    control = await owner.boundingBox();
  await page.mouse.move(control.x + control.width / 2, box.y > control.y ? control.y + control.height + 4 : control.y - 4);
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2, {
    steps: 2
  });
  await page.waitForTimeout(350);
  assert.equal(await bubble.isVisible(), true, 'tooltip must persist under pointer');
  assert.equal(await bubble.evaluate(e => getComputedStyle(e).pointerEvents), 'auto');
  await page.keyboard.press('Escape');
  assert.equal(await bubble.isVisible(), false);
  await page.mouse.move(800, 700);
  await page.keyboard.press('Tab');
  await owner.focus();
  await bubble.waitFor();
  await owner.hover();
  await page.mouse.move(810, 710);
  await page.waitForTimeout(350);
  assert.equal(await bubble.isVisible(), true, 'focused owner keeps tooltip open');
  await owner.hover();
  await bubble.hover();
  await page.locator('#tab-agent').focus();
  await page.waitForTimeout(350);
  assert.equal(await bubble.isVisible(), true, 'hovered bubble survives owner blur');
  await page.mouse.move(810, 710);
  await bubble.waitFor({
    state: 'hidden'
  });
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});


test('Info identity failure shows unavailable values and can be retried in place', async () => {
  const page = await fixture({endpoint:'tasks/a/raw'});
  const tab=page.locator('#tool-status');
  await tab.focus();await page.keyboard.press('Enter');
  await announced(page.locator('#tool-panel').getByText('Identity unavailable.',{exact:false}));
  for(const label of ['Model','Account','Base branch']) {
    const row=page.locator('.status-row').filter({has:page.locator('dt',{hasText:new RegExp('^'+label+'$')})});
    assert.equal(await row.locator('dd').textContent(),'Not available');
  }
  assert.equal(await focused(tab),true);
  await page.route('**/api/tasks/a/raw',route=>route.fulfill({json:{model:'Recovered model',account:'recovered'}}));
  await page.getByRole('button',{name:'Refresh identity',exact:true}).click();
  await page.getByText('Recovered model',{exact:true}).waitFor();
  assert.equal(await page.getByText('Identity unavailable.',{exact:false}).count(),0);
  assert.equal(await focused(page.getByRole('button',{name:'Refresh identity',exact:true})),true);
});
