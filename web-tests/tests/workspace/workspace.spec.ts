import { test } from '@playwright/test';
import { resetServer } from '../_helpers';
import { refreshWorkspace } from './_workspace-controls-fixture.ts';
// Run only against the existing isolated argus-test-server, never a live daemon.
import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
import path from 'node:path';
import { interruptedSend, browserHistory } from './_browser-cases.ts';
import { railArchive } from './workspace/rail/_rail-browser.ts';
import { rememberedToken } from './_token-browser-cases.ts';
import { renderingNegativeControl } from './_browser-negative.ts';
test('workspace authenticated workflows', async ({
  browser
}) => {
  await resetServer();
  const root = path.resolve(__dirname, "../../..");
  const frontend = process.env.WORKSPACE_TEST_URL || 'http://127.0.0.1:17750';
  const daemon = process.env.WORKSPACE_TEST_API || 'http://127.0.0.1:17744';
  const screenshots = path.join(root, '.context/workspace');
  const headers = {
    Authorization: 'Bearer test-token',
    'Content-Type': 'application/json'
  };
  async function api(resource, options = {}) {
    const response = await fetch(daemon + resource, {
      ...options,
      headers
    });
    assert.equal(response.ok, true, `Isolated API failed (${response.status})`);
    return response.json();
  }
  const initial = (await api('/api/tasks?archived=all')).tasks.find(task => task.name === 'echo-bash');
  assert.ok(initial?.backend === 'bash-test' && initial.worktree_path.includes('/argus-test-home-'), 'Refusing to test a daemon without the isolated test harness worktree.');
  const second = await api('/api/tasks', {
    method: 'POST',
    body: JSON.stringify({
      name: `workspace-second-${Date.now()}`,
      project: 'test-proj',
      backend: 'bash-test'
    })
  });
  await mkdir(screenshots, {
    recursive: true
  });
  const checks = [],
    errors = [],
    mutations = [],
    external = [];
  const record = message => {
    checks.push(message);
    console.log(`PASS ${message}`);
  };
  {
    const page = await browser.newPage({
      viewport: {
        width: 1440,
        height: 900
      }
    });
    page.on('pageerror', error => errors.push(error.message));
    const blockedExternal = [];
    page.on('requestfailed', request => {
      if (new URL(request.url()).hostname === 'external.invalid') blockedExternal.push(request.failure()?.errorText);
    });
    await page.route('https://external.invalid/**', route => {
      external.push(route.request().url());
      return route.abort();
    });
    page.on('request', request => {
      if (request.method() === 'POST') mutations.push({
        path: new URL(request.url()).pathname,
        body: request.postData()
      });
    });
    // Control eligibility while retaining real authenticated reads, PTY writes and output.
    let runtimeOverride = null;
    const runtimeTask = task => task.id === initial.id && runtimeOverride !== null ? {
      ...task,
      idle: runtimeOverride,
      needs_input: false
    } : task;
    await page.route('**/api/tasks?archived=all', async route => {
      const response = await route.fetch();
      if (!response.ok()) return route.fulfill({
        response
      });
      const result = await response.json();
      await route.fulfill({
        response,
        json: {
          ...result,
          tasks: result.tasks.map(runtimeTask)
        }
      });
    });
    await page.route('**/api/tasks/' + initial.id, async route => {
      if (route.request().method() !== 'GET') return route.continue();
      const response = await route.fetch();
      if (!response.ok()) return route.fulfill({
        response
      });
      await route.fulfill({
        response,
        json: runtimeTask(await response.json())
      });
    });
    await page.goto(frontend);
    await page.locator('#token').fill('wrong-test-token');
    await page.locator('#connect-button').click();
    await page.locator('#connect-error:not([hidden])').waitFor();
    assert.equal(await page.locator('#connect-dialog').evaluate(dialog => dialog.open), true);
    await page.locator('#token').fill('test-token');
    await page.locator('#connect-button').click();
    const choose = async name => {
      await page.locator('.task-row').filter({
        has: page.getByText(name, {
          exact: true
        })
      }).click();
    };
    await choose('echo-bash');
    await page.waitForFunction(() => document.querySelector('#terminal-state').textContent === 'Live terminal');
    assert.equal(await page.locator('#draft').evaluate(field => document.activeElement === field), true);
    assert.equal(await page.locator('#token').inputValue(), '');
    record('invalid token recovery and real authenticated terminal');
    await page.locator('#draft').fill('draft for the first task');
    await choose(second.name);
    await page.locator('#draft').fill('draft for the second task');
    await choose('echo-bash');
    assert.equal(await page.locator('#draft').inputValue(), 'draft for the first task');
    record('task scoped unsent drafts');
    await page.waitForFunction(() => document.querySelector('#terminal-state').textContent === 'Live terminal');
    await interruptedSend(page, initial, second, choose, mutations);
    record('task switch during send retains original draft and cannot send to the new task');
    runtimeOverride = true;
    await refreshWorkspace(page);
    await page.waitForTimeout(650);
    const beforeLayoutResizes = mutations.filter(value => value.path.endsWith('/resize')).length;
    const layoutResize = page.waitForResponse(response => new URL(response.url()).pathname === `/api/tasks/${initial.id}/resize`);
    await page.locator('#toggle-nav').click();
    await page.locator('#focus-agent').click();
    assert.equal(await page.locator('#toggle-tools').getAttribute('aria-expanded'), 'false');
    await page.locator('#focus-agent').click();
    assert.equal(await page.locator('#toggle-nav').getAttribute('aria-expanded'), 'false');
    assert.equal(await page.locator('#toggle-tools').getAttribute('aria-expanded'), 'true');
    await page.locator('#toggle-nav').click();
    await page.locator('#nav-resize').focus();
    const width = Number(await page.locator('#nav-resize').getAttribute('aria-valuenow'));
    await page.keyboard.press('ArrowRight');
    assert.equal(Number(await page.locator('#nav-resize').getAttribute('aria-valuenow')), width + 8);
    assert.equal((await layoutResize).ok(), true);
    await page.waitForTimeout(550);
    assert.equal(mutations.filter(value => value.path.endsWith('/resize')).length, beforeLayoutResizes + 1, 'Settled rail changes coalesce into one automatic resize');
    assert.deepEqual(await api(`/api/tasks/${initial.id}/size`), JSON.parse(mutations.filter(value => value.path.endsWith('/resize')).at(-1).body));
    runtimeOverride = null;
    record('rail focus restoration and keyboard resizing coalesce a real automatic Fit');
    const proofName = `WORKSPACE_VERIFIED_${Date.now()}`;
    await page.locator('#draft').fill("printf '# Workspace verification\\n\\nReal file in the isolated worktree.\\n' > workspace-check.md; mkdir -p workspace-dir; printf 'Nested file\\n' > workspace-dir/nested.txt; printf 'WORKSPACE_VERIFIED_%s\\n' '" + proofName.split('_').at(-1) + "'");
    const submitResponse = page.waitForResponse(response => new URL(response.url()).pathname === `/api/tasks/${initial.id}/input` && response.request().postData() === '\r');
    await page.locator('#send').click();
    await page.waitForFunction(() => document.querySelector('#draft').value === '');
    const proof = await submitResponse;
    assert.equal(proof.ok(), true);
    await page.waitForFunction(proof => document.querySelector('.xterm-accessibility-tree')?.textContent.includes(proof), proofName);
    const output = await fetch(`${daemon}/api/tasks/${initial.id}/output?bytes=1048576`, {
      headers
    });
    assert.ok((await output.text()).split(/\r?\n/).includes(proofName), 'Expected an independently assembled proof line from bash execution');
    record('compose reaches isolated bash and real output confirms execution');
    await page.locator('#tool-changes').click();
    assert.equal(await page.getByRole('button', {
      name: 'Refresh changes',
      exact: true
    }).count(), 0);
    await page.locator('.file-row').filter({
      hasText: 'workspace-check.md'
    }).click();
    await page.locator('.diff-table.unified').waitFor();
    await page.waitForTimeout(450);
    const hiddenResizeCount = mutations.filter(value => value.path.endsWith('/resize')).length;
    assert.match(await page.locator('#review-panel').textContent(), /Real file in the isolated worktree/);
    await page.getByRole('button', {
      name: 'Split',
      exact: true
    }).click();
    await page.locator('.diff-table.split').waitFor();
    await browserHistory(page, initial);
    record('review URL, Back and Forward preserve the selected comparison view');
    assert.equal(mutations.filter(value => value.path.endsWith('/resize')).length, hiddenResizeCount, 'Hidden review terminal must not issue resizes');
    await page.screenshot({
      path: path.join(screenshots, 'desktop-review.png')
    });
    record('real git file reads, unified and split review beside the agent');
    await page.locator('.file-row').filter({
      hasText: 'workspace-dir/'
    }).click();
    await page.locator('.directory-files .file-row').filter({
      hasText: 'nested.txt'
    }).click();
    await page.waitForFunction(() => document.querySelector('#review-panel').textContent.includes('Nested file'));
    record('untracked directory expansion uses the existing changed-file endpoint');
    await page.locator('#tab-agent').click();
    const keyResponse = page.waitForResponse(response => new URL(response.url()).pathname === `/api/tasks/${initial.id}/input` && response.request().postData() === 'a');
    await page.locator('.xterm-helper-textarea').focus();
    await page.keyboard.type('a');
    assert.equal((await keyResponse).ok(), true);
    await page.keyboard.press('Control+u');
    const beforeKey = mutations.length;
    await page.locator('.xterm-helper-textarea').focus();
    await page.keyboard.press('Control+z');
    await page.waitForTimeout(100);
    assert.equal(mutations.slice(beforeKey).some(value => value.body?.includes('\x1a')), false);
    runtimeOverride = false;
    await refreshWorkspace(page);
    const beforeBusyResize = mutations.filter(value => value.path.endsWith('/resize')).length;
    await page.setViewportSize({
      width: 1350,
      height: 900
    });
    await page.waitForTimeout(650);
    assert.equal(mutations.filter(value => value.path.endsWith('/resize')).length, beforeBusyResize, 'Busy runtime defers automatic fitting');
    assert.equal(await page.locator('#fit-session').getAttribute('data-fit-needed'), 'true');
    const resizeResponse = page.waitForResponse(response => new URL(response.url()).pathname.endsWith('/resize'));
    await page.locator('#fit-session').click();
    assert.equal((await resizeResponse).ok(), true);
    await page.waitForFunction(() => !document.querySelector('#fit-session').disabled);
    assert.equal(mutations.filter(value => value.path.endsWith('/resize')).length, beforeBusyResize + 1);
    assert.deepEqual(await api(`/api/tasks/${initial.id}/size`), JSON.parse(mutations.filter(value => value.path.endsWith('/resize')).at(-1).body));
    runtimeOverride = null;
    const fitProof = `FITTED_${Date.now()}`;
    await page.locator('#draft').fill("printf 'FITTED_%s\\n' '" + fitProof.split('_')[1] + "'");
    await page.locator('#send').click();
    await page.waitForFunction(proof => document.querySelector('.xterm-accessibility-tree')?.textContent.includes(proof), fitProof);
    record('Ctrl+Z is blocked; busy sessions defer automatic Fit and retain the manual fallback');
    await page.route('**/api/tasks/*/artifacts', route => route.fulfill({
      json: {
        artifacts: [{
          name: 'Sandbox check',
          filename: 'sandbox.html',
          type: 'html',
          size: 100
        }]
      }
    }));
    await page.route('**/api/tasks/*/artifacts/sandbox.html', route => route.fulfill({
      contentType: 'text/html',
      body: '<h1>Safe artifact</h1><script>parent.document.body.dataset.compromised="yes"</script><img src="https://external.invalid/leak">'
    }));
    await page.locator('#tool-artifacts').click();
    const externalFailure = page.waitForEvent('requestfailed', {
      predicate: request => request.url() === 'https://external.invalid/leak',
    });
    await page.getByRole('button', {
      name: /Sandbox check/
    }).click();
    const frame = page.locator('#review-panel iframe');
    await frame.waitFor();
    assert.equal(await frame.getAttribute('sandbox'), '');
    assert.doesNotMatch(await frame.getAttribute('srcdoc'), /<script|test-token/);
    assert.equal(await page.locator('body').getAttribute('data-compromised'), null);
    await externalFailure;
    assert.deepEqual(external, []);
    assert.deepEqual(blockedExternal, ['csp']);
    record('artifact HTML stays sandboxed without script or token access');
    await railArchive(page, initial, second, choose, api, record);
    await page.locator('#tab-agent').click();
    await page.screenshot({
      path: path.join(screenshots, 'desktop-agent.png')
    });
    await page.setViewportSize({
      width: 390,
      height: 844
    });
    await page.locator('#toggle-nav').click();
    await page.locator('#search').fill(second.name);
    await choose(second.name);
    assert.equal(await page.locator('#toggle-nav').getAttribute('aria-expanded'), 'false');
    assert.equal(await page.locator('#draft').evaluate(field => document.activeElement === field), true);
    await page.locator('#toggle-tools').click();
    await page.keyboard.press('Escape');
    assert.equal(await page.locator('#toggle-tools').getAttribute('aria-expanded'), 'false');
    assert.equal(await page.locator('#toggle-tools').evaluate(control => document.activeElement === control), true);
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
    await page.screenshot({
      path: path.join(screenshots, 'compact-agent.png')
    });
    record('compact drawers, search, Escape and focus return');
    await page.locator('#disconnect').click();
    assert.equal(await page.locator('#connect-dialog').evaluate(dialog => dialog.open), true);
    assert.equal(await page.locator('#draft').inputValue(), '');
    const stored = await page.evaluate(() => JSON.stringify({
      ...localStorage,
      ...sessionStorage
    }));
    assert.doesNotMatch(stored, /test-token|draft for/);
    record('disconnect clears drafts and remembered credentials from browser storage');
    await rememberedToken(browser, frontend, record);
    await renderingNegativeControl(browser, frontend, initial);
    record('terminal visibility assertion rejects a deliberately disabled output renderer');
    assert.deepEqual(errors, []);
    console.log(`Verified ${checks.length} browser workflows. Screenshots in .context/workspace.`);
  }
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
test('artifact egress probe detects a permissive preview policy', async ({
  page
}) => {
  const external = [];
  await page.route('https://external.invalid/**', route => {
    external.push(route.request().url());
    return route.abort();
  });
  await page.goto('/');
  const failed = page.waitForEvent('requestfailed', {
    predicate: request => new URL(request.url()).hostname === 'external.invalid'
  });
  await page.evaluate(async () => {
    const {
      safeHTML
    } = await import('/review/artifact-view.js');
    const frame = document.createElement('iframe');
    frame.setAttribute('sandbox', '');
    frame.srcdoc = safeHTML('<img src="https://external.invalid/leak">').replace('img-src data: blob:', 'img-src https:');
    document.body.append(frame);
  });
  const request = await failed;
  assert.deepEqual(external, ['https://external.invalid/leak']);
  assert.notEqual(request.failure()?.errorText, 'csp');
});
