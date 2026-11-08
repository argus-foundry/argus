import { test } from '@playwright/test';
import { resetServer } from '../../../_helpers';
import { refreshWorkspace } from '../../_workspace-controls-fixture.ts';
import { mkdir } from 'node:fs/promises';
import assert from 'node:assert/strict';
let browser, frontend, tasks, roster;
const cleanup = [];
async function until(check) {
  const deadline = Date.now() + 3000;
  while (Date.now() < deadline) {
    if (check()) return;
    await new Promise(resolve => setTimeout(resolve, 25));
  }
  throw new Error('Expected fixture mutation was not received');
}
async function fixture({
  blocked = false,
  storedGrouping = false
} = {}) {
  const page = await browser.newPage({
    viewport: {
      width: 1440,
      height: 900
    }
  });
  page.setDefaultTimeout(8000);
  cleanup.push(() => page.close());
  roster=structuredClone(roster);for(const orch of roster.orchestrators)orch.pinned=false;
  const writes = [],
    errors = [],
    state = {
      tasks: tasks.map(task => ({
        ...task
      }))
    };
  page.on('pageerror', error => errors.push(error.message));
  cleanup.push(() => assert.deepEqual(errors, []));
  if (storedGrouping) await page.addInitScript(() => localStorage.setItem('argus-workspace:rail-grouping', JSON.stringify('orchestration')));
  if (blocked) await page.addInitScript(() => Object.defineProperty(window, 'localStorage', {
    get() {
      throw new Error('Blocked');
    }
  }));
  await page.route('**/api/**', route => {
    const request = route.request(),
      path = new URL(request.url()).pathname;
    let value = {};
    if (request.method() !== 'GET') {
      writes.push({
        path,
        body: request.postData()
      });
      if(path.startsWith('/api/hera/orchestrators/')){const orch=roster.orchestrators.find(orch=>orch.id===Number(path.split('/')[4]));orch.pinned=path.endsWith('/pin');return route.fulfill({json:{}});}
      const id = path.split('/')[3],
        operation = path.split('/')[4],
        body = request.postData() ? request.postDataJSON() : {};
      state.tasks = state.tasks.map(task => task.id !== id ? task : operation === 'archive' ? {
        ...task,
        archived: true,
        pinned: false
      } : operation === 'unarchive' ? {
        ...task,
        archived: false
      } : operation === 'status' ? {
        ...task,
        status: body.status
      } : operation === 'raw' ? {
        ...task,
        pinned: body.pinned
      } : task);
      return route.fulfill({
        json: {}
      });
    }
    if (path === '/api/tasks') value = {
      tasks: state.tasks
    };else if (path === '/api/hera') value = roster;else if (path === '/api/projects' || path === '/api/projects/full') value = {
      projects: [{
        name: 'fixture'
      }]
    };else if (path === '/api/backends') value = {
      backends: [{
        name: 'fixture',
        kind: 'custom',
        session_switcher: false
      }]
    };else if (path === '/api/accounts') value = [];else if (path.endsWith('/size')) value = {
      cols: 80,
      rows: 24
    };else {
      const id = path.split('/')[3];
      if (state.tasks.some(task => task.id === id)) value = {
        ...state.tasks.find(task => task.id === id),
        worktree: '/tmp/fixture'
      };
    }
    if (path.endsWith('/output')) return route.fulfill({
      body: 'retained output\r\n',
      headers: {
        'X-Output-Total': '17'
      }
    });
    if (path.endsWith('/stream')) return route.fulfill({
      contentType: 'text/event-stream',
      body: ':fixture\n\n'
    });
    return route.fulfill({
      json: value
    });
  });
  await page.goto(frontend);
  await page.locator('#token').fill('fixture');
  await page.locator('#connect-button').click();
  await page.locator('.task-row[data-task-id="c"]').first().waitFor();
  return {
    page,
    writes,
    state
  };
}
test.beforeAll(async ({
  browser: workerBrowser
}) => {
  browser = workerBrowser;
  await resetServer();
  await mkdir('.context', {
    recursive: true
  });
  frontend = process.env.WORKSPACE_TEST_URL || 'http://127.0.0.1:18750';
  tasks = [{
    id: 'c',
    name: 'Coordinator',
    project: 'fixture'
  }, {
    id: 'w',
    name: 'Worker',
    project: 'fixture',
    needs_input: true
  }, {
    id: 'n',
    name: 'Nested worker',
    project: 'fixture'
  }, {
    id: 'f',
    name: 'Freelance task',
    project: 'fixture'
  }, {
    id: 'p',
    name: 'Plain task',
    project: 'fixture'
  }].map(task => ({
    ...task,
    status: 'complete',
    live: false,
    backend: 'fixture'
  }));
  roster = {
    freelance: [{
      role_id: 6,
      orch_id: 1,
      name: 'freelance',
      kind: 'freelance',
      live: true,
      task_id: 'f',
      status: 'working',
      ready_to_close: true
    }],
    orchestrators: [{
      id: 1,
      name: 'Root orchestration',
      kanban_status: 'active',
      roles: [{
        role_id: 1,
        kind: 'coordinator',
        name: 'coord',
        live: true,
        task_id: 'c',
        status: 'working'
      }, {
        role_id: 2,
        kind: 'worker',
        name: 'worker',
        live: true,
        task_id: 'w',
        status: 'blocked'
      }]
    }, {
      id: 2,
      name: 'Nested orchestration',
      kanban_status: 'backlog',
      bridge_parent_orch_id: 1,
      bridge_parent_role_id: 2,
      roles: [{
        role_id: 3,
        kind: 'coordinator',
        name: 'nested',
        live: true,
        task_id: 'w',
        status: 'idle'
      }, {
        role_id: 4,
        kind: 'worker',
        name: 'nested-worker',
        live: true,
        task_id: 'n',
        status: 'done',
        ready_to_close: true
      }, {
        role_id: 5,
        name: 'unbound',
        kind: 'worker',
        live: false,
        status: 'idle'
      }]
    }]
  };
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
test('project rail keeps compact rows, role tags, actions and coordinator selection independent of split', async () => {
  const {
    page
  } = await fixture();
  assert.equal(await page.locator('#mode-hera').count(), 0);
  assert.equal(await page.locator('.task-row[data-task-id="w"]').count(), 1);
  assert.equal(await page.locator('.task-row[data-task-id="p"]').count(), 1);
  assert.equal(await page.locator('.task-row[data-task-id="n"] .task-role-tag').textContent(), 'ready');
  assert.equal(await page.locator('.task-row[data-task-id="p"] .task-role-tag').count(), 0);
  assert.equal(await page.locator('.task-row[data-task-id="f"] .task-role-tag').textContent(), 'ready');
  const rows = await page.locator('#task-groups .task-row').evaluateAll(rows => rows.map(row => ({
    height: row.getBoundingClientRect().height,
    whiteSpace: getComputedStyle(row.querySelector('.task-name')).whiteSpace
  })));
  assert.equal(rows.length, 5);
  for (const row of rows) {
    assert.ok(row.height <= 36);
    assert.equal(row.whiteSpace, 'nowrap');
  }
  assert.equal(await page.locator('.task-row[data-task-id="c"] [data-status="attention"]').count(), 1);
  await page.locator('.task-row[data-task-id="c"]').click();
  assert.equal(await page.locator('#split-toggle').getAttribute('aria-pressed'), 'false');
  assert.equal(await page.locator('#secondary-pane').isVisible(), false);
  await page.locator('#search').fill('Nested worker');
  assert.equal(await page.locator('.task-row[data-task-id="n"]').count(), 1);
  await page.locator('#search').fill('');
  await page.screenshot({
    path: '.context/unified-project-rail.png'
  });
  await page.reload();
  await page.locator('.project-group').waitFor();
  assert.equal(await page.locator('#task-groups .task-row').count(), 5);
});
test('explicit split picks any other task, uses a coordinator worker and retains the right terminal on close', async () => {
  const {
    page,
    writes
  } = await fixture();
  await page.locator('.task-row[data-task-id="c"]').click();
  await page.locator('#split-toggle').click();
  await page.locator('#secondary-pane .xterm').waitFor();
  assert.equal(await page.locator('#secondary-task-picker').inputValue(), 'w');
  assert.equal(await page.locator('#split-roster').count(), 1);
  await page.locator('#split-roster').evaluate(node => {
    node.open = true;
  });
  await page.screenshot({
    path: '.context/unified-explicit-split.png'
  });
  await page.locator('#secondary-task-picker').selectOption('p');
  await page.locator('#secondary-pane .xterm').waitFor();
  await page.evaluate(() => window.rightTerminal = document.querySelector('#secondary-pane .xterm'));
  await page.locator('#split-toggle').click();
  assert.equal(await page.locator('#secondary-pane').isVisible(), false);
  await page.locator('#split-toggle').click();
  await page.waitForFunction(() => document.querySelector('#secondary-pane .xterm') === window.rightTerminal);
  await page.locator('.task-row[data-task-id="p"]').first().click();
  assert.equal(await page.locator('#secondary-task-picker').inputValue(), '');
  assert.equal(await page.locator('#split-roster').count(), 0);
  await page.locator('#secondary-task-picker').selectOption('c');
  await page.locator('#secondary-pane .xterm').waitFor();
  await page.reload();
  await page.locator('#split-toggle').waitFor();
  assert.equal(await page.locator('#split-toggle').getAttribute('aria-pressed'), 'true');
  assert.equal(await page.locator('.project-group').count(), 1);
  assert.deepEqual(writes, []);
});
test('blocked preference storage keeps the project rail and split off', async () => {
  const {
    page
  } = await fixture({
    blocked: true
  });
  assert.equal(await page.locator('.project-group').count(), 1);
  assert.equal(await page.locator('#split-toggle').getAttribute('aria-pressed'), 'false');
  await page.locator('#shortcuts-help').click();
  assert.match(await page.locator('#shortcuts-dialog').textContent(), /Toggle split view/);
  await page.keyboard.press('Escape');
  await page.keyboard.press('Alt+Shift+D');
  assert.equal(await page.locator('#split-toggle').getAttribute('aria-pressed'), 'true');
});
test('captured row actions, archive and pin preserve the same main task in the project rail', async () => {
  const {
    page,
    writes,
    state
  } = await fixture();
  await page.locator('.task-row[data-task-id="c"]').first().click();
  const menu = () => page.getByRole('button', {
    name: 'Task actions for Worker',
    exact: true
  });
  await menu().click();
  assert.equal(await page.getByRole('menuitem', {name:/Move to (Active|Backlog|Blocked|Done)/}).count(),0);
  assert.equal(await page.getByRole('menuitem', {
    name: 'Revert status',
    exact: true
  }).count(), 0);
  assert.equal(await page.getByRole('menuitem', {
    name: 'Advance status',
    exact: true
  }).count(), 0);
  await page.getByRole('menuitem', {
    name: 'Task settings',
    exact: true
  }).click();
  await page.getByLabel('Workflow status', {
    exact: true
  }).selectOption('in_review');
  await page.getByRole('button', {
    name: 'Save status',
    exact: true
  }).click();
  await page.locator('#task-settings-status').filter({
    hasText: 'Saved task.'
  }).waitFor();
  await page.getByRole('button', {
    name: 'Back to task',
    exact: true
  }).click();
  await until(() => state.tasks.find(task => task.id === 'w').status === 'in_review');
  assert.equal(state.tasks.find(task => task.id === 'w').status, 'in_review');
  assert.equal(await page.locator('#task-title').textContent(), 'Coordinator');
  const pins = writes.filter(write => write.path.endsWith('/pin')).length;
  await menu().click();
  await page.getByRole('menuitem', {
    name: 'Pin',
    exact: true
  }).click();
  await until(() => writes.filter(write => write.path.endsWith('/pin')).length === pins + 1);
  await page.waitForFunction(() => /Task (un)?pinned/.test(document.querySelector('#notice').textContent));
  assert.equal(writes.filter(write => write.path.endsWith('/pin')).at(-1).path, '/api/hera/orchestrators/2/pin');
  assert.equal(roster.orchestrators.find(orch => orch.id === 2).pinned, true);
  await page.locator('.task-row[data-task-id="w"] .task-pin').waitFor();
  await page.locator('.task-archive[data-task-id="w"]').click();
  await page.locator('#archive-dialog').getByRole('button', {
    name: 'Archive task',
    exact: true
  }).click();
  await page.locator('.task-row[data-task-id="w"]').waitFor({
    state: 'detached'
  });
  assert.equal(await page.locator('#task-title').textContent(), 'Coordinator');
  await page.locator('#archive-filter').click();
  await page.locator('.task-archive[data-task-id="w"]').click();
  await page.locator('#archive-dialog').getByRole('button', {
    name: 'Restore task',
    exact: true
  }).click();
  await page.locator('.task-row[data-task-id="w"]').waitFor({
    state: 'detached'
  });
  await page.locator('#active-filter').click();
  assert.equal(await page.locator('.task-row[data-task-id="c"]').getAttribute('aria-current'), 'page');
  state.tasks = state.tasks.map(task => task.id === 'w' ? {
    ...task,
    status: 'complete'
  } : task);
  await refreshWorkspace(page);
  assert.ok(writes.every(write => write.path.startsWith('/api/tasks/w/') || write.path === '/api/hera/orchestrators/2/pin'));
});
test('attention emphasis rolls up while collapsed and clears with the existing signal in both themes', async () => {
  const {
    page,
    state
  } = await fixture();
  for (const light of [false, true]) {
    await page.evaluate(light => {
      const tokens = light ? {
        '--surface': '#ffffff',
        '--raised': '#eee9e2',
        '--text': '#201c18',
        '--warning': '#805000',
        '--accent': '#805000',
        '--border': '#c3bbb2'
      } : {
        '--surface': '#1d1b19',
        '--raised': '#282522',
        '--text': '#efebe5',
        '--warning': '#e5bd72',
        '--accent': '#d9bca6',
        '--border': '#36322e'
      };
      for (const [key, value] of Object.entries(tokens)) document.documentElement.style.setProperty(key, value);
    }, light);
    const parent = page.locator('#task-groups .task-row[data-task-id="c"]'),
      worker = page.locator('#task-groups .task-row[data-task-id="w"]');
    assert.equal(await worker.evaluate(row => row.classList.contains('needs-attention')), true);
    assert.equal(await parent.evaluate(row => row.classList.contains('needs-attention')), true);
    await page.locator('.task-disclosure[data-parent-task-id="c"]').focus();
    await page.keyboard.press('Enter');
    assert.equal(await parent.evaluate(row => row.classList.contains('needs-attention')), true);
    await page.waitForFunction(() => {
      const name = document.querySelector('#task-groups .task-row[data-task-id="c"] .task-name');
      return name && Number(getComputedStyle(name).fontWeight) >= 600;
    });
    await page.locator('.project-header').click();
    assert.equal(await page.locator('.project-header').evaluate(header => header.classList.contains('needs-attention')), true);
    await page.locator('.project-header').click();
    await page.locator('.task-disclosure[data-parent-task-id="c"]').focus();
    await page.keyboard.press('Enter');
    await worker.click();
    assert.equal(await worker.getAttribute('aria-current'), 'page');
    assert.equal(await worker.evaluate(row => row.classList.contains('needs-attention')), true);
  }
  await page.locator('.project-header').click();
  assert.equal(await page.locator('.project-group').evaluate(group => group.open), false);
  state.tasks = state.tasks.map(task => ({
    ...task,
    needs_input: false
  }));
  for (const orch of roster.orchestrators) for (const role of orch.roles || []) {
    role.needs_input = false;
    if (role.status === 'blocked') role.status = 'idle';
  }
  await refreshWorkspace(page);
  await page.waitForFunction(() => document.querySelectorAll('#task-groups .task-row.needs-attention').length === 0);
  assert.equal(await page.locator('.project-header').evaluate(header => header.classList.contains('needs-attention')), false);
  assert.equal(await page.locator('.project-group').evaluate(group => group.classList.contains('needs-attention')), false);
  assert.ok(await page.locator('.project-name').evaluate(name => Number(getComputedStyle(name).fontWeight) < 700));
});
test('hoisted blocked freelancer keeps attention, collapsed project emphasis and next-attention selection', async () => {
  const { page, state } = await fixture();
  state.tasks = state.tasks.map(task => ({ ...task, needs_input: false }));
  for (const orch of roster.orchestrators) for (const role of orch.roles || []) role.status = 'idle';
  const freelancer = roster.freelance[0];
  freelancer.status = 'blocked';
  await refreshWorkspace(page);
  const row = page.locator('#task-groups .task-row[data-task-id="f"]');
  assert.equal(await row.locator('.task-role-tag').textContent(), 'blocked');
  assert.equal(await row.locator('.task-status-icon').getAttribute('data-status'), 'attention');
  assert.equal(await row.evaluate(row => row.classList.contains('needs-attention')), true);
  await page.locator('.task-row[data-task-id="p"]').click();
  await page.locator('.project-header').click();
  assert.equal(await page.locator('.project-group').evaluate(group => group.open), false);
  assert.equal(await page.locator('.project-header').evaluate(header => header.classList.contains('needs-attention')), true);
  await page.keyboard.press('Alt+Shift+J');
  await page.locator('#task-title').filter({ hasText: 'Freelance task' }).waitFor();
  for (const change of [{ status: 'idle' }, { status: 'blocked', archived: true }]) {
    Object.assign(freelancer, change);
    await refreshWorkspace(page);
    await page.waitForFunction(() => !document.querySelector('.project-header').classList.contains('needs-attention'));
    assert.equal(await row.locator('.task-status-icon').getAttribute('data-status'), 'merged');
    assert.equal(await row.evaluate(row => row.classList.contains('needs-attention')), false);
    await page.locator('.task-row[data-task-id="p"]').click();
    await page.keyboard.press('Alt+Shift+J');
    assert.equal(await page.locator('#task-title').textContent(), 'Plain task');
  }
});
test('project rail ignores unrelated grouping and has no grouping control or Hera filter', async () => {
  const {
    page
  } = await fixture({
    storedGrouping: true
  });
  assert.equal(await page.getByRole('group', {
    name: 'Task grouping',
    exact: true
  }).count(), 0);
  assert.equal(await page.locator('#show-hera-tasks').count(), 0);
  assert.equal(await page.locator('.project-group[data-project="fixture"] .task-row').count(), 5);
  await page.locator('#shortcuts-help').click();
  assert.doesNotMatch(await page.locator('#shortcuts-dialog').textContent(), /grouping|Hera tasks/i);
});
test('cross-project worker remains nested and searchable despite stored orchestration preference', async () => {
  const {
    page,
    state
  } = await fixture({
    storedGrouping: true
  });
  state.tasks = state.tasks.map(task => task.id === 'w' ? {
    ...task,
    project: 'other-project'
  } : task);
  await refreshWorkspace(page);
  assert.equal(await page.locator('.project-group[data-project="fixture"] .task-row[data-task-id="w"]').count(), 1);
  assert.equal(await page.locator('.task-row').count(), 5);
  await page.locator('#search').fill('other-project');
  assert.equal(await page.locator('.project-group[data-project="other-project"] .task-row[data-task-id="w"]').isVisible(), true);
});
test('failed captured pin preserves the unrelated main draft', async () => {
  const {
    page,
    writes
  } = await fixture();
  await page.locator('.task-row[data-task-id="c"]').click();
  await page.locator('#draft').fill('Unrelated main draft');
  await page.route('**/api/hera/orchestrators/2/pin', route => route.fulfill({status:503,json:{}}));
  await page.getByRole('button', {
    name: 'Task actions for Worker',
    exact: true
  }).click();
  await page.getByRole('menuitem', {
    name: 'Pin',
    exact: true
  }).click();
  await page.locator('#notice').filter({
    hasText: 'not confirmed'
  }).waitFor();
  assert.equal(await page.locator('#draft').inputValue(), 'Unrelated main draft');
  assert.equal(await page.locator('#task-title').textContent(), 'Coordinator');
  assert.equal(await page.locator('.task-row[data-task-id="c"]').getAttribute('aria-current'), 'page');
  assert.deepEqual(writes, []);
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
