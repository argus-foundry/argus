import {test, expect} from '@playwright/test';

async function fixture(page) {
  await page.route('**/suggestion-fixture',route=>route.fulfill({contentType:'text/html',body:`<!doctype html><meta charset="utf-8">
  <form id="composer"><label for="draft">Message</label><textarea id="draft" placeholder="Message your agent…"></textarea><span id="composer-label"></span><button id="send">Send</button></form><div id="composer-status"></div>
  <script type="module">
  import {TaskComposer} from '/composer/composer.js';
  const bridge=(id,text)=>({task_id:id,live:true,session_id:'s',generation:'g',last_event_at:new Date().toISOString(),suggestion:text===null?null:{text,session_id:'s',generation:'g',recorded_at:new Date().toISOString()}});
  window.bridge=bridge;window.data={a:bridge('a','<b>Next step</b>'),b:bridge('b','Other task hint')};window.held=[];window.block=false;window.paths=[];window.sent=[];
  const api={json:async(path,options)=>{window.paths.push(path);if(path.endsWith('/bridge')){const data=structuredClone(window.data[path.split('/')[3]]);if(window.block)return new Promise(resolve=>window.held.push(()=>resolve(data)));return data;}return {};}};
  window.composer=new TaskComposer(async(text,task)=>window.sent.push({text,id:task.id}),api);
  window.composer.select({id:'a'});window.composer.setInteractive(true);window.ready=true;
  </script>`}));
  await page.goto('/suggestion-fixture');await page.waitForFunction(()=>window.ready);
}

test('plain-text placeholder preserves typed/restored drafts and never sends or accepts automatically',async({page})=>{
  await fixture(page);const draft=page.getByRole('textbox',{name:'Message'});
  await expect(draft).toHaveAttribute('placeholder','<b>Next step</b>');await expect(draft).toHaveValue('');
  await draft.press('Enter');await draft.press('Tab');
  expect(await page.evaluate(()=>window.sent)).toEqual([]);await expect(draft).toHaveValue('');
  await draft.fill('keep my draft');
  await page.evaluate(()=>{window.data.a=window.bridge('a','Changed hint');window.composer.suggestion.event({type:'task.suggestion_changed',task_id:'a'});});
  await expect.poll(()=>page.evaluate(()=>window.paths.filter(p=>p.endsWith('/bridge')).length)).toBeGreaterThanOrEqual(2);
  await expect(draft).toHaveValue('keep my draft');await expect(draft).toHaveAttribute('placeholder','Message your agent…');
  await page.evaluate(()=>{window.composer.select({id:'b'});window.composer.select({id:'a'});});
  await expect(draft).toHaveValue('keep my draft');await draft.fill('');await expect(draft).toHaveAttribute('placeholder','Changed hint');
  expect(await page.evaluate(()=>window.sent)).toEqual([]);
  await draft.fill('explicit message');await page.evaluate(()=>window.composer.setInteractive(true));await page.getByRole('button',{name:'Send message'}).click();
  expect(await page.evaluate(()=>window.sent)).toEqual([{text:'explicit message',id:'a'}]);
  await expect(draft).toHaveValue('');await expect(draft).toHaveAttribute('placeholder','Message your agent…');
});

test('clear, new turn, session replacement and stale/unsupported authority restore the normal hint',async({page})=>{
  await fixture(page);const draft=page.locator('#draft');await expect(draft).toHaveAttribute('placeholder','<b>Next step</b>');
  for(const patch of [{suggestion:null},{live:false},{session_id:'replaced'},{generation:'new'},{last_event_at:'2000-01-01T00:00:00Z'}]){
    await page.evaluate(patch=>{window.data.a={...window.bridge('a','hidden'),...patch};window.composer.suggestion.event({type:'task.suggestion_changed',task_id:'a',payload:{available:false}});},patch);
    await expect(draft).toHaveAttribute('placeholder','Message your agent…');
    await page.evaluate(()=>window.composer.suggestion.refresh());await expect(draft).toHaveAttribute('placeholder','Message your agent…');
  }
  await page.evaluate(()=>{window.data.a=window.bridge('a','Next turn');window.composer.suggestion.refresh();});await expect(draft).toHaveAttribute('placeholder','Next turn');
  await page.evaluate(()=>{window.data.a.suggestion=null;window.composer.suggestion.event({type:'session.started',task_id:'a'});});
  await expect(draft).toHaveAttribute('placeholder','Message your agent…');
  expect(await page.evaluate(()=>window.sent)).toEqual([]);
});

test('task switching and disconnect fence bridge reads that ignore cancellation',async({page})=>{
  await fixture(page);const draft=page.locator('#draft');await expect(draft).toHaveAttribute('placeholder','<b>Next step</b>');
  await page.evaluate(()=>{window.block=true;window.composer.suggestion.refresh();window.composer.select({id:'b'});window.held.shift()();});
  await expect(draft).toHaveAttribute('placeholder','Message your agent…');
  await page.evaluate(()=>window.held.shift()());await expect(draft).toHaveAttribute('placeholder','Other task hint');
  await page.evaluate(()=>{window.composer.suggestion.refresh();window.composer.clear();window.held.shift()();});
  await expect(draft).toHaveAttribute('placeholder','Message your agent…');await expect(draft).toBeDisabled();
});

test('real workspace SSE refreshes and clears the selected hint during a buffered snapshot',async({page})=>{
  let text='Initial suggestion', selected='', reads=0;
  await page.addInitScript(()=>{
    const nativeFetch=window.fetch.bind(window);
    window.emit=event=>window.stream.enqueue(new TextEncoder().encode(`data: ${JSON.stringify(event)}\n\n`));
    window.fetch=(url,options)=>String(url).startsWith('/api/events/stream')?Promise.resolve(new Response(new ReadableStream({start(controller){window.stream=controller;}}),{headers:{'content-type':'text/event-stream'}})):nativeFetch(url,options);
  });
  await page.route('**/api/tasks/*/bridge',route=>{
    selected=new URL(route.request().url()).pathname.split('/')[3];reads++;
    return route.fulfill({json:{task_id:selected,live:true,session_id:'s',generation:'g',last_event_at:new Date().toISOString(),suggestion:text===null?null:{text,session_id:'s',generation:'g',recorded_at:new Date().toISOString()}}});
  });
  await page.goto('/');await page.locator('#token').fill('test-token');await page.locator('#connect-button').click();
  const draft=page.getByRole('textbox',{name:'Message the selected agent'});
  await expect(draft).toHaveAttribute('placeholder','Initial suggestion');
  const before=reads;text='Fresh from GET';
  await page.evaluate(task_id=>window.emit({id:1,type:'task.suggestion_changed',task_id,payload:{available:true,text:'Never event text'}}),selected);
  await expect(draft).toHaveAttribute('placeholder','Fresh from GET');expect(reads).toBeGreaterThan(before);
  let release;
  await page.route('**/api/tasks?archived=all',async route=>{await new Promise(resolve=>release=resolve);await route.continue();});
  await page.locator('#connection-banner button').evaluate(button=>button.click());
  await expect.poll(()=>!!release).toBe(true);
  text=null;
  await page.evaluate(task_id=>window.emit({id:2,type:'task.suggestion_changed',task_id,payload:{available:false}}),selected);
  await expect(draft).toHaveAttribute('placeholder','');release();
});

test('visible hints poll at 15s, typed drafts and cleared suggestions do not poll',async({page})=>{
  await page.clock.install();await page.clock.pauseAt(new Date(Date.now()+1000));await fixture(page);const draft=page.locator('#draft');
  await expect(draft).toHaveAttribute('placeholder','<b>Next step</b>');
  const count=()=>page.evaluate(()=>window.paths.filter(path=>path.endsWith('/bridge')).length);
  expect(await count()).toBe(1);await page.clock.runFor(14999);expect(await count()).toBe(1);
  await page.clock.runFor(1);expect(await count()).toBe(2);
  await draft.fill('draft');await page.clock.runFor(5000);expect(await count()).toBe(2);
  await draft.fill('');await page.evaluate(()=>{window.block=true;});await page.clock.runFor(15000);expect(await count()).toBe(3);
  await page.clock.runFor(10001);await expect(draft).toHaveAttribute('placeholder','Message your agent…');
  await page.evaluate(()=>window.held.shift()());await page.clock.runFor(60000);expect(await count()).toBe(3);
});

test('Settings pauses suggestion polling and returning refreshes the visible composer',async({page})=>{
  await page.clock.install();await page.clock.pauseAt(new Date(Date.now()+1000));
  let refreshes=0;
  page.on('console',message=>{if(message.text().includes('[workspace:composer-suggestion] refreshed'))refreshes++;});
  await page.route('**/api/tasks/*/bridge',route=>{
    const task_id=new URL(route.request().url()).pathname.split('/')[3];
    return route.fulfill({json:{task_id,live:true,session_id:'s',generation:'g',last_event_at:new Date().toISOString(),suggestion:{text:'Visible hint',session_id:'s',generation:'g',recorded_at:new Date().toISOString()}}});
  });
  await page.goto('/');await page.locator('#token').fill('test-token');await page.locator('#connect-button').click();
  const draft=page.getByRole('textbox',{name:'Message the selected agent'});
  await expect(draft).toHaveAttribute('placeholder','Visible hint');
  await page.locator('#workspace-settings').click();await expect(draft).not.toBeVisible();
  const before=refreshes;await page.clock.runFor(15001);expect(refreshes).toBe(before);
  await page.keyboard.press('Escape');await expect(draft).toBeVisible();
  await expect.poll(()=>refreshes).toBeGreaterThan(before);await expect(draft).toHaveAttribute('placeholder','Visible hint');
  await page.locator('#tab-review').click();await expect(draft).toBeVisible();
  const reviewing=refreshes;await page.clock.runFor(15000);await expect.poll(()=>refreshes).toBeGreaterThan(reviewing);
});
