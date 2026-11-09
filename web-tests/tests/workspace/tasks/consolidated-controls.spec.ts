import { test } from '@playwright/test';
import { resetServer } from '../../_helpers';
import assert from 'node:assert/strict';
import { refreshWorkspace } from '../_workspace-controls-fixture.ts';
test('tasks consolidated-controls', async ({
  browser
}) => {
  await resetServer();
  {
    const page = await browser.newPage();
    page.setDefaultTimeout(8000);
    const errors = [],
      writes = [];
    let rawReads = 0,
      fail = false;
    page.on('pageerror', error => errors.push(error.message));
    const task = {
      id: 'a',
      name: 'Consolidated task',
      project: 'fixture',
      backend: 'claude',
      status: 'complete',
      pinned: true,
      live: false
    };
    await page.route('**/api/**', route => {
      const request = route.request(),
        path = new URL(request.url()).pathname;
      let value = {};
      if (path.endsWith('/pin') && request.method() === 'POST') {
        writes.push(path);
        assert.deepEqual(request.postDataJSON(),{pinned:false});Object.assign(task,{pinned:false,archived:false});
        return route.fulfill({
          json:{task_id:task.id,pinned:task.pinned,archived:task.archived}
        });
      }
      if (path === '/api/tasks') {
        if (fail) return route.fulfill({
          status: 503,
          json: {}
        });
        value = {
          tasks: [task]
        };
      } else if (path === '/api/projects/full') value = {
        projects: [{
          name: 'fixture'
        }]
      };else if (path === '/api/backends') value = {
        backends: [{
          name: 'claude',
          kind: 'claude',
          session_switcher: true
        }]
      };else if (path.endsWith('/raw')) {
        rawReads++;
        value = task;
      } else if (path === '/api/tasks/a') value = task;else if (path === '/api/accounts') value = [];else if (path.endsWith('/size')) value = {
        cols: 80,
        rows: 24
      };
      if (path.endsWith('/output')) return route.fulfill({
        body: '',
        headers: {
          'X-Output-Total': '0'
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
    await page.goto(process.env.WORKSPACE_TEST_URL);
    await page.locator('#token').fill('fixture');
    await page.locator('#connect-button').click();
    await page.locator('.task-row').waitFor();
    await page.waitForFunction(() => document.querySelector('#composer-model') || document.querySelector('#composer-label').textContent.includes('claude'));
    assert.equal(await page.locator('#archive-task,#refresh-tasks,.project-menu').count(), 0);
    const menu = () => page.getByRole('button', {
      name: 'Task actions for Consolidated task',
      exact: true
    });
    await menu().click();
    for (const name of ['Advance status', 'Revert status', 'Archive', 'Toggle pin']) assert.equal(await page.getByRole('menuitem', {
      name,
      exact: true
    }).count(), 0);
    await page.getByRole('menuitem', {
      name: 'Unpin',
      exact: true
    }).waitFor();
    assert.equal(rawReads, 1, 'Only composer identity reads raw when the DTO supplies pinned.');
    await page.getByRole('menuitem', {
      name: 'Unpin',
      exact: true
    }).click();
    await page.locator('#notice').filter({
      hasText: 'Task unpinned.'
    }).waitFor();
    assert.deepEqual(writes, ['/api/tasks/a/pin']);
    await menu().click();
    await page.getByRole('menuitem', {
      name: 'Pin',
      exact: true
    }).waitFor();
    await page.getByRole('menuitem', {
      name: 'Task settings',
      exact: true
    }).click();
    const sessions = page.locator('#task-session-actions');
    await sessions.getByRole('button', {
      name: 'Switch Claude session',
      exact: true
    }).waitFor();
    for (const name of ['Stop agent', 'Resume agent']) assert.equal(await sessions.getByRole('button', {
      name,
      exact: true
    }).count(), 1);
    assert.equal(await page.locator('.terminal-toolbar #switch-claude-session').count(), 0);
    assert.equal(await page.locator('#fit-session,#split-toggle').count(), 2);
    await page.getByRole('button', {
      name: 'Back to task',
      exact: true
    }).click();
    fail = true;
    await refreshWorkspace(page);
    await page.locator('#connection-banner').waitFor();
    assert.match(await page.locator('#connection-state').getAttribute('aria-label'), /Connection unavailable/);
    assert.doesNotMatch(await page.locator('#connection-state').textContent(), /retry|reconnect|↻/i);
    assert.equal(await page.getByRole('button', {
      name: 'Retry connection',
      exact: true
    }).count(), 1);
    assert.equal(await page.locator('#reconnect-terminal').count(), 1);
    assert.deepEqual(errors, []);
    console.log('PASS consolidated menu/archive/status, supplied pin state, session controls and single connection Retry');
  }
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
