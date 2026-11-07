import { test } from '@playwright/test';
import { resetServer } from '../../_helpers';
import assert from 'node:assert/strict';
test('tasks mac-information', async ({
  browser
}) => {
  await resetServer();
  const page = await browser.newPage();
  page.setDefaultTimeout(10000);
  const task = {
    id: 'a',
    name: 'Info task',
    project: 'fixture',
    branch: 'feature/info',
    worktree_path: '/tmp/task',
    created_at: '2026-11-06T10:00:00Z',
    status: 'complete',
    prompt: 'Original prompt',
    backend: 'claude'
  };
  const requests = [],
    creates = [],
    mutations = [];
  let succeed = false;
  {
    await page.route('**/api/**', route => {
      const path = new URL(route.request().url()).pathname;
      requests.push(route.request().url());
      if (route.request().method() !== 'GET') {
        mutations.push({
          path,
          method: route.request().method(),
          body: route.request().postDataJSON()
        });
        if (path === '/api/tasks') {
          creates.push(route.request().postDataJSON());
          return route.fulfill({
            status: succeed ? 201 : 503,
            json: succeed ? {
              ...task,
              id: 'created',
              name: 'Created account task'
            } : {}
          });
        }
        return route.fulfill({
          json: {}
        });
      }
      let payload = {};
      if (path === '/api/tasks') payload = {
        tasks: succeed ? [task, {...task, id: 'created', name: 'Created account task'}] : [task]
      };else if (path === '/api/projects') payload = {
        projects: [{
          name: 'fixture'
        }]
      };else if (path === '/api/backends') payload = {
        backends: [{
          name: 'claude',
          kind: 'claude'
        }, {
          name: 'codex',
          kind: 'codex'
        }]
      };else if (path === '/api/accounts') payload = [{
        name: 'default'
      }, {
        name: 'work',
        label: 'Work Claude',
        supports: {
          claude: true
        },
        is_default: true
      }, {
        name: 'codex-work',
        label: 'Work Codex',
        supports: {
          codex: true
        }
      }];else if (path === '/api/settings') payload = {
        defaults: {
          backend: 'claude'
        }
      };else if (path === '/api/tasks/created') payload = {
        ...task,
        id: 'created',
        name: 'Created account task'
      };else if (path.endsWith('/size')) payload = {
        cols: 80,
        rows: 24
      };else if (path.endsWith('/links')) payload = {
        links: [{
          url: 'https://github.com/org/repo/pull/1',
          label: 'PR 1',
          isPR: true
        }, {
          url: 'javascript:alert(1)',
          label: 'Unsafe'
        }, {
          url: 'https://example.com/doc',
          label: 'Documentation'
        }]
      };else if (path.endsWith('/git/status')) payload = {
        Status: ' M src/file.go'
      };
      if (path.endsWith('/output')) return route.fulfill({
        body: '',
        headers: {
          'X-Output-Total': '0'
        }
      });
      return route.fulfill({
        json: payload
      });
    });
    await page.addInitScript(() => {
      window.copied = [];
      Object.defineProperty(navigator, 'clipboard', {
        value: {
          writeText: async value => window.copied.push(value)
        },
        configurable: true
      });
    });
    await page.goto(process.env.WORKSPACE_TEST_URL);
    await page.locator('#token').fill('fixture');
    await page.locator('#connect-button').click();
    await page.locator('.task-row').click();
    await page.locator('.task-header-links a').waitFor();
    assert.equal(await page.locator('.task-header-links a').getAttribute('href'), 'https://github.com/org/repo/pull/1');
    await page.locator('#tool-changes').click();
    await page.getByRole('button', {
      name: 'Show tree view',
      exact: true
    }).click();
    await page.locator('.file-row').first().waitFor();
    assert.equal(await page.locator('#tool-files').count(), 0);
    assert.equal(requests.filter(url => url.endsWith('/links')).length, 1);
    assert.equal(await page.locator('#tool-panel summary').textContent(), 'src');
    const file = page.locator('.file-entry').filter({
      has: page.getByRole('button', {
        name: /^Open diff for src\/file\.go, /
      })
    });
    await file.getByRole('button', {
      name: 'File actions for src/file.go',
      exact: true
    }).click();
    await page.getByRole('menuitem', {
      name: 'Copy path',
      exact: true
    }).click();
    await file.getByRole('button', {
      name: 'File actions for src/file.go',
      exact: true
    }).click();
    await page.getByRole('menuitem', {
      name: 'Copy absolute path',
      exact: true
    }).click();
    assert.deepEqual(await page.evaluate(() => window.copied), ['src/file.go', '/tmp/task/src/file.go']);
    await page.getByRole('button', {
      name: 'Show flat view',
      exact: true
    }).click();
    assert.equal(await page.locator('#tool-panel summary').count(), 0);
    assert.equal(await page.locator('#tool-panel .file-row').filter({
      hasText: 'src/file.go'
    }).count(), 1);
    await page.locator('#tool-status').click();
    await page.locator('#tool-panel').getByRole('link', {
      name: 'Documentation'
    }).waitFor();
    assert.equal(await page.getByRole('link', {
      name: 'Unsafe'
    }).count(), 0);
    assert.match(await page.locator('#tool-panel').textContent(), /2026-11-06/);
    await page.locator('.project-new-task').click();
    await page.locator('#new-task-backend option[value="codex"]').waitFor({
      state: 'attached'
    });
    assert.equal(await page.locator('#new-task-account').inputValue(), 'work');
    await page.locator('#new-task-backend').selectOption('codex');
    await page.locator('#new-task-account').selectOption('codex-work');
    await page.locator('#new-task-prompt').fill('Account creation fixture');
    await page.getByRole('button', {
      name: 'Start task',
      exact: true
    }).click();
    await page.locator('#new-task-error:not([hidden])').waitFor();
    assert.equal(creates[0].account, 'codex-work');
    assert.equal(creates[0].backend, 'codex');
    assert.equal(await page.locator('#new-task-prompt').inputValue(), 'Account creation fixture');
    succeed = true;
    mutations.length = 0;
    await page.getByRole('button', {
      name: 'Start task',
      exact: true
    }).click();
    await page.waitForFunction(() => document.querySelector('#task-title').textContent === 'Created account task');
    assert.deepEqual(mutations, [{
      path: '/api/tasks',
      method: 'POST',
      body: {
        project: 'fixture',
        backend: 'codex',
        account: 'codex-work',
        prompt: 'Account creation fixture'
      }
    }]);
    assert.equal(await page.locator('.task-row[aria-current="page"]').getAttribute('data-task-id'), 'created');
    assert.equal(creates.at(-1).account, 'codex-work');
    assert.equal(creates.at(-1).backend, 'codex');
    console.log('PASS consolidated Changes flat/tree, copy controls, safe Info/header links and backend creation choice');
  }
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
