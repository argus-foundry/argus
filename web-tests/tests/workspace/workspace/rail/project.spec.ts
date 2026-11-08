import { test } from '@playwright/test';
import { resetServer } from '../../../_helpers';
import { refreshWorkspace } from '../../_workspace-controls-fixture.ts';
import assert from 'node:assert/strict';
let browser, frontend, seed, group, create;
const cleanup = [];
async function fixture() {
  const page = await browser.newPage({
    viewport: {
      width: 1440,
      height: 900
    }
  });
  cleanup.push(() => page.close());
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  cleanup.push(() => assert.deepEqual(errors, []));
  let tasks = [seed];
  await page.route('**/api/**', route => route.fulfill({
    status: 404,
    json: {}
  }));
  await page.route('**/api/tasks?archived=all', route => route.fulfill({
    json: {
      tasks
    }
  }));
  await page.route('**/api/projects/full', route => route.fulfill({
    json: {
      projects: [{
        name: 'populated'
      }, {
        name: 'empty'
      }]
    }
  }));
  await page.route('**/api/hera', route => route.fulfill({
    json: {}
  }));
  await page.goto(frontend);
  await page.locator('#token').fill('fixture-token');
  await page.locator('#connect-button').click();
  await page.locator('.task-row').waitFor();
  return {
    page,
    setTasks: next => {
      tasks = next;
    }
  };
}
test.beforeAll(async ({
  browser: workerBrowser
}) => {
  browser = workerBrowser;
  await resetServer();
  frontend = process.env.WORKSPACE_TEST_URL || 'http://127.0.0.1:17750';
  seed = {
    id: 'existing',
    name: 'Existing task',
    project: 'populated',
    status: 'complete'
  };
  group = (page, name) => page.locator(`.project-group[data-project="${name}"]`);
  create = (page, name) => page.getByRole('button', {
    name: `New task in ${name}`,
    exact: true
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
test('empty project has no disclosure even after an expanded project loses its tasks', async () => {
  const {
    page,
    setTasks
  } = await fixture();
  const empty = group(page, 'empty');
  assert.equal(await empty.count(), 1);
  assert.equal(await empty.locator('summary').count(), 0);
  assert.equal(await empty.getAttribute('open'), null);
  assert.equal(await empty.locator('.project-name').textContent(), 'empty');
  assert.equal(await empty.locator('.project-avatar').textContent(), 'E');
  assert.equal(await create(page, 'empty').isVisible(), true);
  assert.equal(await page.locator('.project-count').count(), 0);
  const populated = group(page, 'populated');
  assert.equal(await populated.evaluate(item => item.open), true);
  setTasks([]);
  await refreshWorkspace(page);
  assert.equal(await populated.locator('summary').count(), 0);
  assert.equal(await populated.getAttribute('open'), null);
  assert.equal(await populated.locator('.task-row').count(), 0);
  await page.locator('#archive-filter').click();
  assert.equal(await page.locator('.project-group summary').count(), 0);
});
test('project plus preserves creation drafts while a direct Configuration gear opens settings', async () => {
  const {
    page
  } = await fixture();
  const populated = group(page, 'populated');
  await populated.locator('summary').click({
    position: {
      x: 5,
      y: 15
    }
  });
  assert.equal(await populated.evaluate(item => item.open), false);
  const more = page.getByRole('button', {
    name: 'Configuration for populated',
    exact: true
  });
  assert.equal(await more.evaluate(item => getComputedStyle(item).opacity), '1');
  await page.locator('#search').focus();
  await page.locator('#task-title').hover();
  assert.equal(await more.evaluate(item => getComputedStyle(item).opacity), '0');
  await populated.locator('.project-header').hover();
  assert.equal(await more.evaluate(item => getComputedStyle(item).opacity), '1');
  await create(page, 'populated').click();
  assert.equal(await page.locator('#new-task-project').inputValue(), 'populated');
  assert.equal(await populated.evaluate(item => item.open), false);
  assert.equal(await page.locator('#new-task-prompt').evaluate(item => item === document.activeElement), true);
  await page.locator('#new-task-prompt').fill('A draft to keep');
  await page.keyboard.press('Escape');
  assert.equal(await page.locator('#new-task-view').isVisible(), false);
  assert.equal(await page.locator('#task-title').textContent(), seed.name);
  assert.equal(await create(page, 'populated').evaluate(item => item === document.activeElement), true);
  await more.click();
  await page.locator('#settings-panel:not([hidden])').waitFor();
  assert.equal(await page.locator('.project-menu').count(), 0);
  assert.equal(await page.getByRole('menuitem', {
    name: 'New task',
    exact: true
  }).count(), 0);
  await page.keyboard.press('Escape');
  assert.equal(await page.locator('#settings-panel').isVisible(), false);
  await create(page, 'populated').click();
  assert.equal(await page.locator('#new-task-prompt').inputValue(), 'A draft to keep');
  await page.getByRole('button', {
    name: 'Cancel new task',
    exact: true
  }).click();
  assert.equal(await create(page, 'populated').evaluate(item => item === document.activeElement), true);
});
test('creation failures preserve pasted prompts; success uses defaults and selects the created task', async () => {
  const {
    page,
    setTasks
  } = await fixture();
  let fail = true;
  const posts = [];
  const created = {
    id: 'created',
    name: 'Created safely',
    project: 'empty',
    status: 'complete',
    live: false
  };
  await page.route('**/api/tasks', async route => {
    assert.equal(route.request().method(), 'POST');
    posts.push(route.request().postDataJSON());
    if (fail) return route.fulfill({
      status: 503,
      json: {}
    });
    setTasks([seed, created]);
    return route.fulfill({
      status: 201,
      json: {
        id: created.id,
        name: created.name,
        status: created.status
      }
    });
  });
  await page.route('**/api/tasks/created', route => route.fulfill({
    json: created
  }));
  await create(page, 'empty').click();
  await page.locator('#new-task-name').fill('Created safely');
  await page.locator('#new-task-prompt').evaluate(field => {
    const data = new DataTransfer();
    data.setData('text/plain', 'first line\nsecond line');
    field.dispatchEvent(new ClipboardEvent('paste', {
      bubbles: true,
      cancelable: true,
      clipboardData: data
    }));
  });
  assert.equal(await page.locator('#new-task-prompt').inputValue(), 'first line\nsecond line');
  assert.equal(posts.length, 0);
  await page.getByRole('button', {
    name: 'Start task',
    exact: true
  }).click();
  await page.locator('#new-task-error:not([hidden])').waitFor();
  assert.equal(await page.locator('#new-task-prompt').inputValue(), 'first line\nsecond line');
  assert.equal(await page.locator('#new-task-name').inputValue(), 'Created safely');
  fail = false;
  await page.getByRole('button', {
    name: 'Start task',
    exact: true
  }).click();
  await page.waitForFunction(() => document.querySelector('#task-title').textContent === 'Created safely');
  assert.deepEqual(posts, Array(2).fill({
    project: 'empty',
    prompt: 'first line\nsecond line',
    name: 'Created safely'
  }));
  assert.equal(await page.locator('#new-task-view').isVisible(), false);
  assert.equal(await page.locator('.task-row[aria-current="page"]').getAttribute('data-task-id'), 'created');
});
test('canceling an in-flight creation ignores a late result and keeps the previous task', async () => {
  const {
    page
  } = await fixture();
  let release;
  const hold = new Promise(resolve => {
    release = resolve;
  });
  await page.route('**/api/tasks', async route => {
    await hold;
    await route.fulfill({
      status: 201,
      json: {
        id: 'late',
        name: 'Late',
        status: 'in_progress'
      }
    }).catch(() => {});
  });
  await create(page, 'empty').click();
  await page.locator('#new-task-prompt').fill('Pending prompt');
  const requested = page.waitForRequest(request => request.method() === 'POST');
  await page.getByRole('button', {
    name: 'Start task',
    exact: true
  }).click();
  await requested;
  await page.getByRole('button', {
    name: 'Cancel new task',
    exact: true
  }).click();
  release();
  assert.equal(await page.locator('#task-title').textContent(), seed.name);
  await create(page, 'empty').click();
  assert.equal(await page.locator('#new-task-prompt').inputValue(), 'Pending prompt');
  assert.match(await page.locator('#new-task-error').textContent(), /may have completed/);
});
test('project creation reaches the isolated harness and attaches its real default-backend terminal', async () => {
  const api = process.env.WORKSPACE_TEST_API || 'http://127.0.0.1:17744';
  const headers = {
    Authorization: 'Bearer test-token'
  };
  const existing = (await (await fetch(`${api}/api/tasks?archived=all`, {
    headers
  })).json()).tasks;
  assert.ok(existing.some(task => task.name === 'echo-bash' && task.backend === 'bash-test' && task.worktree_path.includes('/argus-test-home-')), 'Refusing writes without the isolated harness');
  const page = await browser.newPage({
    viewport: {
      width: 1440,
      height: 900
    }
  });
  cleanup.push(() => page.close());
  await page.goto(frontend);
  await page.locator('#token').fill('test-token');
  await page.locator('#connect-button').click();
  await create(page, 'test-proj').click();
  const name = `project-create-${Date.now()}`;
  await page.locator('#new-task-name').fill(name);
  await page.locator('#new-task-prompt').fill('/dev/stdin');
  await page.getByRole('button', {
    name: 'Start task',
    exact: true
  }).click();
  await page.waitForFunction(name => document.querySelector('#task-title').textContent === name, name);
  const id = await page.locator('.task-row[aria-current="page"]').getAttribute('data-task-id');
  const actual = await (await fetch(`${api}/api/tasks/${id}`, {
    headers
  })).json();
  assert.equal(actual.project, 'test-proj');
  assert.equal(actual.backend, 'bash-test');
  assert.equal(actual.prompt, '/dev/stdin');
  await page.waitForFunction(() => document.querySelector('#terminal-state').textContent === 'Live terminal');
  const marker = `PROJECT_CREATE_${Date.now()}`;
  await page.locator('#draft').fill(`printf '${marker}\\n'`);
  await page.locator('#send').click();
  await page.waitForFunction(marker => [...document.querySelectorAll('.xterm-screen .xterm-rows>div')].some(row => row.textContent.trim() === marker), marker);
});
test('passive task disappearance preserves an open creation draft and disconnect clears it', async () => {
  const {
    page,
    setTasks
  } = await fixture();
  await create(page, 'empty').click();
  await page.locator('#new-task-prompt').fill('Keep this draft through refresh');
  setTasks([]);
  await refreshWorkspace(page);
  assert.equal(await page.locator('#new-task-view').isVisible(), true);
  assert.equal(await page.locator('#new-task-prompt').inputValue(), 'Keep this draft through refresh');
  await page.getByRole('button', {
    name: 'Cancel new task',
    exact: true
  }).click();
  assert.equal(await page.locator('#task-title').textContent(), 'Task unavailable');
  await create(page, 'empty').click();
  await page.locator('#disconnect').click();
  await page.locator('#token').fill('fixture-token');
  await page.locator('#connect-button').click();
  await create(page, 'empty').click();
  assert.equal(await page.locator('#new-task-prompt').inputValue(), '');
});
test('creation prompt keeps Shift+Enter and composition native while ordinary Enter starts once', async () => {
  const {
    page,
    setTasks
  } = await fixture();
  let posts = 0;
  const created = {
    id: 'keyboard-created',
    name: 'Keyboard created',
    project: 'empty',
    status: 'complete'
  };
  await page.route('**/api/tasks', route => {
    posts++;
    setTasks([seed, created]);
    return route.fulfill({
      status: 201,
      json: created
    });
  });
  await page.route('**/api/tasks/keyboard-created', route => route.fulfill({
    json: created
  }));
  await create(page, 'empty').click();
  await page.locator('#new-task-prompt').fill('First line');
  await page.keyboard.press('Shift+Enter');
  assert.equal(await page.locator('#new-task-prompt').inputValue(), 'First line\n');
  await page.locator('#new-task-prompt').evaluate(field => {
    field.dispatchEvent(new KeyboardEvent('keydown', {
      key: 'Enter',
      isComposing: true,
      bubbles: true,
      cancelable: true
    }));
    field.dispatchEvent(new KeyboardEvent('keydown', {
      key: 'Enter',
      repeat: true,
      bubbles: true,
      cancelable: true
    }));
  });
  assert.equal(posts, 0);
  await page.keyboard.press('Enter');
  await page.waitForFunction(() => document.querySelector('#task-title').textContent === 'Keyboard created');
  assert.equal(posts, 1);
});
test('compact project creation closes the task drawer and returns focus on cancel', async () => {
  const {
    page
  } = await fixture();
  await page.setViewportSize({
    width: 390,
    height: 844
  });
  await page.locator('#toggle-nav').click();
  await create(page, 'empty').click();
  assert.equal(await page.locator('#new-task-prompt').evaluate(field => field === document.activeElement && !field.closest('[inert]')), true);
  assert.equal(await page.locator('#toggle-nav').getAttribute('aria-expanded'), 'false');
  await page.keyboard.press('Escape');
  assert.equal(await page.locator('#new-task-view').isVisible(), false);
  assert.equal(await page.locator('#toggle-nav').evaluate(control => control === document.activeElement), true);
});
test('project actions are absent when no non-creation action is available', async () => {
  const {
    page
  } = await fixture();
  const result = await page.evaluate(async () => {
    const {
      projectGroup
    } = await import('/workspace/rail/project-group.js');
    const group = projectGroup('only-create', [], {
      collapsed: false,
      onCollapse() {},
      onMenu() {},
      onCreate() {},
      renderTask() {}
    });
    return {
      more: group.querySelectorAll('.project-configuration').length,
      menus: group.querySelectorAll('[role="menu"]').length,
      plus: group.querySelectorAll('.project-new-task').length
    };
  });
  assert.deepEqual(result, {
    more: 0,
    menus: 0,
    plus: 1
  });
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
