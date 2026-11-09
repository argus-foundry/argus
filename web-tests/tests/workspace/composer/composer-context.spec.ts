import { test } from '@playwright/test';
import { resetServer } from '../../_helpers';
import assert from 'node:assert/strict';
let browser;
const cleanup = [];
async function fixture(contextRoute = 'missing') {
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
  const reads={raw,bridge:{live:true,context:{tokens:421200,window:1000000,percent:42.12},last_measure_at:new Date().toISOString()},meta:{entries:[]},accounts:[{name:'work',label:'Work',plan:'Team',org:'Fixture organization',email:'fixture@example.invalid',usage:{five_hour_pct:8,five_hour_resets_at:new Date(Date.now()+3600000).toISOString(),weekly_pct:83,weekly_resets_at:new Date(Date.now()+86400000).toISOString()}}],backends:{backends:[{name:'custom-claude',kind:'claude'},{name:'custom-codex',kind:'codex'}]},hera:{orchestrators:[{roles:[{live:true,task_id:'a',tokens_input:234300,tokens_cache_read:232100,tokens_output:33}]}]}};
  window.paths=[];window.held=[];window.block=false;window.reads=reads;
  window.contextMode=${JSON.stringify(contextRoute)};
  window.measurement={task_id:'a',session_id:'s1',provider:'anthropic',source:'bridge',live:true,measured_at:new Date(Date.now()-7200000).toISOString(),context:{used_tokens:5000,window_tokens:200000,percent:2.5,categories:{messages:3000,memory:null,tools:0,agents:100,skills:100,system:1800,compaction:null}}};
  const api={request:async(path,options)=>{
    window.paths.push(path);
    if(path.includes('/usage?'))return Response.json({task_id:'a',session_id:'s1',totals:null,turns:[],next_cursor:null});
    const response=window.contextMode==='missing'?new Response('',{status:404}):window.contextMode==='html'?new Response('<!doctype html><html>old daemon</html>',{headers:{'content-type':'text/html'}}):window.contextMode==='error'?new Response('',{status:500}):Response.json(window.measurement);
    if(window.block)return new Promise(resolve=>window.held.push(()=>resolve(response)));return response;
  },json:async(path,options)=>{window.paths.push(path);const key=path.split('/').pop().split('?')[0];if(window.block) return new Promise(resolve=>window.held.push(()=>resolve(reads[key])));return reads[key];}};
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
test('context popover shows sourced fill, remaining quotas without Info duplicates with dismissal', async () => {
  const page = await fixture();
  await page.locator('#draft').fill('keep my draft');
  await page.locator('#context-toggle').click();
  await page.locator('#composer-context:popover-open').waitFor();
  const panel = page.locator('#composer-context');
  const content = await panel.textContent();
  for (const value of ['421,200 / 1,000,000', '42.1%', '92% left', '17% left', 'Team', 'fixture@example.invalid', 'Fixture organization']) assert.ok(content.includes(value), value);
  for (const value of ['Role token totals', '234,300', '232,100', 'Account', 'Cached input', 'Cache write (1h)', 'Cache write (5m)', 'Output']) assert.equal(content.includes(value), false, value);
  assert.equal(await page.evaluate(() => window.paths.includes('/api/hera')), false);
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
  assert.equal(await page.locator('#composer-context .context-measurement').textContent(), 'ContextContext window usage is not available for this session.');
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

test('measured categories keep zeros, omit unknowns and show old measurement age without staleness', async () => {
  const page = await fixture('supported');
  await page.locator('#context-toggle').click();
  const panel=page.locator('#composer-context');
  await page.waitForFunction(()=>document.querySelector('#composer-context').textContent.includes('Messages'));
  const rows=await panel.locator('.context-row').allTextContents();
  for(const row of ['Messages3,000 tokens','Tools0 tokens','Agents100 tokens','Skills100 tokens','System1,800 tokens'])assert.ok(rows.includes(row),row);
  const content=await panel.textContent();
  assert.ok(content.includes('Measured 2h ago'));assert.equal(content.includes('Last recorded'),false);
  for(const name of ['Memory','Compaction','Role token totals'])assert.equal(content.includes(name),false);
  await page.setViewportSize({width:320,height:600});
  await page.waitForFunction(()=>{const r=document.querySelector('#composer-context').getBoundingClientRect();return r.x>=0&&r.right<=innerWidth&&r.y>=0&&r.bottom<=innerHeight;});
  await page.screenshot({path:'.context/context-categories.png'});
});
for(const mode of ['missing','html'])test(`old daemon ${mode} quietly retains bridge behavior`,async()=>{
  const page=await fixture(mode);await page.locator('#context-toggle').click();
  await page.waitForFunction(()=>document.querySelector('#composer-context').textContent.includes('421,200 / 1,000,000'));
  assert.equal(await page.locator('#composer-context').getByText('Messages',{exact:true}).count(),0);
});
test('supported empty and failed context never revive older bridge readings',async()=>{
  const page=await fixture('supported');
  await page.evaluate(async()=>{window.measurement.context=null;window.measurement.live=false;await window.composer.context.refresh();});
  assert.equal(await page.locator('.context-ring').getAttribute('data-known'),'false');
  await page.evaluate(async()=>{window.contextMode='error';await window.composer.context.refresh();});
  assert.equal(await page.locator('.context-ring').getAttribute('data-known'),'false');
});
test('same-task session event rejects held reads and fetches replacement categories',async()=>{
  const page=await fixture('supported');
  await page.evaluate(async()=>{
    window.block=true;window.composer.context.refresh();const old=window.held.splice(0);
    window.block=false;window.measurement={...window.measurement,session_id:'s2',context:{...window.measurement.context,categories:{messages:9876}}};
    window.composer.context.event({type:'task.context_changed',task_id:'a',payload:{session_id:'s2'}});
    old.forEach(resolve=>resolve());
  });
  await page.locator('#context-toggle').click();
  await page.waitForFunction(()=>document.querySelector('#composer-context').textContent.includes('9,876 tokens'));
  assert.equal(await page.locator('#composer-context').textContent().then(text=>text.includes('3,000 tokens')),false);
});

test('overflowing context region supports keyboard scroll, refresh and Escape',async()=>{
  const page=await fixture('supported');
  await page.setViewportSize({width:320,height:600});
  await page.evaluate(async()=>{
    window.measurement.context.categories={messages:3000,memory:10,tools:0,agents:100,skills:100,system:1800,compaction:20};
    await window.composer.context.refresh();
  });
  const trigger=page.locator('#context-toggle'),panel=page.getByRole('region',{name:'Context and usage',exact:true});
  await trigger.focus();await page.keyboard.press('Enter');
  await page.locator('#composer-context:popover-open').waitFor();
  assert.ok(await panel.evaluate(element=>element.scrollHeight>element.clientHeight),'Fixture must overflow');
  assert.equal(await panel.evaluate(element=>document.activeElement===element),true,'Keyboard opening must focus the scroll region');
  await page.keyboard.press('PageDown');
  await page.waitForFunction(()=>document.querySelector('#composer-context').scrollTop>0);
  await page.keyboard.press('End');
  await page.waitForFunction(()=>{const panel=document.querySelector('#composer-context');return panel.scrollTop>0&&panel.scrollTop+panel.clientHeight>=panel.scrollHeight;});
  const scroll=await panel.evaluate(element=>element.scrollTop);
  await page.evaluate(async()=>{window.measurement.context.categories.tools=123;await window.composer.context.refresh();});
  assert.equal(await panel.evaluate(element=>document.activeElement===element),true);
  assert.ok(Math.abs(await panel.evaluate(element=>element.scrollTop)-scroll)<=1,'Refresh must retain the scroll position');
  await page.keyboard.press('Escape');
  assert.equal(await panel.isVisible(),false);
  assert.equal(await trigger.evaluate(element=>document.activeElement===element),true);
  await page.keyboard.press('Enter');
  await page.keyboard.press('Tab');
  assert.equal(await page.getByRole('button',{name:'Refresh usage'}).evaluate(element=>document.activeElement===element),true,'Tab enters the usage controls');
  await page.keyboard.press('Tab');
  assert.equal(await page.locator('#attach-files').evaluate(element=>document.activeElement===element),true,'The nonmodal panel must allow normal Tab navigation');
});

test('context event bursts publish independently of a held account refresh',async()=>{
  const page=await fixture('supported');await page.clock.install();
  await page.locator('#context-toggle').click();
  await page.waitForFunction(()=>!window.composer.context.contextRead&&!window.composer.context.usageRead);
  await page.evaluate(()=>{
    const api=window.composer.context.api,read=api.json.bind(api);
    window.heldAccounts=[];
    api.json=(path,options)=>path==='/api/accounts'?new Promise(resolve=>window.heldAccounts.push(()=>resolve(window.reads.accounts))):read(path,options);
    window.composer.context.refresh();
  });
  await page.waitForFunction(()=>window.heldAccounts.length===1&&!window.composer.context.contextRead);
  const before=await page.evaluate(()=>window.paths.length);
  await page.evaluate(()=>{window.measurement.context.categories.messages=9876;});
  for(let i=0;i<10;i++){
    await page.evaluate(()=>window.composer.context.event({type:'task.context_changed',task_id:'a',payload:{session_id:'s1'}}));
    assert.ok((await page.locator('#composer-context').textContent()).includes('3,000 tokens'),'Valid categories stay displayed during refetch');
    await page.clock.runFor(20);
  }
  await page.clock.runFor(101);
  await page.waitForFunction(()=>document.querySelector('#composer-context').textContent.includes('9,876 tokens'));
  assert.deepEqual(await page.evaluate(before=>window.paths.slice(before),before),['/api/tasks/a/context']);
  assert.equal(await page.evaluate(()=>window.heldAccounts.length),1);
  await page.evaluate(()=>window.heldAccounts.splice(0).forEach(release=>release()));
});
