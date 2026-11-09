import {test,expect} from '../../../web-tests/node_modules/@playwright/test/index.mjs';
async function fixture(page){
 const errors=[];page.on('pageerror',error=>errors.push(error.message));
 await page.route('**/mail-fixture',route=>route.fulfill({contentType:'text/html',body:`
 <link rel="stylesheet" href="/workspace/styles.css"><div id="workspace"><div id="rail"></div><aside id="tools">
 <div class="tool-tabs" role="tablist"><button role="tab" id="tool-changes" data-tool="changes">Changes</button><button role="tab" id="tool-artifacts" data-tool="artifacts">Artifacts</button><button role="tab" id="tool-status" data-tool="status">Info</button><button role="tab" id="tool-messages" data-tool="messages">Agent messages</button><button role="tab" id="tool-inbox" data-tool="inbox" hidden>Inbox</button></div><section id="tool-panel" role="tabpanel"></section></aside></div><form id="composer"><textarea id="draft">existing draft</textarea></form>
 <script type="module">
 import {TaskTools} from '/tasks/tools.js';import {TaskComposer} from '/composer/composer.js';import {TaskNavigation} from '/workspace/rail/tasks.js';
 const task={id:'a',name:'Coordinator',status:'complete',archived:false};
 const role={role_id:2,kind:'coordinator',live:true,task_id:'a',board_unread:1};
 const roster={orchestrators:[{id:1,roles:[role]}]};
 window.item={id:7,role_id:2,kind:'decision',subject:'Choose path',body:'Decision body',updated_at:'2026-11-08T00:00:00Z',read:false};window.calls=[];window.conflict=false;
 const api={json:async(path,options={})=>{
 calls.push({path,method:options.method||'GET',body:options.body?JSON.parse(options.body):undefined});
 if(path.includes('/hera-messages'))return {messages:path.includes('before_id')?[{source:'hera',id:1,from_name:'(deleted role)',to_name:'em',tldr:'Ended binding',body:'older body'}]:[{source:'hera',id:2,from_name:'worker',to_name:'em',tldr:'Current',body:'new body',read_at:null}],next_before_id:path.includes('before_id')?null:2};
 if(path.endsWith('/board'))return {items:[item],unread_count:item.read?0:1};
 if(path.includes('/board/')){if(options.method!==(path.endsWith('read-state')?'PUT':'POST'))throw new Error('Wrong board method');if(window.holdWrites)await new Promise(resolve=>(window.heldWrites ||= []).push(resolve));if(window.writeError)throw Object.assign(new Error('failed'),{status:500});if(conflict){window.conflict=false;item={...item,subject:'Changed decision',updated_at:'2026-11-08T00:01:00Z',read:false};throw Object.assign(new Error('conflict'),{status:409});}if(path.endsWith('dismiss'))item={...item,resolved:true};else item={...item,read:JSON.parse(options.body).read};return {item,unread_count:item.read||item.resolved?0:1};}
 return {files:[]};}};
 const composer=Object.assign(Object.create(TaskComposer.prototype),{task,drafts:new Map(),render(){}});
 const tools=window.tools=new TaskTools(api,{}, {reply:(task,item)=>composer.reply(task,item),changed:(id,count)=>{role.board_unread=count;window.count=count;}});
 tools.updateRoster(roster);tools.selectTask(task);
 const navigation=Object.assign(Object.create(TaskNavigation.prototype),{tasks:[task],roster,roles:new Map(),hierarchy:{attention:new Set(),failed:new Set(),parents:new Map()},pinned:new Set(),actions:{openInbox:()=>{tools.selectTask(task);tools.selectTab('tool-inbox');}},select(){}});
 document.querySelector('#rail').append(navigation.taskItem({task,children:[],needsInput:false},'',new Map()));
 window.ready=true;
 </script>`}));
 await page.goto('/mail-fixture');await page.waitForFunction(()=>window.ready);return errors;
}
test('message history stays read only across refresh and older paging',async({page})=>{
 const errors=await fixture(page);await page.getByRole('tab',{name:'Agent messages'}).click();
 await expect(page.getByText('worker → em · Current')).toBeVisible();await page.getByRole('button',{name:'Older messages'}).click();
 await expect(page.getByText('(deleted role) → em · Ended binding')).toBeVisible();
 expect(await page.locator('.agent-message').evaluateAll(nodes=>nodes.map(n=>n.dataset.messageKey))).toEqual(['hera:1','hera:2']);
 await page.getByRole('button',{name:'Refresh',exact:true}).click();
 const calls=await page.evaluate(()=>calls);expect(calls.every(c=>c.method==='GET')).toBe(true);expect(calls.some(c=>/inbox|ack|input/.test(c.path))).toBe(false);
 await expect(page.locator('#draft')).toHaveValue('existing draft');expect(errors).toEqual([]);
});
test('Inbox glyph opens coordinator mail, revision conflict stays unread and reply only drafts',async({page})=>{
 const errors=await fixture(page);const mail=page.getByRole('button',{name:'Open Inbox for Coordinator'});await expect(mail).toBeVisible();expect(await mail.textContent()).toBe('');
 await mail.click();await expect(page.getByRole('heading',{name:'Inbox',exact:true})).toBeVisible();
 await page.evaluate(()=>window.conflict=true);await page.getByRole('button',{name:'Unread · Choose path',exact:true}).click();
 await expect(page.getByRole('alert')).toContainText('Item changed');await expect(page.getByRole('button',{name:'Unread · Changed decision'})).toHaveAttribute('aria-expanded','false');
 await page.getByRole('button',{name:'Unread · Changed decision'}).click();await expect(page.getByRole('button',{name:'Reply in composer'})).toBeEnabled();
 await page.getByRole('button',{name:'Reply in composer'}).click();await expect(page.locator('#draft')).toHaveValue('existing draft\n\nReply to Inbox #7: Changed decision\n');
 await page.getByRole('button',{name:'Mark unread'}).click();await expect(page.getByRole('button',{name:'Unread · Changed decision'})).toBeVisible();
 await page.getByRole('button',{name:'Unread · Changed decision'}).click();await page.getByRole('button',{name:'Dismiss',exact:true}).click();await expect(page.getByText('No open Inbox items.')).toBeVisible();
 const writes=await page.evaluate(()=>calls.filter(c=>c.method!=='GET'));expect(writes.every(c=>c.body.expected_updated_at)).toBe(true);expect(writes.some(c=>c.path.endsWith('/input'))).toBe(false);
 await page.evaluate(()=>{tools.updateRoster({});tools.selectTask({id:'ordinary'});});await expect(page.getByRole('tab',{name:'Inbox',exact:true})).toBeHidden();expect(errors).toEqual([]);
});

test('real workspace shell routes the mail glyph and keeps draft after SSE reconciliation',async({page})=>{
 const errors=[],writes=[];let boardReads=0,holdUsage=false,releaseUsage;let usage={task_id:'coord',session_id:'s',totals:{input_tokens:10},turns:[],next_cursor:null};page.on('pageerror',error=>errors.push(error.message));
 const task={id:'coord',name:'Shell coordinator',project:'fixture',status:'complete',live:false,archived:false,pinned:false,backend:'codex'};
 let item={id:9,role_id:4,kind:'decision',subject:'Shell decision',body:'Review this',updated_at:'2026-11-08T00:00:00Z',read:false};
 await page.addInitScript(()=>{
  localStorage.setItem('argus-workspace-token','fixture');
  const original=window.fetch.bind(window);
  window.fetch=(url,options)=>String(url).startsWith('/api/events/stream')?Promise.resolve(new Response(new ReadableStream({start(controller){window.closeEvents=()=>controller.close();window.emit=e=>controller.enqueue(new TextEncoder().encode('data: '+JSON.stringify(e)+'\n\n'));}}),{headers:{'content-type':'text/event-stream'}})):original(url,options);
 });
 await page.route('**/api/**',async route=>{
  const request=route.request(),path=new URL(request.url()).pathname;
  if(request.method()!=='GET'){writes.push(path);if(path.endsWith('read-state')){expect(request.method()).toBe('PUT');}else expect(request.method()).toBe('POST');if(path.endsWith('read-state'))item={...item,read:request.postDataJSON().read};return route.fulfill({json:{item,unread_count:0}});}
  if(path==='/api/tasks')return route.fulfill({json:{tasks:[task]}});
  if(path==='/api/hera')return route.fulfill({json:{orchestrators:[{id:1,name:'fixture',kanban_status:'active',roles:[{role_id:4,kind:'coordinator',name:'em',live:true,task_id:'coord',board_unread:item.read?0:1}]}]}});
  if(path.endsWith('/board')){boardReads++;return route.fulfill({json:{items:[item],unread_count:item.read?0:1}});}
  if(path.endsWith('/raw'))return route.fulfill({json:task});
  if(path.endsWith('/context'))return route.fulfill({json:{task_id:'coord',session_id:'s',provider:'openai',source:'codex_rollout',live:false,context:null}});
  if(path.endsWith('/usage')){const captured=structuredClone(usage);if(holdUsage){holdUsage=false;return new Promise(resolve=>releaseUsage=async()=>{await route.fulfill({json:captured}).catch(()=>{});resolve();});}return route.fulfill({json:usage});}
  if(path.endsWith('/output'))return route.fulfill({body:'',headers:{'X-Output-Total':'0'}});
  if(path.endsWith('/size'))return route.fulfill({status:404,json:{error:'offline'}});
  if(path==='/api/accounts')return route.fulfill({json:[]});
  return route.fulfill({json:{projects:[],backends:[],files:[],entries:[],artifacts:[]}});
 });
 await page.goto('/');await expect(page.locator('.task-row[data-task-id="coord"]')).toBeVisible();
 await page.locator('#draft').fill('keep shell draft');
 const glyph=page.getByRole('button',{name:'Open Inbox for Shell coordinator'});await glyph.focus();await page.evaluate(()=>window.emit({id:1,type:'session.idle',task_id:'coord'}));await expect(glyph).toBeFocused();await glyph.click();
 await expect(page.getByRole('tab',{name:'Inbox',exact:true})).toHaveAttribute('aria-selected','true');
 await page.getByRole('button',{name:'Unread · Shell decision'}).click();await page.getByRole('button',{name:'Reply in composer'}).click();
 await expect(page.locator('#draft')).toHaveValue('keep shell draft\n\nReply to Inbox #9: Shell decision\n');
 await expect(page.getByRole('button',{name:'Open Inbox for Shell coordinator'})).toHaveCount(0);
 await page.evaluate(()=>window.emit({id:2,type:'hera.board_changed',payload:{role_id:4,item_id:9,unread_count:0,operation:'read'}}));
 await expect(page.locator('#draft')).toHaveValue('keep shell draft\n\nReply to Inbox #9: Shell decision\n');
 const before=boardReads;item={...item,body:'Changed while offline',updated_at:'2026-11-08T00:02:00Z'};
 await page.evaluate(()=>window.closeEvents());await page.getByRole('button',{name:'Retry connection',exact:true}).click();
 await expect.poll(()=>boardReads).toBeGreaterThan(before);await expect(page.locator('.inbox-item')).toContainText('Changed while offline');
 await page.locator('#context-toggle').click();await expect(page.locator('.session-usage')).toContainText('Session s');
 usage={...usage,totals:{input_tokens:42}};await page.evaluate(()=>window.emit({id:3,type:'task.usage_changed',task_id:'coord',payload:{session_id:'s'}}));
 await expect(page.locator('.session-usage')).toContainText('42');
 holdUsage=true;await page.getByRole('button',{name:'Refresh usage',exact:true}).click();await expect.poll(()=>!!releaseUsage).toBe(true);
 usage={...usage,session_id:'replacement',totals:{input_tokens:99}};await page.evaluate(()=>window.emit({id:4,type:'task.usage_changed',task_id:'coord',payload:{session_id:'replacement'}}));
 await expect(page.locator('.session-usage')).toContainText('Session replacement');await releaseUsage();await expect(page.locator('.session-usage')).toContainText('99');
 expect(writes).toEqual(['/api/hera/roles/4/board/9/read-state']);expect(errors).toEqual([]);
});

test('Inbox supports multiple coordinator bindings and keyboard tabs skip unavailable Inbox',async({page})=>{
 const errors=await fixture(page);
 await page.evaluate(()=>{tools.updateRoster({orchestrators:[{id:1,roles:[{role_id:2,name:'first',kind:'coordinator',live:true,task_id:'a'},{role_id:3,name:'second',kind:'coordinator',live:true,task_id:'a'}]}]});tools.selectTab('tool-inbox');});
 await expect(page.getByRole('combobox',{name:'Coordinator Inbox'})).toBeVisible();await page.getByRole('combobox',{name:'Coordinator Inbox'}).selectOption('3');
 await expect.poll(()=>page.evaluate(()=>calls.at(-1).path)).toBe('/api/hera/roles/3/board');
 await page.evaluate(()=>{tools.updateRoster({});tools.selectTask({id:'ordinary'});});
 await page.getByRole('tab',{name:'Agent messages'}).focus();await page.keyboard.press('ArrowRight');await expect(page.getByRole('tab',{name:'Changes',exact:true})).toBeFocused();expect(errors).toEqual([]);
});

test('older history pages preserve the visible message position',async({page})=>{
 const errors=await fixture(page);
 await page.evaluate(()=>{
  const original=tools.api.json;tools.api.json=async(path,options)=>path.includes('/hera-messages')?{messages:Array.from({length:100},(_,i)=>({source:'hera',id:i+(path.includes('before_id')?1:101),from_name:'worker',to_name:'em',tldr:'Message '+i,body:'body'})),next_before_id:path.includes('before_id')?null:101}:original(path,options);
  const panel=document.querySelector('#tool-panel');Object.assign(panel.style,{height:'200px',overflow:'auto'});tools.selectTab('tool-messages');
 });
 const older=page.getByRole('button',{name:'Older messages'});await older.scrollIntoViewIfNeeded();
 const target=page.locator('[data-message-key="hera:200"]');const before=(await target.boundingBox()).y;
 await older.click();await expect(page.locator('.agent-message')).toHaveCount(200);const after=(await target.boundingBox()).y;
 expect(Math.abs(after-before)).toBeLessThan(3);expect(errors).toEqual([]);
});

for(const outcome of ['success','failure','conflict'])test(`Inbox preserves action focus through delayed ${outcome}`,async({page})=>{
 const errors=await fixture(page);await page.getByRole('tab',{name:'Inbox',exact:true}).click();
 await page.evaluate(outcome=>{window.holdWrites=true;window.writeError=outcome==='failure';window.conflict=outcome==='conflict';},outcome);
 const subject=page.getByRole('button',{name:'Unread · Choose path',exact:true});await subject.focus();await page.keyboard.press('Enter');await page.waitForFunction(()=>window.heldWrites?.length);
 await page.evaluate(()=>{window.holdWrites=false;window.heldWrites.shift()();});
 const focused=outcome==='conflict'?page.getByRole('button',{name:'Unread · Changed decision'}):page.getByRole('button',{name:outcome==='success'?'Choose path':'Unread · Choose path',exact:true});
 await expect(focused).toBeFocused();
 if(outcome==='success'){await page.getByRole('button',{name:'Dismiss',exact:true}).focus();await page.keyboard.press('Enter');await expect(page.getByRole('button',{name:'Refresh Inbox'})).toBeFocused();await expect(page.getByRole('status')).toHaveText('No open Inbox items.');}
 expect(errors).toEqual([]);
});
