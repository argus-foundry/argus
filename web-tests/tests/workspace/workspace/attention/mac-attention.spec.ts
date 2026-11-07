import { test } from '@playwright/test';
import { resetServer } from '../../../_helpers';
import assert from 'node:assert/strict';
test('workspace attention mac-attention', async ({
  browser
}) => {
  await resetServer();
  const page = await browser.newPage();
  {
    await page.route('**/attention-fixture', route => route.fulfill({
      contentType: 'text/html',
      body: `<!doctype html><title>Argus</title><link rel="icon" href="/icon-192.png"><footer class="nav-footer"></footer><script type="module">
 import {WorkspaceAttention} from '/workspace/attention/workspace-attention.js';
 const tasks=['a','b','c','d','e'].map(id=>({id,name:'Task '+id,needs_input:false}));const selected=[];
 window.notifications=0;window.Notification=class{static permission='granted';constructor(){window.notifications++;}close(){}};localStorage.setItem('argus-workspace:attention',JSON.stringify({banners:true}));
 const attention=new WorkspaceAttention(task=>selected.push(task.id));
 window.fixture={attention,tasks,selected,arrive:id=>{tasks.find(task=>task.id===id).needs_input=true;attention.update(tasks);attention.event({type:'session.needs_input',task_id:id,payload:{needs_input:true}});}};
 </script>`
    }));
    await page.goto(process.env.WORKSPACE_TEST_URL + '/attention-fixture');
    await page.waitForFunction(() => window.fixture);
    assert.equal(await page.locator('.attention-alert').count(), 0);
    await page.evaluate(() => {
      for (const id of ['a', 'b', 'c', 'd', 'e']) window.fixture.arrive(id);
      window.fixture.arrive('a');
    });
    assert.equal(await page.locator('.attention-alert').count(), 4);
    assert.equal(await page.evaluate(() => window.notifications), 0);
    assert.match(await page.locator('#attention-alerts').textContent(), /2 more tasks need input/);
    assert.match(await page.title(), /^\(5\)/);
    assert.equal(await page.locator('#attention-menu-toggle,#attention-menu').count(), 0);
    await page.getByRole('button', {
      name: 'Task b needs input',
      exact: true
    }).click();
    assert.deepEqual(await page.evaluate(() => window.fixture.selected), ['b']);
    await page.evaluate(() => window.fixture.attention.clear());
    assert.equal(await page.locator('.attention-alert').count(), 0);
    assert.equal(await page.title(), 'Argus');
    await page.evaluate(() => {
      window.fixture.attention.update(window.fixture.tasks, 'a');
      window.fixture.arrive('a');
      window.fixture.attention.event({
        type: 'session.idle',
        task_id: 'b'
      });
    });
    assert.equal(await page.locator('.attention-alert').count(), 0);
    console.log('PASS sparse immediate alerts, burst summary, dedupe, title/favicon and attention task selection');
  }
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
