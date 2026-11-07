import { test } from '@playwright/test';
import { resetServer } from '../../../_helpers';
import { refreshWorkspace } from '../../_workspace-controls-fixture.ts';
import assert from 'node:assert/strict';
let browser, panel;
const cleanup = [];
async function fixture() {
  const page = await browser.newPage({
    viewport: {
      width: 1440,
      height: 900
    }
  });
  cleanup.push(() => page.close());
  const errors = [],
    writes = [];
  page.on('pageerror', error => errors.push(error.message));
  cleanup.push(() => assert.deepEqual(errors, []));
  const state = {
    fail: false,
    tasks: ['a', 'b'].map(id => ({
      id,
      name: `Task ${id.toUpperCase()}`,
      status: 'complete',
      live: false,
      pinned: false,
      project: 'demo',
      backend: 'custom-claude',
      model: 'explicit-model',
      account: 'default',
      branch: `feature/${id}`,
      base_branch: 'main',
      worktree: `/tmp/${id}`,
      worktree_path: `/tmp/${id}`,
      created_at: '2026-11-01T00:00:00Z',
      sandbox_override: 'enabled'
    }))
  };
  await page.route('**/api/**', route => route.fulfill({
    status: 404,
    json: {}
  }));
  await page.route('**/api/tasks?archived=all', route => route.fulfill({
    json: {
      tasks: state.tasks
    }
  }));
  await page.route('**/api/hera', route => route.fulfill({
    json: {
      orchestrators: []
    }
  }));
  await page.route('**/api/projects/full', route => route.fulfill({
    json: {
      projects: [{
        name: 'demo',
        path: '/tmp/demo',
        branch: 'main',
        backend: 'custom-claude'
      }]
    }
  }));
  await page.route('**/api/backends', route => route.fulfill({
    json: {
      backends: [{
        name: 'custom-claude',
        kind: 'claude'
      }]
    }
  }));
  await page.route('**/api/accounts*', route => route.fulfill({
    json: {
      accounts: [{
        name: 'default',
        supports: {
          claude: true
        }
      }, {
        name: 'second',
        label: 'Second account',
        supports: {
          claude: true
        }
      }, {
        name: 'codex-only',
        supports: {
          codex: true
        }
      }]
    }
  }));
  await page.route(/\/api\/tasks\/[^/]+(?:\/raw)?$/, route => {
    const path = new URL(route.request().url()).pathname,
      id = path.split('/')[3];
    if (route.request().method() === 'DELETE') {
      writes.push({
        path,
        method: 'DELETE'
      });
      state.tasks = state.tasks.filter(task => task.id !== id);
      return route.fulfill({
        json: {
          status: 'deleted'
        }
      });
    }
    return route.fulfill({
      json: state.tasks.find(task => task.id === id) || {}
    });
  });
  await page.route('**/api/tasks/*/meta?namespace=pr', route => route.fulfill({
    json: {
      entries: [{
        namespace: 'pr',
        key: 'url',
        value: 'https://github.com/example/repo/pull/12'
      }]
    }
  }));
  for (const operation of ['rename', 'status', 'stop', 'restart', 'resume', 'account', 'fork', 'archive', 'unarchive']) await page.route(`**/api/tasks/*/${operation}`, route => {
    const path = new URL(route.request().url()).pathname,
      id = path.split('/')[3];
    const body = route.request().postData() ? route.request().postDataJSON() : null;
    writes.push({
      path,
      method: route.request().method(),
      body
    });
    if (state.fail) return route.fulfill({
      status: 409,
      json: {}
    });
    state.tasks = state.tasks.map(task => task.id === id ? {
      ...task,
      ...(operation === 'rename' ? {
        name: body.name
      } : operation === 'status' ? {
        status: body.status
      } : operation === 'account' ? {
        account: body.account
      } : operation === 'archive' ? {
        archived: true
      } : operation === 'unarchive' ? {
        archived: false
      } : {})
    } : task);
    if (operation === 'fork') {
      const task = {
        ...state.tasks.find(task => task.id === id),
        id: 'forked',
        name: 'Forked task'
      };
      state.tasks.push(task);
      return route.fulfill({
        status: 201,
        json: task
      });
    }
    return route.fulfill({
      json: {}
    });
  });
  await page.goto(process.env.WORKSPACE_TEST_URL || 'http://127.0.0.1:17750');
  await page.locator('#token').fill('fixture-token');
  await page.locator('#connect-button').click();
  await page.locator('.task-row').first().waitFor();
  return {
    page,
    state,
    writes
  };
}
async function openTask(page, name = 'Task B') {
  const control = page.getByRole('button', {
    name: `Task actions for ${name}`,
    exact: true
  });
  assert.equal(await control.count(), 1);
  await control.click();
  await page.getByRole('menuitem', {
    name: 'Task settings',
    exact: true
  }).click();
  await page.locator('#task-settings-panel').getByLabel('Task name', {
    exact: true
  }).waitFor();
}
test.beforeAll(async ({
  browser: workerBrowser
}) => {
  browser = workerBrowser;
  await resetServer();
  panel = page => page.locator('#task-settings-panel');
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
test('project Configuration uses the existing project editor and route', async () => {
  const {
    page,
    writes
  } = await fixture();
  await page.getByRole('button', {
    name: 'Configuration for demo',
    exact: true
  }).click();
  assert.equal(await page.locator('.project-menu').count(), 0);
  await page.getByLabel('Repository path', {
    exact: true
  }).waitFor();
  assert.equal(await page.getByLabel('Project name', {
    exact: true
  }).inputValue(), 'demo');
  assert.equal(new URL(page.url()).searchParams.get('settings'), 'repository:demo');
  assert.equal(await page.locator('#task-title').textContent(), 'Task A');
  assert.deepEqual(writes, []);
});
test('task hover menu is left of archive; settings capture that task without selecting it and support history', async () => {
  const {
    page,
    writes
  } = await fixture();
  const more = page.getByRole('button', {
    name: 'Task actions for Task B',
    exact: true
  });
  assert.equal(await more.count(), 1);
  await page.locator('#search').focus();
  await page.locator('#task-title').hover();
  assert.equal(await more.evaluate(item => getComputedStyle(item).opacity), '0');
  await page.locator('.task-row[data-task-id="b"]').hover();
  const moreBox = await more.boundingBox(),
    archiveBox = await page.getByRole('button', {
      name: 'Archive Task B',
      exact: true
    }).boundingBox();
  assert.ok(moreBox.x < archiveBox.x);
  await openTask(page);
  assert.equal(await panel(page).getByLabel('Task name', {
    exact: true
  }).inputValue(), 'Task B');
  assert.doesNotMatch(await panel(page).textContent(), /explicit-model|feature\/b/);
  assert.match(await panel(page).textContent(), /Sandbox override/);
  assert.equal(await panel(page).getByRole('button', {
    name: 'Restart agent',
    exact: true
  }).count(), 0);
  assert.equal(await panel(page).getByRole('link', {
    name: 'Open pull request',
    exact: true
  }).getAttribute('href'), 'https://github.com/example/repo/pull/12');
  assert.equal(new URL(page.url()).searchParams.get('task_settings'), 'b');
  assert.equal(await page.locator('.task-row[aria-current="page"]').getAttribute('data-task-id'), 'a');
  await page.keyboard.press('Escape');
  await page.waitForFunction(() => document.querySelector('#task-settings-panel').hidden);
  assert.equal(await page.locator('#task-title').textContent(), 'Task A');
  await page.goForward();
  await panel(page).getByLabel('Task name', {
    exact: true
  }).waitFor();
  assert.deepEqual(writes, []);
});
test('rename and workflow changes use captured targeted endpoints and retain failures', async () => {
  const {
    page,
    state,
    writes
  } = await fixture();
  await openTask(page);
  await panel(page).getByLabel('Task name', {
    exact: true
  }).fill('Renamed B');
  state.fail = true;
  await panel(page).getByRole('button', {
    name: 'Save name',
    exact: true
  }).click();
  await page.locator('#task-settings-status').filter({
    hasText: 'not confirmed'
  }).waitFor();
  assert.equal(await panel(page).getByLabel('Task name', {
    exact: true
  }).inputValue(), 'Renamed B');
  state.fail = false;
  await panel(page).getByRole('button', {
    name: 'Save name',
    exact: true
  }).click();
  await page.waitForFunction(() => document.querySelector('#task-settings-status').textContent.includes('Saved'));
  await panel(page).getByLabel('Workflow status', {
    exact: true
  }).selectOption('in_review');
  await panel(page).getByRole('button', {
    name: 'Save status',
    exact: true
  }).click();
  await page.waitForFunction(() => document.querySelector('#task-settings-status').textContent.includes('Saved'));
  assert.deepEqual(writes.map(write => [write.path, write.body]), [['/api/tasks/b/rename', {
    name: 'Renamed B'
  }], ['/api/tasks/b/rename', {
    name: 'Renamed B'
  }], ['/api/tasks/b/status', {
    status: 'in_review'
  }]]);
  assert.equal(writes.some(write => write.path.endsWith('/raw')), false);
});
test('session, account and deletion actions confirm their captured task; fork selects its new task', async () => {
  const {
    page,
    writes
  } = await fixture();
  await openTask(page);
  for (const label of ['Stop agent', 'Resume agent']) {
    const count = writes.length;
    await panel(page).getByRole('button', {
      name: label,
      exact: true
    }).click();
    assert.equal(writes.length, count);
    await page.locator('#task-action-dialog').getByRole('button', {
      name: 'Cancel',
      exact: true
    }).click();
    assert.equal(writes.length, count);
    await panel(page).getByRole('button', {
      name: label,
      exact: true
    }).click();
    await page.locator('#task-action-dialog').getByRole('button', {
      name: 'Confirm',
      exact: true
    }).click();
    await page.waitForFunction(count => document.querySelector('#task-action-dialog') === null && !document.querySelector('#task-settings-panel fieldset').disabled, count);
  }
  assert.equal(await panel(page).getByLabel('Move to account', {
    exact: true
  }).locator('option[value="codex-only"]').count(), 0);
  await panel(page).getByLabel('Move to account', {
    exact: true
  }).selectOption('second');
  await panel(page).getByRole('button', {
    name: 'Move account',
    exact: true
  }).click();
  await page.locator('#task-action-dialog').getByRole('button', {
    name: 'Confirm',
    exact: true
  }).click();
  await page.waitForFunction(() => !document.querySelector('#task-settings-panel fieldset').disabled);
  assert.deepEqual(writes.find(write => write.path.endsWith('/account')).body, {
    account: 'second',
    force: false
  });
  await panel(page).getByRole('button', {
    name: 'Fork task',
    exact: true
  }).click();
  await page.waitForFunction(() => document.querySelector('#task-title').textContent === 'Forked task');
  await openTask(page);
  await panel(page).getByRole('button', {
    name: 'Delete task',
    exact: true
  }).click();
  assert.match(await page.locator('#task-action-dialog').textContent(), /remote branch/);
  await page.locator('#task-action-dialog').getByRole('button', {
    name: 'Confirm',
    exact: true
  }).click();
  await page.waitForFunction(() => document.querySelector('#task-settings-panel').hidden);
  assert.equal(writes.at(-1).method, 'DELETE');
  assert.equal(writes.at(-1).path, '/api/tasks/b');
  assert.equal(await page.locator('#task-title').textContent(), 'Forked task');
});
test('copy actions use available captured fields and menu focus survives refresh', async () => {
  const {
    page,
    writes
  } = await fixture();
  await page.evaluate(() => {
    window.copied = [];
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: {
        writeText: async value => window.copied.push(value)
      }
    });
  });
  for (const label of ['Copy task ID', 'Copy branch', 'Copy worktree path']) {
    await page.getByRole('button', {
      name: 'Task actions for Task B',
      exact: true
    }).click();
    if (label === 'Copy task ID') {
      await page.getByRole('menuitem', {
        name: label,
        exact: true
      }).focus();
      await refreshWorkspace(page);
      assert.equal(await page.getByRole('menuitem', {
        name: label,
        exact: true
      }).evaluate(item => item === document.activeElement), true);
    }
    await page.getByRole('menuitem', {
      name: label,
      exact: true
    }).click();
  }
  assert.deepEqual(await page.evaluate(() => window.copied), ['b', 'feature/b', '/tmp/b']);
  assert.deepEqual(writes, []);
});
test('closed or superseded task settings ignore late reads and canceled confirmations send nothing', async () => {
  const {
    page,
    writes
  } = await fixture();
  let release, reached, delivered;
  const held = new Promise(resolve => {
      release = resolve;
    }),
    requested = new Promise(resolve => {
      reached = resolve;
    }),
    completed = new Promise(resolve => {
      delivered = resolve;
    });
  await page.route('**/api/tasks/b/raw', async route => {
    reached();
    await held;
    await route.fulfill({
      json: {
        id: 'b',
        name: 'Late B',
        project: 'demo',
        status: 'complete'
      }
    }).catch(() => {});
    delivered();
  });
  await page.getByRole('button', {
    name: 'Task actions for Task B',
    exact: true
  }).click();
  await page.getByRole('menuitem', {
    name: 'Task settings',
    exact: true
  }).click();
  await requested;
  await openTask(page, 'Task A');
  release();
  await completed;
  await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
  assert.equal(await panel(page).getByLabel('Task name', {
    exact: true
  }).inputValue(), 'Task A');
  await panel(page).getByRole('button', {
    name: 'Stop agent',
    exact: true
  }).click();
  await page.keyboard.press('Escape');
  await page.locator('#task-action-dialog').waitFor({
    state: 'detached'
  });
  assert.equal(await page.locator('#task-action-dialog').count(), 0);
  assert.deepEqual(writes, []);
  await page.locator('#disconnect').click();
  assert.equal(await panel(page).isVisible(), false);
  assert.equal(await panel(page).getByLabel('Task name', {
    exact: true
  }).count(), 0);
});
test('session failures stay inline and never retry a busy agent with force', async () => {
  const {
    page,
    state,
    writes
  } = await fixture();
  await openTask(page);
  state.fail = true;
  await panel(page).getByRole('button', {
    name: 'Resume agent',
    exact: true
  }).click();
  await page.locator('#task-action-dialog').getByRole('button', {
    name: 'Confirm',
    exact: true
  }).click();
  await page.locator('#task-settings-status').filter({
    hasText: 'not confirmed'
  }).waitFor();
  assert.equal(await panel(page).isVisible(), true);
  assert.equal(writes.length, 1);
  assert.equal(writes[0].path, '/api/tasks/b/resume');
  assert.equal(writes[0].body, null);
});
test('deleting the main task clears its viewer even when the following task refresh fails', async () => {
  const {
    page
  } = await fixture();
  await openTask(page, 'Task A');
  await page.route('**/api/tasks?archived=all', route => route.fulfill({
    status: 503,
    json: {}
  }));
  await panel(page).getByRole('button', {
    name: 'Delete task',
    exact: true
  }).click();
  await page.locator('#task-action-dialog').getByRole('button', {
    name: 'Confirm',
    exact: true
  }).click();
  await page.waitForFunction(() => document.querySelector('#task-settings-panel').hidden && document.querySelector('#task-title').textContent !== 'Task A');
  assert.equal(await page.locator('.xterm').count(), 0);
  assert.equal(await page.locator('.task-row[data-task-id="a"]').count(), 0);
});
test('archive inside task settings refreshes its restore action and Back retains the main task and filter', async () => {
  const {
    page
  } = await fixture();
  await openTask(page, 'Task A');
  await panel(page).getByRole('button', {
    name: 'Archive task',
    exact: true
  }).click();
  await page.locator('#archive-dialog .primary').click();
  await panel(page).getByRole('button', {
    name: 'Restore task',
    exact: true
  }).waitFor();
  assert.equal(await page.locator('.task-row[data-task-id="a"]').count(), 0);
  await page.keyboard.press('Escape');
  await page.waitForFunction(() => document.querySelector('#task-settings-panel').hidden);
  assert.equal(await page.locator('#active-filter').getAttribute('aria-pressed'), 'true');
  assert.equal(await page.locator('#task-title').textContent(), 'Task A');
  assert.equal(await page.locator('#archive-task').count(), 0);
  assert.equal(await page.locator('.task-row[aria-current="page"]').count(), 0);
});
test('task menu keyboard dismissal and rail archive/restore retain the unrelated main task', async () => {
  const {
    page,
    writes
  } = await fixture();
  const more = () => page.getByRole('button', {
    name: 'Task actions for Task B',
    exact: true
  });
  await more().focus();
  await page.keyboard.press('ArrowDown');
  assert.equal(await page.getByRole('menuitem', {
    name: 'Task settings',
    exact: true
  }).evaluate(item => item === document.activeElement), true);
  await page.keyboard.press('End');
  await page.keyboard.press('Escape');
  assert.equal(await more().evaluate(item => item === document.activeElement), true);
  for (const operation of ['Archive', 'Restore']) {
    await more().click();
    assert.equal(await page.getByRole('menuitem', {
      name: operation,
      exact: true
    }).count(), 0);
    await page.keyboard.press('Escape');
    await page.getByRole('button', {
      name: operation + ' Task B',
      exact: true
    }).click();
    assert.equal(writes.length, operation === 'Archive' ? 0 : 1);
    await page.locator('#archive-dialog .primary').click();
    await page.locator('.task-row[data-task-id="b"]').waitFor({
      state: 'detached'
    });
    assert.equal(await page.locator('#task-title').textContent(), 'Task A');
    if (operation === 'Archive') await page.locator('#archive-filter').click();
  }
  assert.deepEqual(writes.map(write => write.path), ['/api/tasks/b/archive', '/api/tasks/b/unarchive']);
});
test('leaving during a pending rename fences the late result and keeps the main task', async () => {
  const {
    page,
    state
  } = await fixture();
  await openTask(page);
  let release, reached, delivered;
  const hold = new Promise(resolve => {
      release = resolve;
    }),
    requested = new Promise(resolve => {
      reached = resolve;
    }),
    completed = new Promise(resolve => {
      delivered = resolve;
    });
  await page.route('**/api/tasks/b/rename', async route => {
    reached();
    await hold;
    state.tasks = state.tasks.map(task => task.id === 'b' ? {
      ...task,
      name: 'Late rename'
    } : task);
    await route.fulfill({
      json: {
        name: 'Late rename'
      }
    }).catch(() => {});
    delivered();
  });
  await panel(page).getByLabel('Task name', {
    exact: true
  }).fill('Late rename');
  await panel(page).getByRole('button', {
    name: 'Save name',
    exact: true
  }).click();
  await requested;
  await panel(page).getByRole('button', {
    name: 'Back to task',
    exact: true
  }).click();
  await page.waitForFunction(() => document.querySelector('#task-settings-panel').hidden);
  release();
  await completed;
  await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
  assert.equal(await page.locator('#task-title').textContent(), 'Task A');
  assert.equal(await panel(page).isVisible(), false);
});
test('a superseded retry cannot replace edits from a newer task read', async () => {
  const {
    page,
    state
  } = await fixture();
  let calls = 0,
    release,
    reached,
    delivered,
    lateRequest;
  const hold = new Promise(resolve => {
      release = resolve;
    }),
    requested = new Promise(resolve => {
      reached = resolve;
    }),
    completed = new Promise(resolve => {
      delivered = resolve;
    });
  await page.route('**/api/tasks/b/raw', async route => {
    calls++;
    if (calls === 1) return route.fulfill({
      status: 503,
      json: {}
    });
    if (calls === 2) {
      lateRequest = route.request();
      reached();
      await hold;
      await route.fulfill({
        json: {
          ...state.tasks[1],
          name: 'Older response'
        }
      }).catch(() => {});
      delivered();
      return;
    }
    return route.fulfill({
      json: state.tasks[1]
    });
  });
  await page.getByRole('button', {
    name: 'Task actions for Task B',
    exact: true
  }).click();
  await page.getByRole('menuitem', {
    name: 'Task settings',
    exact: true
  }).click();
  const retry = page.getByRole('button', {
    name: 'Retry task settings',
    exact: true
  });
  await retry.click();
  await requested;
  await retry.click();
  await panel(page).getByLabel('Task name', {
    exact: true
  }).fill('Typed after newer read');
  release();
  await completed;
  const response = await lateRequest.response();
  if (response) await response.finished();
  await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
  assert.equal(await panel(page).getByLabel('Task name', {
    exact: true
  }).inputValue(), 'Typed after newer read');
});
test('successful session changes retain a subsequent details-read failure instead of reporting a loaded view', async () => {
  const {
    page,
    writes
  } = await fixture();
  await openTask(page);
  await page.route('**/api/tasks/b/raw', route => route.fulfill({
    status: 503,
    json: {}
  }));
  await panel(page).getByRole('button', {
    name: 'Stop agent',
    exact: true
  }).click();
  await page.locator('#task-action-dialog').getByRole('button', {
    name: 'Confirm',
    exact: true
  }).click();
  await panel(page).getByRole('button', {
    name: 'Retry task settings',
    exact: true
  }).waitFor();
  assert.match(await page.locator('#task-settings-status').textContent(), /unavailable/);
  assert.deepEqual(writes.map(write => write.path), ['/api/tasks/b/stop']);
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
