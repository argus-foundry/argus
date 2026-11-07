import { test } from '@playwright/test';
import { resetServer } from '../../_helpers';
import { auditIconTooltips } from '../shared/_icon-tooltip-audit.ts';
import assert from 'node:assert/strict';
import path from 'node:path';
let browser, root, frontend, settings, open, section;
const cleanup = [];
async function fixture() {
  const page = await browser.newPage();
  cleanup.push(() => page.close());
  const errors = [],
    writes = [],
    state = {
      settings: settings(),
      projects: [{
        name: 'demo',
        path: '/tmp/demo',
        branch: 'main',
        sandbox: {
          enabled: false,
          deny_read: ['/private'],
          extra_write: ['/tmp/safe'],
          allow_apple_events: ['com.apple.finder']
        }
      }],
      fail: false
    };
  page.on('pageerror', error => errors.push(error.message));
  cleanup.push(() => assert.deepEqual(errors, []));
  page.on('request', request => {
    if (!['GET', 'HEAD'].includes(request.method())) writes.push({
      path: new URL(request.url()).pathname,
      method: request.method(),
      body: request.postDataJSON()
    });
  });
  await page.route('**/api/**', route => route.fulfill({
    status: 404,
    json: {}
  }));
  await page.route('**/api/tasks?archived=all', route => route.fulfill({
    json: {
      tasks: [{
        id: 'a',
        name: 'Task A',
        project: 'demo',
        status: 'complete',
        live: false
      }]
    }
  }));
  await page.route('**/api/hera', route => route.fulfill({
    json: {
      orchestrators: []
    }
  }));
  await page.route('**/api/backends', route => route.fulfill({
    json: {
      backends: [{
        name: 'alpha'
      }, {
        name: 'beta'
      }]
    }
  }));
  await page.route('**/api/projects/full', route => route.fulfill({
    json: {
      projects: state.projects
    }
  }));
  await page.route('**/api/settings', async route => {
    if (route.request().method() === 'PUT') {
      if (state.fail) return route.fulfill({
        status: 500,
        json: {}
      });
      for (const [key, value] of Object.entries(route.request().postDataJSON())) state.settings[key] = {
        ...state.settings[key],
        ...value
      };
    }
    return route.fulfill({
      json: state.settings
    });
  });
  await page.route('**/api/projects', route => {
    const body = route.request().postDataJSON();
    state.projects.push(body);
    return route.fulfill({
      status: 201,
      json: body
    });
  });
  await page.route('**/api/projects/*', route => {
    const name = decodeURIComponent(new URL(route.request().url()).pathname.split('/').at(-1));
    if (route.request().method() === 'DELETE') state.projects = state.projects.filter(project => project.name !== name);else state.projects = state.projects.map(project => project.name === name ? route.request().postDataJSON() : project);
    return route.fulfill({
      json: {}
    });
  });
  // Specific read wins over the project mutation wildcard.
  await page.route('**/api/projects/full', route => route.fulfill({
    json: {
      projects: state.projects
    }
  }));
  await page.goto(frontend);
  await page.locator('#token').fill('fixture-token');
  await page.locator('#connect-button').click();
  await page.locator('.task-row').waitFor();
  return {
    page,
    state,
    writes
  };
}
const surfaceShortcuts = [['new', 'Alt+Shift+N', '#new-task-view'], ['rename', 'Alt+Shift+R', '#task-settings-panel'], ['fork', 'Alt+Shift+B', '#task-settings-panel'], ['delete', 'Alt+Shift+Backspace', '#task-action-dialog'], ['archive', 'Alt+Shift+A', '#archive-dialog'], ['filter', 'Alt+Shift+F', '#search'], ['split', 'Alt+Shift+D', '#secondary-task-picker'], ['agent', 'Alt+1', '#agent-panel'], ['review', 'Alt+2', '#review-panel'], ['files', 'Alt+3', '#tools'], ['info', 'Alt+4', '#tools'], ['attention', 'Alt+Shift+J', '#agent-panel']];
test.beforeAll(async ({
  browser: workerBrowser
}) => {
  browser = workerBrowser;
  await resetServer();
  root = path.resolve(__dirname, "../../../..");
  frontend = process.env.WORKSPACE_TEST_URL || 'http://127.0.0.1:17750';
  settings = () => ({
    kb: {
      enabled: true,
      metis_vault_path: '/tmp/vault'
    },
    api: {
      enabled: true,
      http_port: 17744
    },
    defaults: {
      backend: 'alpha',
      share_project: 'demo'
    },
    sandbox: {
      enabled: true
    },
    bridge: {
      enabled: true
    }
  });
  open = page => page.getByRole('button', {
    name: 'Settings',
    exact: true
  }).click();
  section = (page, name) => page.locator('#settings-panel').getByRole('button', {
    name,
    exact: true
  }).click();
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
for (const entry of ['schedules', 'rename', 'new']) {
  test(`compact Tools shortcut opens interactive ${entry} and preserves desktop layout`, async () => {
    const {
      page
    } = await fixture();
    await page.route('**/api/schedules', route => route.fulfill({
      json: {
        schedules: []
      }
    }));
    await page.route('**/api/tasks/a/raw', route => route.fulfill({
      json: {
        id: 'a',
        name: 'Task A',
        project: 'demo',
        status: 'complete'
      }
    }));
    await page.locator('#toggle-nav').click();
    const preference = await page.evaluate(() => localStorage.getItem('argus-workspace:layout'));
    await page.setViewportSize({
      width: 390,
      height: 844
    });
    await page.locator('#toggle-tools').click();
    await page.locator('#tool-status').focus();
    assert.equal(await page.locator('#main').evaluate(el => el.inert), true);
    await page.keyboard.press(`Alt+Shift+${{
      schedules: 'S',
      rename: 'R',
      new: 'N'
    }[entry]}`);
    const panel = page.locator({
      schedules: '#settings-panel',
      rename: '#task-settings-panel',
      new: '#new-task-view'
    }[entry]);
    await panel.waitFor();
    assert.equal(await page.locator('#main').evaluate(el => el.inert), false);
    assert.equal(await page.locator('#toggle-tools').getAttribute('aria-expanded'), 'false');
    assert.equal(await panel.evaluate(el => el.contains(document.activeElement)), true);
    if (entry === 'schedules') {
      await page.getByLabel('Settings section', {
        exact: true
      }).selectOption('general');
      await page.getByLabel('Vault path', {
        exact: true
      }).fill('/tmp/compact-click');
      await section(page, '← Back');
    } else if (entry === 'rename') {
      await page.locator('#task-field-task-name').fill('Compact rename');
      await panel.getByRole('button', {
        name: 'Back to task',
        exact: true
      }).click();
    } else {
      await page.locator('#new-task-prompt').fill('Compact creation');
      await page.getByRole('button', {
        name: 'Cancel new task',
        exact: true
      }).click();
    }
    assert.equal(await panel.isVisible(), false);
    assert.equal(await page.locator('#main').evaluate(el => el.inert), false);
    assert.equal(await page.evaluate(() => localStorage.getItem('argus-workspace:layout')), preference);
  });
}
test('settings home replaces the footer fallback and returns through Escape and browser history', async () => {
  const {
    page,
    writes
  } = await fixture();
  assert.equal(await page.getByRole('button', {
    name: 'Settings',
    exact: true
  }).count(), 1);
  assert.equal(await page.locator('.nav-footer a[href="/original"]').count(), 0);
  await page.locator('#draft').fill('Keep task draft');
  await open(page);
  await page.getByLabel('Vault path', {
    exact: true
  }).waitFor();
  assert.equal(new URL(page.url()).searchParams.get('settings'), 'general');
  await page.keyboard.press('Escape');
  await page.waitForFunction(() => document.querySelector('#settings-panel').hidden);
  assert.equal(await page.locator('#draft').inputValue(), 'Keep task draft');
  await page.goForward();
  await page.locator('#settings-panel:not([hidden])').waitFor();
  await section(page, '← Back');
  await page.waitForFunction(() => document.querySelector('#settings-panel').hidden);
  assert.deepEqual(writes, []);
});
test('General keeps failed input, sends partial settings and shows restart notices', async () => {
  const {
    page,
    state,
    writes
  } = await fixture();
  await open(page);
  await page.getByLabel('Vault path', {
    exact: true
  }).fill('/tmp/new-vault');
  await page.getByLabel('Default agent', {
    exact: true
  }).selectOption('beta');
  await page.getByLabel('HTTP API enabled', {
    exact: true
  }).uncheck();
  state.fail = true;
  await page.getByRole('button', {
    name: 'Save General',
    exact: true
  }).click();
  await page.locator('#settings-status').filter({
    hasText: 'not confirmed'
  }).waitFor();
  assert.equal(await page.getByLabel('Vault path', {
    exact: true
  }).inputValue(), '/tmp/new-vault');
  state.fail = false;
  await page.getByRole('button', {
    name: 'Save General',
    exact: true
  }).click();
  await page.locator('#settings-status').filter({
    hasText: 'Saved'
  }).waitFor();
  assert.match(await page.locator('#settings-restart').textContent(), /restart required/i);
  const body = writes.filter(write => write.path === '/api/settings').at(-1).body;
  assert.deepEqual(Object.keys(body).sort(), ['api', 'defaults', 'kb']);
  assert.equal(body.defaults.backend, 'beta');
  assert.equal(body.api.enabled, false);
  assert.equal(state.settings.bridge.enabled, true);
  assert.equal(state.settings.sandbox.enabled, true);
});
test('Projects preserve overrides, reject duplicate adds and confirm removal without touching tasks', async () => {
  const {
    page,
    state,
    writes
  } = await fixture();
  await open(page);
  await section(page, 'demo');
  assert.equal(await page.getByLabel('Project name', {
    exact: true
  }).isDisabled(), true);
  assert.match(await page.locator('#project-profile-warning').textContent(), /profile/i);
  await page.getByLabel('Repository path', {
    exact: true
  }).fill('/tmp/new-demo');
  await page.getByText('Project sandbox overrides', {
    exact: true
  }).click();
  await page.getByLabel('Project sandbox', {
    exact: true
  }).selectOption('inherit');
  await page.getByRole('button', {
    name: 'Save project',
    exact: true
  }).click();
  await page.locator('#settings-status').filter({
    hasText: 'Saved project.'
  }).waitFor();
  assert.equal(state.projects[0].sandbox.enabled, null);
  assert.deepEqual(state.projects[0].sandbox.allow_apple_events, ['com.apple.finder']);
  await page.getByRole('button', {
    name: 'Add repository',
    exact: true
  }).click();
  await page.getByLabel('Project name', {
    exact: true
  }).fill('demo');
  await page.getByLabel('Repository path', {
    exact: true
  }).fill('/tmp/new');
  await page.getByRole('button', {
    name: 'Save project',
    exact: true
  }).click();
  await page.locator('#settings-status').filter({
    hasText: 'already exists'
  }).waitFor();
  assert.equal(writes.filter(write => write.method === 'POST').length, 0);
  await page.getByLabel('Project name', {
    exact: true
  }).fill('second');
  await page.getByRole('button', {
    name: 'Save project',
    exact: true
  }).click();
  await page.locator('#settings-status').filter({
    hasText: 'Saved project.'
  }).waitFor();
  await page.getByRole('button', {
    name: 'Remove second',
    exact: true
  }).click();
  assert.match(await page.locator('#project-delete-dialog').textContent(), /tasks and worktrees remain/i);
  await page.locator('#project-delete-dialog').getByRole('button', {
    name: 'Cancel',
    exact: true
  }).click();
  assert.equal(state.projects.length, 2);
  await page.getByRole('button', {
    name: 'Remove second',
    exact: true
  }).click();
  await page.getByRole('button', {
    name: 'Remove project',
    exact: true
  }).click();
  await page.getByRole('button', {
    name: 'Remove second',
    exact: true
  }).waitFor({
    state: 'detached'
  });
  assert.equal(state.projects.length, 1);
  assert.equal(writes.some(write => write.path.includes('/tasks/')), false);
});
test('compact Settings replaces drawers and keeps dialog Escape scoped', async () => {
  const {
    page
  } = await fixture();
  await page.setViewportSize({
    width: 390,
    height: 844
  });
  await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
  await page.locator('#toggle-nav').click();
  await open(page);
  await page.getByLabel('Vault path', {
    exact: true
  }).waitFor();
  assert.equal(await page.locator('#main').evaluate(element => element.inert), false);
  assert.ok(await page.evaluate(() => document.scrollingElement.scrollWidth <= document.scrollingElement.clientWidth));
  await page.getByLabel('Settings section', {
    exact: true
  }).selectOption('repository:demo');
  await page.getByLabel('Repository path', {
    exact: true
  }).waitFor();
  assert.ok((await page.locator('.settings-navigation').boundingBox()).height <= 100, 'Compact selector must leave room for the editor');
  assert.equal(await page.locator('#toggle-tools').isVisible(), false);
  await page.getByRole('button', {
    name: 'Remove demo',
    exact: true
  }).click();
  await page.keyboard.press('Escape');
  assert.equal(await page.locator('#settings-panel').isVisible(), true);
  await page.keyboard.press('Escape');
  await page.waitForFunction(() => document.querySelector('#settings-panel').hidden);
});
test('settings load failures retry and closed views ignore delayed responses', async () => {
  const {
    page,
    state
  } = await fixture();
  await page.route('**/api/settings', route => route.fulfill({
    status: 500,
    json: {}
  }), {
    times: 1
  });
  await open(page);
  await page.getByRole('button', {
    name: 'Retry General',
    exact: true
  }).click();
  await page.getByLabel('Vault path', {
    exact: true
  }).waitFor();
  await section(page, 'demo');
  await page.getByLabel('Repository path', {
    exact: true
  }).waitFor();
  let release, reached;
  const gate = new Promise(resolve => {
      release = resolve;
    }),
    held = new Promise(resolve => {
      reached = resolve;
    });
  await page.route('**/api/settings', async route => {
    reached();
    await gate;
    try {
      await route.fulfill({
        json: state.settings
      });
    } catch {/* Closed view canceled the read. */}
  });
  await section(page, 'General');
  await held;
  await page.keyboard.press('Escape');
  await page.waitForFunction(() => document.querySelector('#settings-panel').hidden);
  release();
  await page.waitForFunction(() => document.querySelector('#settings-panel').hidden && document.querySelector('#settings-status').textContent === '');
  assert.equal(await page.getByLabel('Vault path', {
    exact: true
  }).count(), 0);
});
test('Settings and task creation release each others focus boundaries', async () => {
  const {
    page
  } = await fixture();
  await page.getByRole('button', {
    name: 'New task in demo',
    exact: true
  }).click();
  await page.locator('#new-task-prompt').fill('Keep creation draft');
  await open(page);
  await page.getByLabel('Vault path', {
    exact: true
  }).fill('/tmp/accessible');
  assert.equal(await page.locator('#settings-panel').evaluate(el => el.inert), false);
  assert.equal(await page.locator('#new-task-view').isVisible(), false);
  await page.keyboard.press('Escape');
  await page.waitForFunction(() => document.querySelector('#settings-panel').hidden);
  await page.getByRole('button', {
    name: 'New task in demo',
    exact: true
  }).click();
  assert.equal(await page.locator('#settings-panel').isVisible(), false);
  assert.equal(await page.locator('#new-task-prompt').inputValue(), 'Keep creation draft');
  await page.keyboard.press('Escape');
  assert.equal(await page.locator('#draft').evaluate(el => el.closest('[inert]')), null);
});
test('grouped settings search exposes repository editors and bounds mobile navigation', async () => {
  const {
    page
  } = await fixture();
  await open(page);
  for (const name of ['Personal', 'Agents & environment', 'Automation', 'System', 'Repositories']) assert.ok(await page.locator('.settings-nav-group').filter({
    has: page.getByText(name, {
      exact: true
    })
  }).count());
  const search = page.getByRole('searchbox', {
    name: 'Search settings',
    exact: true
  });
  await search.fill('demo');
  await page.locator('.settings-nav').getByRole('button', {
    name: 'demo',
    exact: true
  }).click();
  await page.getByLabel('Repository path', {
    exact: true
  }).waitFor();
  assert.equal(await page.getByLabel('Project name', {
    exact: true
  }).inputValue(), 'demo');
  assert.match(new URL(page.url()).searchParams.get('settings'), /^repository:/);
  await search.fill('not-present');
  assert.equal(await page.locator('.settings-nav-entry:visible').count(), 0);
  await search.fill('');
  await page.getByRole('button', {
    name: 'Add repository',
    exact: true
  }).click();
  assert.equal(await page.getByLabel('Project name', {
    exact: true
  }).inputValue(), '');
  await page.setViewportSize({
    width: 390,
    height: 844
  });
  assert.ok((await page.locator('.settings-navigation').boundingBox()).height <= 240);
  assert.ok(await page.evaluate(() => document.scrollingElement.scrollWidth <= innerWidth));
});
test('project editor route opens its existing editor and reports a missing project', async () => {
  const {
    page,
    writes
  } = await fixture();
  await page.goto(frontend + '?settings=repository%3Ademo');
  await page.getByLabel('Repository path', {
    exact: true
  }).waitFor({
    timeout: 5000
  });
  assert.equal(await page.getByLabel('Project name', {
    exact: true
  }).inputValue(), 'demo');
  assert.equal(await page.getByLabel('Repository path', {
    exact: true
  }).inputValue(), '/tmp/demo');
  assert.deepEqual(writes, []);
  await page.goto(frontend + '?settings=repository%3Amissing-project');
  await page.locator('#settings-status').filter({
    hasText: 'no longer available'
  }).waitFor({
    timeout: 5000
  });
  assert.equal(await page.getByRole('button', {
    name: 'Add repository',
    exact: true
  }).isVisible(), true);
  assert.deepEqual(writes, []);
});
test('repository creation rechecks newly added project names before the upsert endpoint', async () => {
  const {
    page,
    state,
    writes
  } = await fixture();
  await open(page);
  await section(page, 'Add repository');
  await page.getByLabel('Project name', {
    exact: true
  }).fill('added-elsewhere');
  await page.getByLabel('Repository path', {
    exact: true
  }).fill('/tmp/new');
  state.projects.push({
    name: 'added-elsewhere',
    path: '/tmp/existing'
  });
  await page.getByRole('button', {
    name: 'Save project',
    exact: true
  }).click();
  await page.locator('#settings-status').filter({
    hasText: 'already exists'
  }).waitFor();
  assert.equal(writes.some(write => write.method === 'POST'), false);
});
test('Settings owns the full window and restores the exact task review route', async () => {
  const {
    page
  } = await fixture();
  await page.route('**/api/tasks/a/git/diff?path=notes.txt', route => route.fulfill({
    json: {
      Diff: 'diff --git a/notes.txt b/notes.txt\n--- a/notes.txt\n+++ b/notes.txt\n@@ -1 +1 @@\n-before\n+after\n'
    }
  }));
  await page.goto(frontend + '?task=a&view=review&file=notes.txt&mode=split');
  await page.locator('#review-panel').getByText('+after', {
    exact: true
  }).waitFor();
  const previous = page.url();
  await open(page);
  await page.getByLabel('Vault path', {
    exact: true
  }).waitFor();
  await page.keyboard.press('/');
  assert.equal(await page.getByRole('searchbox', {
    name: 'Search settings',
    exact: true
  }).evaluate(e => e === document.activeElement), true);
  for (const id of ['navigation', 'tools', 'nav-resize', 'tools-resize']) assert.equal(await page.locator('#' + id).isVisible(), false, id);
  assert.equal(await page.locator('.workspace-header').isVisible(), false);
  for (const id of ['navigation', 'tools']) assert.equal(await page.locator('#' + id).evaluate(e => e.inert), true);
  assert.equal((await page.locator('.settings-nav-entry svg').count()) >= 9, true);
  const panel = await page.locator('#settings-panel').boundingBox();
  assert.equal(panel.x, 0);
  assert.equal(panel.y, 0);
  assert.equal(panel.width, 1280);
  assert.equal(panel.height, 720);
  assert.equal(await page.locator('.settings-navigation').getByRole('button', {
    name: '← Back',
    exact: true
  }).count(), 1);
  await section(page, '← Back');
  await page.waitForURL(previous);
  assert.equal(await page.locator('#tab-review').getAttribute('aria-selected'), 'true');
  assert.equal(await page.locator('#task-title').textContent(), 'Task A');
  for (const id of ['navigation', 'tools']) assert.equal(await page.locator('#' + id).evaluate(e => e.inert), false);
  await open(page);
  await page.keyboard.press('Escape');
  await page.waitForURL(previous);
  assert.equal(await page.locator('#review-panel').isVisible(), true);
  await page.locator('#review-panel').getByText('+after', {
    exact: true
  }).waitFor();
  assert.equal(new URL(page.url()).searchParams.get('mode'), 'split');
});
test('Settings reload and shared rows fit narrow, desktop and ultrawide windows', async () => {
  const {
    page
  } = await fixture();
  await page.goto(frontend + '?task=a&view=review&settings=general');
  await page.getByLabel('Vault path', {
    exact: true
  }).waitFor();
  for (const width of [390, 1280, 2544]) {
    await page.setViewportSize({
      width,
      height: 900
    });
    assert.equal(await page.locator('#navigation').isVisible(), false);
    assert.equal(await page.locator('#tools').isVisible(), false);
    assert.ok(await page.evaluate(() => document.scrollingElement.scrollWidth <= innerWidth));
    assert.ok(await page.locator('.settings-content').evaluate(e => e.scrollWidth <= e.clientWidth));
    const row = page.locator('.settings-field').filter({
      has: page.getByLabel('Vault path', {
        exact: true
      })
    });
    assert.equal(await row.evaluate(e => getComputedStyle(e).borderBottomWidth), '1px');
    if (width > 700) {
      const label = await row.locator('span').first().boundingBox(),
        input = await row.locator('input').boundingBox(),
        hint = await row.locator('small').boundingBox();
      assert.ok(input.x > label.x + label.width);
      assert.ok(hint.x < input.x);
      assert.ok((await page.locator('.settings-page').boundingBox()).width <= 1040);
    } else {
      await page.getByLabel('Settings section', {
        exact: true
      }).selectOption('repository:demo');
      await page.getByLabel('Repository path', {
        exact: true
      }).waitFor();
      await page.getByLabel('Settings section', {
        exact: true
      }).selectOption('general');
      await page.getByLabel('Vault path', {
        exact: true
      }).waitFor();
    }
    await page.screenshot({
      path: path.join(root, `.context/settings-shell/settings-${width}.png`)
    });
  }
  await section(page, '← Back');
  await page.waitForFunction(() => document.querySelector('#settings-panel').hidden);
  assert.equal(new URL(page.url()).searchParams.get('task'), 'a');
  assert.equal(await page.locator('#tab-review').getAttribute('aria-selected'), 'true');
});
test('compact selector and Back respect the existing pending-save boundary', async () => {
  const {
    page,
    state
  } = await fixture();
  await page.setViewportSize({
    width: 390,
    height: 844
  });
  await page.locator('#toggle-nav').click();
  await open(page);
  await page.getByLabel('Vault path', {
    exact: true
  }).waitFor();
  let release, reached;
  const gate = new Promise(resolve => {
      release = resolve;
    }),
    held = new Promise(resolve => {
      reached = resolve;
    });
  await page.route('**/api/settings', async route => {
    reached();
    await gate;
    await route.fulfill({
      json: state.settings
    });
  });
  await page.getByRole('button', {
    name: 'Save General',
    exact: true
  }).click();
  await held;
  assert.equal(await page.getByLabel('Settings section', {
    exact: true
  }).isDisabled(), true);
  assert.equal(await page.getByRole('button', {
    name: '← Back',
    exact: true
  }).isDisabled(), true);
  await page.keyboard.press('Escape');
  assert.equal(await page.locator('#settings-panel').isVisible(), true);
  release();
  await page.locator('#settings-status').filter({
    hasText: 'Saved'
  }).waitFor();
  assert.equal(await page.getByLabel('Settings section', {
    exact: true
  }).isDisabled(), false);
  await page.getByLabel('Settings section', {
    exact: true
  }).selectOption('repository:demo');
  await page.getByLabel('Repository path', {
    exact: true
  }).waitFor();
  await page.keyboard.press('Escape');
  await page.waitForFunction(() => document.querySelector('#settings-panel').hidden);
});
test('Settings icon controls declare accessible delegated tooltips and search clear works', async () => {
  const {
    page
  } = await fixture();
  const gear = page.locator('#workspace-settings');
  assert.equal(await gear.getAttribute('data-tooltip'), 'Settings');
  assert.equal(await gear.getAttribute('title'), null);
  await open(page);
  await page.getByLabel('Vault path', {
    exact: true
  }).waitFor();
  await auditIconTooltips(page, 'body');
  const controls = page.locator('.settings-nav-entry');
  for (const control of await controls.all()) {
    assert.equal(await control.getAttribute('aria-label'), null);
    const repository = (await control.getAttribute('data-section')).startsWith('repository:');
    assert.equal(await control.getAttribute('data-tooltip'), repository ? (await control.textContent()).slice(1) : null);
    if (repository) assert.equal(await control.getAttribute('data-tooltip-overflow'), '');
    assert.equal(await control.getAttribute('title'), null);
  }
  const search = page.getByRole('searchbox', {
    name: 'Search settings',
    exact: true
  });
  await search.fill('vault');
  const clear = page.getByRole('button', {
    name: 'Clear settings search',
    exact: true
  });
  assert.equal(await clear.getAttribute('data-tooltip'), 'Clear settings search');
  await clear.click();
  assert.equal(await search.inputValue(), '');
  assert.equal(await search.evaluate(e => document.activeElement === e), true);
  assert.equal(await clear.isVisible(), false);
  assert.ok((await page.locator('.settings-nav-entry:visible').count()) >= 10);
});
test('Settings delegated tooltips render on hover and keyboard focus inside the full-window shell', async () => {
  const {
      page
    } = await fixture(),
    tooltip = page.getByRole('tooltip');
  await page.locator('#workspace-settings').hover();
  await tooltip.waitFor();
  assert.equal(await tooltip.textContent(), 'Settings');
  await open(page);
  await page.getByLabel('Vault path', {
    exact: true
  }).waitFor();
  await page.locator('.settings-nav-entry[data-section="system"]').hover();
  await page.waitForTimeout(500);
  assert.equal(await tooltip.isVisible(), false);
  await page.keyboard.press('Tab');
  await page.keyboard.press('Shift+Tab');
  await page.locator('.settings-back').focus();
  await tooltip.filter({
    hasText: /^Esc$/
  }).waitFor();
  assert.equal(await tooltip.locator('kbd').textContent(), 'Esc');
  assert.match(await page.getByRole('button', {
    name: '← Back',
    exact: true
  }).getAttribute('aria-describedby'), /workspace-tooltip/);
  await page.getByRole('searchbox', {
    name: 'Search settings',
    exact: true
  }).fill('vault');
  const clear = page.getByRole('button', {
    name: 'Clear settings search',
    exact: true
  });
  await clear.hover();
  await tooltip.filter({
    hasText: /^Clear settings search$/
  }).waitFor();
  await clear.click();
  await tooltip.waitFor({
    state: 'hidden'
  });
  await page.setViewportSize({
    width: 390,
    height: 844
  });
  await page.getByRole('button', {
    name: '← Back',
    exact: true
  }).hover();
  await tooltip.waitFor();
  const box = await tooltip.boundingBox();
  assert.ok(box.x >= 0 && box.x + box.width <= 390);
  await page.screenshot({
    path: path.join(root, '.context/settings-shell/tooltip-mobile.png')
  });
});
for (const [action, chord, target] of surfaceShortcuts) {
  test(`Settings shortcut ${action} releases Settings before opening its usable surface`, async () => {
    const {
      page,
      writes
    } = await fixture();
    await page.route('**/api/tasks/a/raw', route => route.fulfill({
      json: {
        id: 'a',
        name: 'Task A',
        project: 'demo',
        status: 'complete'
      }
    }));
    await page.route('**/api/tasks/a/fork', route => route.fulfill({
      status: 503,
      json: {}
    }));
    if (action === 'attention') {
      await page.route('**/api/tasks?archived=all', route => route.fulfill({
        json: {
          tasks: [{
            id: 'a',
            name: 'Task A',
            project: 'demo',
            status: 'complete',
            needs_input: true
          }]
        }
      }));
      await page.reload();
      await page.locator('.task-row').waitFor();
    }
    await open(page);
    await page.getByLabel('Vault path', {
      exact: true
    }).waitFor();
    await page.locator('.settings-back').focus();
    await page.keyboard.press(chord);
    await page.waitForFunction(() => document.querySelector('#settings-panel').hidden, null, {
      timeout: 1500
    });
    assert.equal(await page.locator('#main').evaluate(el => el.inert), false);
    assert.equal(await page.locator('body').evaluate(el => el.classList.contains('settings-mode')), false);
    const surface = page.locator(target);
    await surface.waitFor({
      state: 'visible',
      timeout: 1500
    });
    assert.equal(await surface.evaluate(el => !!el.closest('[inert]')), false);
    if (action === 'new') {
      await page.locator('#new-task-prompt').fill('Visible shortcut draft');
      assert.equal(await page.getByRole('button', {
        name: 'Start task',
        exact: true
      }).isEnabled(), true);
      await page.getByRole('button', {
        name: 'Cancel new task',
        exact: true
      }).click();
      await open(page);
      await page.locator('.settings-back').focus();
      await page.keyboard.press(chord);
      assert.equal(await page.locator('#new-task-prompt').inputValue(), 'Visible shortcut draft');
      await page.getByRole('button', {
        name: 'Cancel new task',
        exact: true
      }).click();
    } else if (action === 'rename' || action === 'fork') {
      if (action === 'fork') await page.locator('#task-settings-status').filter({
        hasText: 'not confirmed'
      }).waitFor();
      await page.locator('#task-field-task-name').fill('Usable rename');
      await page.getByRole('button', {
        name: 'Back to task',
        exact: true
      }).click();
    } else if (action === 'archive' || action === 'delete') {
      await surface.getByRole('button', {
        name: 'Cancel',
        exact: true
      }).click();
    } else if (action === 'filter') {
      await surface.fill('Task A');
      assert.equal(await page.locator('.task-row').count(), 1);
    } else if (action === 'split') {
      await surface.focus();
      assert.equal(await surface.isEnabled(), true);
    } else if (action === 'files' || action === 'info') {
      const tab = page.locator(action === 'files' ? '#tool-changes' : '#tool-status');
      await tab.click();
      assert.equal(await tab.getAttribute('aria-selected'), 'true');
    } else {
      const tab = page.locator(action === 'review' ? '#tab-review' : '#tab-agent');
      await tab.click();
      assert.equal(await tab.getAttribute('aria-selected'), 'true');
    }
    assert.equal(writes.some(write => write.path === '/api/settings'), false);
  });
}
for (const [action, chord] of surfaceShortcuts) {
  test(`Settings shortcut ${action} refuses to leave during a save and works after it finishes`, async () => {
    const {
      page,
      state
    } = await fixture();
    let release, reached;
    const held = new Promise(resolve => {
        reached = resolve;
      }),
      gate = new Promise(resolve => {
        release = resolve;
      });
    cleanup.push(() => release());
    await page.route('**/api/settings', async route => {
      if (route.request().method() === 'PUT') {
        reached();
        await gate;
        state.settings.kb.metis_vault_path = '/tmp/saved-shortcut';
      }
      await route.fulfill({
        json: state.settings
      });
    });
    await open(page);
    await page.getByLabel('Vault path', {
      exact: true
    }).fill('/tmp/saved-shortcut');
    await page.getByRole('button', {
      name: 'Save General',
      exact: true
    }).click();
    await held;
    {
      await page.locator('#settings-panel h1').click();
      await page.keyboard.press(chord);
      await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
      assert.equal(await page.locator('#settings-panel').isVisible(), true, chord);
      assert.equal(await page.locator('#settings-panel').evaluate(el => !!el.closest('[inert]')), false, chord);
      assert.equal(await page.locator('#new-task-view').isVisible(), false, chord);
      assert.equal(await page.locator('#task-settings-panel').isVisible(), false, chord);
      assert.equal(await page.locator('dialog[open]').count(), 0, chord);
    }
    release();
    await page.locator('#settings-status').filter({
      hasText: 'Saved General'
    }).waitFor();
    await page.locator('.settings-back').focus();
    await page.keyboard.press('Alt+Shift+N');
    await page.locator('#new-task-prompt').fill('After saved');
    assert.equal(state.settings.kb.metis_vault_path, '/tmp/saved-shortcut');
  });
}
test('unsaved Settings edits follow Back without an implicit save on shortcut departure', async () => {
  const {
    page,
    state,
    writes
  } = await fixture();
  await open(page);
  await page.getByLabel('Vault path', {
    exact: true
  }).fill('/tmp/unsaved-shortcut');
  await page.locator('.settings-back').focus();
  await page.keyboard.press('Alt+Shift+N');
  await page.waitForFunction(() => document.querySelector('#settings-panel').hidden, null, {
    timeout: 1500
  });
  await page.locator('#new-task-prompt').fill('After unsaved edits');
  assert.equal(state.settings.kb.metis_vault_path, '/tmp/vault');
  assert.equal(writes.some(write => write.path === '/api/settings'), false);
});
test('New Task shortcut also releases the task-settings boundary', async () => {
  const {
    page
  } = await fixture();
  await page.route('**/api/tasks/a/raw', route => route.fulfill({
    json: {
      id: 'a',
      name: 'Task A',
      project: 'demo',
      status: 'complete'
    }
  }));
  await open(page);
  await page.locator('.settings-back').focus();
  await page.keyboard.press('Alt+Shift+R');
  await page.locator('#task-field-task-name').waitFor();
  await page.getByRole('button', {
    name: 'Back to task',
    exact: true
  }).focus();
  await page.keyboard.press('Alt+Shift+N');
  await page.locator('#new-task-prompt').fill('Creation from task settings');
  assert.equal(await page.locator('#task-settings-panel').isVisible(), false);
  assert.equal(await page.locator('#settings-panel').isVisible(), false);
  assert.equal(await page.locator('#new-task-view').evaluate(el => !!el.closest('[inert]')), false);
  await page.getByRole('button', {
    name: 'Cancel new task',
    exact: true
  }).click();
  assert.equal(await page.locator('#draft').evaluate(el => !!el.closest('[inert]')), false);
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
