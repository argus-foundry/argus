import { test } from '@playwright/test';
import { resetServer } from '../../../_helpers';
import assert from 'node:assert/strict';
test('workspace events mac-events', async ({
  browser
}) => {
  await resetServer();
  const page = await browser.newPage();
  page.setDefaultTimeout(10000);
  const task = {
    id: 'a',
    name: 'Before event',
    project: 'fixture',
    status: 'complete',
    live: false
  };
  let reads = 0;
  {
    await page.route('**/api/**', route => {
      const path = new URL(route.request().url()).pathname;
      let payload = {};
      if (path === '/api/tasks') {
        reads++;
        payload = {
          tasks: [task]
        };
      } else if (path === '/api/projects') payload = {
        projects: [{
          name: 'fixture'
        }]
      };else if (path.endsWith('/size')) payload = {
        cols: 80,
        rows: 24
      };
      if (path.endsWith('/output')) return route.fulfill({
        body: '',
        headers: {
          'X-Output-Total': '0'
        }
      });
      if (path === '/api/events/stream') return route.fulfill({
        contentType: 'text/event-stream',
        body: 'event: task.renamed\ndata: {"id":1,"type":"task.renamed","task_id":"a","payload":{"to":"After event"}}\n\n'
      });
      return route.fulfill({
        json: payload
      });
    });
    await page.goto(process.env.WORKSPACE_TEST_URL);
    await page.locator('#token').fill('fixture');
    await page.locator('#connect-button').click();
    await page.locator('.task-row').filter({
      hasText: 'After event'
    }).waitFor();
    assert.equal(reads, 1);
    await page.locator('#connection-banner').waitFor();
    await page.getByRole('button', {
      name: 'Retry connection',
      exact: true
    }).click();
    await page.waitForFunction(() => document.querySelector('.task-name')?.textContent === 'Before event');
    console.log('PASS subscribe-before-snapshot buffers named event and persistent banner retries without disconnect');
  }
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
