import { test } from '@playwright/test';
import { resetServer } from '../../../_helpers';
import assert from 'node:assert/strict';
test('tasks settings mac-session', async ({
  browser
}) => {
  await resetServer();
  const page = await browser.newPage();
  page.setDefaultTimeout(5000);
  const errors = [],
    writes = [],
    sessionReads = [];
  let failSessionRead = true;
  page.on('pageerror', error => errors.push(error.message));
  const tasks = [{
    id: 'a',
    name: 'Claude A',
    project: 'fixture',
    backend: 'claude',
    status: 'in_progress',
    live: true,
    idle: true
  }, {
    id: 'b',
    name: 'Codex B',
    project: 'fixture',
    backend: 'codex',
    status: 'in_review',
    live: false
  }];
  {
    await page.route('**/api/**', async route => {
      const request = route.request(),
        path = new URL(request.url()).pathname;
      let result = {};
      if (request.method() === 'POST') {
        writes.push({
          path,
          body: request.postDataJSON()
        });
        result = {
          status: 'switched'
        };
      } else if (path === '/api/tasks') result = {
        tasks
      };else if (path === '/api/projects') result = {
        projects: [{
          name: 'fixture'
        }]
      };else if (path === '/api/backends') result = {
        backends: [{
          name: 'claude',
          kind: 'claude',
          session_switcher: true
        }, {
          name: 'codex',
          kind: 'codex',
          session_switcher: false
        }]
      };else if (path.endsWith('/claude-sessions')) {
        sessionReads.push(path);
        if (failSessionRead) {
          failSessionRead = false;
          return route.fulfill({
            status: 503,
            json: {}
          });
        }
        result = {
          current_session_id: 'old',
          sessions: [{
            id: 'new',
            title: 'New conversation',
            mod_time: '2026-11-06T10:00:00.123456789Z',
            branch: 'main',
            pr_ref: '#12'
          }, {
            id: 'old',
            title: 'Current conversation'
          }]
        };
      } else if (path.endsWith('/size')) result = {
        cols: 80,
        rows: 24
      };else if (path.endsWith('/raw')) result = tasks.find(task => path.includes('/' + task.id + '/'));
      if (path.endsWith('/output')) return route.fulfill({
        body: '',
        headers: {
          'X-Output-Total': '0'
        }
      });
      if (path.endsWith('/stream')) return route.fulfill({
        contentType: 'text/event-stream',
        body: ': fixture\n\n'
      });
      return route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify(result)
      });
    });
    await page.goto(process.env.WORKSPACE_TEST_URL);
    await page.locator('#token').fill('fixture');
    await page.locator('#connect-button').click();
    await page.locator('.task-row').filter({
      hasText: 'Codex B'
    }).click();
    await page.getByRole('button', {
      name: 'Task actions for Claude A',
      exact: true
    }).click();
    await page.getByRole('menuitem', {
      name: 'Task settings',
      exact: true
    }).click();
    assert.equal(await page.locator('.terminal-toolbar #switch-claude-session').count(), 0);
    await page.getByRole('button', {
      name: 'Stop agent',
      exact: true
    }).waitFor();
    for (const control of await page.locator('#task-session-actions button').all()) {
      assert.ok(await control.getAttribute('data-tooltip'));
      assert.ok(await control.getAttribute('aria-label'));
      assert.equal(await control.getAttribute('title'), null);
    }
    const stop = page.getByRole('button', {
      name: 'Stop agent',
      exact: true
    });
    await stop.hover();
    const help = page.locator('#workspace-tooltip');
    await help.waitFor({
      state: 'visible'
    });
    assert.equal(await help.locator('span').textContent(), await stop.getAttribute('data-tooltip'));
    await page.mouse.move(0, 0);
    await help.waitFor({
      state: 'hidden'
    });
    await page.locator('#switch-claude-session').click();
    await page.locator('#claude-sessions-dialog').getByRole('button', {
      name: 'Retry',
      exact: true
    }).click();
    await page.getByRole('button', {
      name: 'Current conversation',
      exact: false
    }).waitFor();
    assert.equal(await page.getByRole('button', {
      name: 'Current conversation',
      exact: false
    }).isDisabled(), true);
    await page.getByRole('button', {
      name: 'New conversation',
      exact: false
    }).click();
    await page.waitForFunction(() => !document.querySelector('#claude-sessions-dialog'));
    assert.deepEqual(writes, [{
      path: '/api/tasks/a/claude-session',
      body: {
        session_id: 'new'
      }
    }]);
    assert.deepEqual(sessionReads, ['/api/tasks/a/claude-sessions', '/api/tasks/a/claude-sessions']);
    assert.equal(await page.locator('#task-title').textContent(), 'Codex B');
    await page.getByRole('button', {
      name: 'Back to task',
      exact: true
    }).click();
    await page.getByRole('button', {
      name: 'Task actions for Codex B',
      exact: true
    }).click();
    await page.getByRole('menuitem', {
      name: 'Task settings',
      exact: true
    }).click();
    await page.getByRole('button', {
      name: 'Stop agent',
      exact: true
    }).waitFor();
    assert.equal(await page.locator('#switch-claude-session').count(), 0);
    await page.getByRole('button', {
      name: 'Back to task',
      exact: true
    }).click();
    await page.getByRole('button', {
      name: 'Task actions for Claude A',
      exact: true
    }).click();
    await page.getByRole('menuitem', {
      name: 'Task settings',
      exact: true
    }).click();
    let release, reached;
    const held = new Promise(resolve => reached = resolve),
      gate = new Promise(resolve => release = resolve);
    await page.route('**/api/tasks/a/claude-sessions', async route => {
      reached();
      await gate;
      await route.fulfill({
        json: {
          sessions: []
        }
      }).catch(() => {});
    });
    await page.locator('#switch-claude-session').click();
    await held;
    await page.goBack();
    await page.waitForFunction(() => document.querySelector('#task-settings-panel').hidden);
    release();
    assert.equal(await page.locator('#claude-sessions-dialog').count(), 0);
    assert.equal(writes.length, 1);
    assert.deepEqual(errors, []);
    console.log('PASS captured Claude session picker, current marker, switch body, unsupported backend and closed-view cancellation');
  }
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
