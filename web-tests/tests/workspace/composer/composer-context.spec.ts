import { test } from '@playwright/test';
import { resetServer } from '../../_helpers';
import assert from 'node:assert/strict';
let browser;
const cleanup = [];
async function fixture() {
  const page = await browser.newPage({
    viewport: {
      width: 900,
      height: 700
    }
  });
  cleanup.push(() => page.close());
  const errors = [];
  page.on('pageerror', e => errors.push(e.message));
  cleanup.push(() => assert.deepEqual(errors, []));
  const html = `<!doctype html><link rel="stylesheet" href="/workspace/styles.css"><div style="position:absolute;bottom:20px;left:20px;right:20px"><form id="composer"><textarea id="draft"></textarea><div class="composer-footer"><span id="composer-label"></span><div class="composer-actions"><button id="context-toggle" type="button"><span class="context-ring"></span></button><button id="attach-files" type="button">+</button><button id="send" type="submit"></button></div></div></form><div id="composer-status"></div></div><script type="module">
  import {TaskComposer} from '/composer/composer.js';
  const raw={id:'a',backend:'custom-claude',model:'opus',account:'work'};
  const reads={raw,bridge:{live:true,context:{tokens:421200,window:1000000,percent:42.12},last_measure_at:new Date().toISOString()},meta:{entries:[]},accounts:[{name:'work',label:'Work',plan:'Team',email:'fixture@example.invalid',usage:{five_hour_pct:8,five_hour_resets_at:new Date(Date.now()+3600000).toISOString(),weekly_pct:83,weekly_resets_at:new Date(Date.now()+86400000).toISOString()}}],backends:{backends:[{name:'custom-claude',kind:'claude'},{name:'custom-codex',kind:'codex'}]},hera:{orchestrators:[{roles:[{live:true,task_id:'a',tokens_input:234300,tokens_cache_read:232100,tokens_output:33}]}]}};
  window.held=[];window.block=false;window.reads=reads;
  const api={json:async(path,options)=>{const key=path.split('/').pop().split('?')[0];if(window.block) return new Promise(resolve=>window.held.push(()=>resolve(reads[key])));return reads[key];}};
  window.composer=new TaskComposer(async()=>{},api);window.composer.select({id:'a',backend:'custom-claude'});window.composer.setInteractive(true);window.composer.focus();window.ready=true;
  </script>`;
  await page.route('**/context-fixture', route => route.fulfill({
    contentType: 'text/html',
    body: html
  }));
  await page.goto((process.env.WORKSPACE_TEST_URL || 'http://127.0.0.1:17750') + '/context-fixture');
  await page.waitForFunction(() => window.ready && document.querySelector('.context-ring').dataset.known === 'true');
  return page;
}
test.beforeAll(async ({
  browser: workerBrowser
}) => {
  browser = workerBrowser;
  await resetServer();
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
test('context popover shows sourced fill, remaining quotas and scoped totals with dismissal', async () => {
  const page = await fixture();
  await page.locator('#draft').fill('keep my draft');
  await page.locator('#context-toggle').click();
  await page.locator('#composer-context:popover-open').waitFor();
  const panel = page.locator('#composer-context');
  const content = await panel.textContent();
  for (const value of ['421,200 / 1,000,000', '42.1%', '92% left', '17% left', 'Role token totals', '234,300', '232,100', '33', 'Team', 'fixture@example.invalid']) assert.ok(content.includes(value), value);
  assert.equal(await page.locator('#context-toggle').getAttribute('aria-expanded'), 'true');
  assert.equal(await page.locator('#context-toggle').getAttribute('data-tooltip'), 'Context 42.1% used. Open usage details');
  assert.equal(await panel.locator('progress').count(), 3);
  await page.keyboard.press('Escape');
  await page.waitForFunction(() => document.querySelector('#context-toggle').getAttribute('aria-expanded') === 'false');
  assert.equal(await page.locator('#draft').inputValue(), 'keep my draft');
  await page.locator('#context-toggle').click();
  await page.mouse.click(850, 30);
  assert.equal(await panel.isVisible(), false);
  await page.locator('#context-toggle').focus();
  await page.keyboard.press('Enter');
  await panel.waitFor({
    state: 'visible'
  });
  await page.setViewportSize({
    width: 320,
    height: 600
  });
  await page.waitForFunction(() => {
    const box = document.querySelector('#composer-context').getBoundingClientRect();
    return box.x >= 0 && box.right <= innerWidth && box.y >= 0 && box.bottom <= innerHeight;
  });
  const box = await panel.boundingBox();
  assert.ok(box.x >= 0 && box.x + box.width <= 320);
  assert.ok(box.y >= 0 && box.y + box.height <= 600);
  await page.screenshot({
    path: '.context/composer-context.png'
  });
});
test('task switch and disconnect reject late reads and clear measurements', async () => {
  const page = await fixture();
  await page.evaluate(() => {
    window.block = true;
    window.composer.context.refresh();
    const old = window.held.splice(0);
    window.composer.select({
      id: 'b',
      backend: 'custom-codex'
    });
    old.forEach(resolve => resolve());
  });
  await page.waitForTimeout(100);
  assert.equal(await page.locator('.context-ring').getAttribute('data-known'), 'false');
  await page.evaluate(() => {
    window.composer.clear();
    window.held.splice(0).forEach(resolve => resolve());
  });
  await page.waitForTimeout(100);
  assert.equal(await page.locator('#context-toggle').isDisabled(), true);
  assert.equal(await page.locator('.context-ring').getAttribute('data-known'), 'false');
  assert.equal(await page.locator('#composer-context').textContent(), 'ContextContext window usage is not available for this session.');
});
test('Codex uses a recorded context stamp, with no inferred window or Claude account fields', async () => {
  const page = await fixture();
  await page.evaluate(() => {
    window.reads.raw = {
      id: 'c',
      backend: 'custom-codex',
      account: 'work',
      model: 'gpt-6.1-sol'
    };
    window.reads.bridge = {
      live: false,
      context: {
        percent: 99
      }
    };
    window.reads.meta = {
      entries: [{
        namespace: 'hera',
        key: 'context_size',
        value: '12345',
        updated_at: new Date().toISOString()
      }]
    };
    window.composer.select({
      id: 'c',
      backend: 'custom-codex'
    });
  });
  await page.locator('#context-toggle').click();
  await page.waitForFunction(() => document.querySelector('#composer-context').textContent.includes('12,345 tokens'));
  const content = await page.locator('#composer-context').textContent();
  assert.ok(content.includes('Last recorded'));
  assert.ok(content.includes('limit is not exposed'));
  for (const value of ['Team', 'fixture@example.invalid', '5h limit', '7d limit', '99%']) assert.equal(content.includes(value), false, value);
  assert.equal(await page.locator('#composer-context progress').count(), 0);
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
