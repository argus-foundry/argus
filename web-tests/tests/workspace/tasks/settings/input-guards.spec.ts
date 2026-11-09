import {test,expect} from '@playwright/test';
import {actorPage} from '../../_lifecycle-fixture.ts';
const cleanup=[];
test.afterEach(async()=>{for(const dispose of cleanup.splice(0))await dispose();});
for(const operation of ['resume','restart'])test(`${operation} revival cancel and confirm preserve explicit request boundaries`,async({browser})=>{
 const page=await actorPage(browser,cleanup,'<button id="opener">Resume</button>');const calls=[];
 await page.route('**/api/tasks/a/'+operation,async route=>{const raw=route.request().postData();calls.push(raw?JSON.parse(raw):null);await route.fulfill({status:calls.length<=2?409:200,json:calls.length<=2?{code:'hera_closeout',can_force_revive:true}:{status:'ok'}});});
 await page.evaluate(async operation=>{const {Api}=await import('/core/api.js');const {reviveTask}=await import('/tasks/task-revival.js');document.querySelector('#opener').onclick=()=>{window.pending=reviveTask(new Api(),{id:'a',name:'Worker'},operation,null,new AbortController().signal).catch(error=>error.message);};},operation);
 await page.locator('#opener').click();await expect(page.getByRole('dialog')).toBeVisible();await expect(page.getByRole('button',{name:'Cancel',exact:true})).toBeFocused();await page.keyboard.press('Escape');await expect(page.getByRole('dialog')).toHaveCount(0);expect(calls).toEqual([null]);
 await page.locator('#opener').click();await page.getByRole('button',{name:'Revive task',exact:true}).click();await expect(page.getByRole('dialog')).toHaveCount(0);expect(calls).toEqual([null,null,{force_revive:true}]);
});
test('authoritative archived input fences raw writes until restored without replay',async({browser})=>{
 const page=await actorPage(browser,cleanup,'<div></div>');let posts=0;
 await page.route('**/api/backends',route=>route.fulfill({json:{backends:[]}}));
 await page.route('**/api/tasks/a/input',route=>{posts++;return route.fulfill({status:409,json:{code:'task_archived'}});});
 await page.evaluate(async()=>{const {Api}=await import('/core/api.js');const {TerminalInput}=await import('/terminal/terminal-input.js');window.task={id:'a',status:'in_progress',live:true};const api=new Api(()=>window.task);api.inputRejected=()=>window.task.archive_pending=true;window.input=new TerminalInput(api,window.task,new AbortController().signal,()=>true);await window.input.raw('rejected').catch(()=>{});await window.input.raw('queued').catch(()=>{});window.task={...window.task,archive_pending:false,archived:true};await window.input.raw('still archived').catch(()=>{});});expect(posts).toBe(1);
 await page.route('**/api/tasks/a/input',route=>{posts++;return route.fulfill({json:{status:'ok'}});});
 await page.evaluate(async()=>{window.task.archived=false;await window.input.raw('new deliberate send');});expect(posts).toBe(2);
});
test('archive rejection retains draft and uploaded attachment through failed and still-archived reconciliation',async({browser})=>{
 const html='<header class="workspace-header"></header><main><div id="agent-panel"></div><form id="composer"><textarea id="draft"></textarea><button id="send">Send</button></form><span id="composer-label"></span><span id="composer-status"></span></main>';
 const page=await actorPage(browser,cleanup,html);let posts=0;
 await page.route('**/api/**',route=>route.fulfill({status:404,json:{}}));
 await page.route('**/api/backends',route=>route.fulfill({json:{backends:[]}}));
 await page.route('**/api/tasks/a',route=>route.fulfill({json:{id:'a',status:'in_progress',live:true}}));
 await page.route('**/api/tasks/a/upload',route=>route.fulfill({json:{paths:['./.context/note.txt']}}));
 await page.route('**/api/tasks/a/input',route=>{posts++;return route.fulfill({status:409,json:{code:'task_archived'}});});
 await page.evaluate(async()=>{
  const {Api}=await import('/core/api.js'),{TerminalInput}=await import('/terminal/terminal-input.js'),{TaskComposer}=await import('/composer/composer.js'),{WorkspaceEvents}=await import('/workspace/events/workspace-events.js'),{patchEvent}=await import('/workspace/events/workspace-events-policy.js');
  window.tasks=[{id:'a',name:'Task',status:'in_progress',live:true}];const api=new Api(id=>window.tasks.find(task=>task.id===id));const input=new TerminalInput(api,window.tasks[0],new AbortController().signal,()=>true);
  window.composer=new TaskComposer((text,task,options)=>input.send(text,options),api);window.composer.select(window.tasks[0]);window.composer.setInteractive(true);
  window.events=new WorkspaceEvents(api,{refresh:()=>{},event:event=>{window.tasks=patchEvent(window.tasks,event).tasks;window.composer.task=window.tasks[0];window.composer.setInteractive(!window.tasks[0].archive_pending&&!window.tasks[0].archived);}});api.inputRejected=id=>window.events.rejectInput(id);
  window.reconcile=archived=>{window.events.beginSnapshot();window.tasks=window.events.snapshot([{...window.tasks[0],archived}],window.tasks);window.composer.task=window.tasks[0];window.composer.setInteractive(!window.tasks[0].archive_pending&&!window.tasks[0].archived);};
  window.composer.attachments.add([new File(['note'],'note.txt',{type:'text/plain'})]);
 });
 await page.locator('.attachment-chip[role=listitem]').waitFor();await expect(page.locator('.attachment-chip[role=listitem]')).toContainText('note.txt');await page.locator('#draft').fill('keep this message');await expect(page.locator('#send')).toBeEnabled();await page.locator('#send').click();await expect(page.locator('#composer-status')).toContainText('task was archived');expect(posts).toBe(1);
 await expect(page.locator('#draft')).toHaveValue('keep this message');await expect(page.locator('.attachment-chip[role=listitem]')).toContainText('note.txt');await expect(page.locator('#send')).toBeDisabled();
 // A failed snapshot cannot call snapshot(), so the pending fence stays intact.
 expect(await page.evaluate(()=>window.tasks[0].archive_pending)).toBe(true);
 await page.evaluate(()=>window.reconcile(true));await expect(page.locator('#send')).toBeDisabled();await expect(page.locator('#draft')).toHaveValue('keep this message');
 await page.evaluate(()=>window.reconcile(false));await expect(page.locator('#send')).toBeEnabled();expect(posts).toBe(1);await expect(page.locator('.attachment-chip[role=listitem]')).toContainText('note.txt');
 await page.evaluate(()=>{window.events.stop();window.composer.clear();});
});
test('actual app archive rejection fences primary and split through held and failed snapshots without replay',async({browser})=>{
 const page=await browser.newPage({viewport:{width:1440,height:900}});cleanup.push(()=>page.close());
 const tasks=['a','b'].map(id=>({id,name:id==='a'?'Archive example':'Other task',project:'demo',status:'in_progress',live:true,idle:false,archived:false}));
 const posts=[],held=[];let mode='ok';
 await page.addInitScript(()=>{
  window.EventSource=class{constructor(){queueMicrotask(()=>this.onopen?.());}addEventListener(){}close(){}};
  const original=window.fetch.bind(window);window.fetch=(url,options)=>String(url).startsWith('/api/events/stream')?Promise.resolve(new Response(new ReadableStream(),{headers:{'Content-Type':'text/event-stream'}})):original(url,options);
 });
 await page.route('**/api/**',route=>{
  const path=new URL(route.request().url()).pathname;
  if(path==='/api/tasks'){
   if(mode==='hold'){held.push(route);return;}
   return route.fulfill({json:{tasks}});
  }
  if(path.endsWith('/input')){posts.push({path,body:route.request().postData()});return route.fulfill({status:409,json:{code:'task_archived'}});}
  if(path.endsWith('/upload'))return route.fulfill({json:{paths:['./.context/note.txt']}});
  if(path==='/api/tasks/a/unarchive'){tasks[0].archived=false;return route.fulfill({json:{status:'ok'}});}
  if(path.match(/^\/api\/tasks\/[ab](\/raw)?$/))return route.fulfill({json:tasks.find(task=>task.id===path.split('/')[3])});
  if(path.endsWith('/size'))return route.fulfill({json:{cols:80,rows:24}});
  if(path.endsWith('/output'))return route.fulfill({body:'\x1b[?2004h',headers:{'X-Output-Total':'8'}});
  if(path==='/api/projects/full')return route.fulfill({json:{projects:[{name:'demo'}]}});
  if(path==='/api/backends')return route.fulfill({json:{backends:[]}});
  return route.fulfill({json:{}});
 });
 await page.goto(process.env.WORKSPACE_TEST_URL);await page.locator('#token').fill('fixture-token');await page.locator('#connect-button').click();
 await expect(page.locator('#terminal-state')).toHaveText('Live terminal');
 await page.getByLabel('Choose attachments',{exact:true}).setInputFiles({name:'note.txt',mimeType:'text/plain',buffer:Buffer.from('isolated fixture')});
 await expect(page.locator('.attachment-chip[role=listitem]')).toContainText('Ready');await page.locator('#draft').fill('retained actual app draft');mode='hold';
 await page.locator('#send').click();await expect(page.locator('#composer-status')).toContainText('task was archived');
 await expect(page.locator('#send')).toBeDisabled();await expect.poll(()=>held.length).toBe(1);
 await expect(page.locator('#draft')).toHaveValue('retained actual app draft');await expect(page.locator('.attachment-chip[role=listitem]')).toContainText('note.txt');
 await page.locator('#terminal .xterm-helper-textarea').press('x');expect(posts).toHaveLength(1);
 await page.locator('#split-toggle').click();await page.locator('[data-task-id="b"]').first().click();await page.getByLabel('Second task',{exact:true}).selectOption('a');
 await expect(page.locator('#secondary-terminal-state')).toHaveText('Recorded output');await page.locator('#secondary-terminal .xterm-helper-textarea').press('x');expect(posts).toHaveLength(1);
 mode='ok';tasks[0].archived=true;await held.shift().fulfill({status:503,json:{}});
 await expect(page.locator('#connection-banner')).toContainText('Task refresh failed');
 await page.getByRole('button',{name:'Retry connection',exact:true}).click();await expect(page.locator('#secondary-terminal-state')).toHaveText('Recorded output');
 await page.locator('#archive-filter').click();await page.getByRole('button',{name:'Restore Archive example',exact:true}).click();await page.getByRole('button',{name:'Restore task',exact:true}).click();
 await expect(page.locator('#archive-dialog')).toHaveCount(0);await page.locator('#active-filter').click();await page.locator('[data-task-id="a"]').first().click();
 await expect(page.locator('#send')).toBeEnabled();await expect(page.locator('#draft')).toHaveValue('retained actual app draft');await expect(page.locator('.attachment-chip[role=listitem]')).toContainText('note.txt');expect(posts).toHaveLength(1);
});
