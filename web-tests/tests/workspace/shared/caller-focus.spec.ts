import {test,expect} from '@playwright/test';

async function setup(page,surface){
 await page.goto('/');
 await page.evaluate(async surface=>{
  document.body.replaceChildren();
  const {node,button}=await import('/shared/dom.js');
  const header=node('header','workspace-header');document.body.append(header);
  const outside=button('Outside',()=>{});document.body.append(outside);
  const main=node('main');main.id='main';document.body.append(main);
  const draft=node('textarea');draft.id='draft';draft.setAttribute('aria-label','Draft');main.append(draft);
  const filter=button('Active filter',()=>{});filter.id='active-filter';main.append(filter);
  const tab=button('Agent tab',()=>{});tab.id='tab-agent';main.append(tab);
  const host=node('section','settings-section');main.append(host);
  const back=button('Settings Back',()=>{});back.className='settings-back';back.hidden=true;main.append(back);
  window.pending=[];window.fail=false;window.hold=false;
  const api={invalidateReads(){},async json(path,config={}){
   if(window.hold)await new Promise((resolve,reject)=>{window.pending.push({resolve,reject,signal:config.signal});config.signal?.addEventListener('abort',()=>reject(config.signal.reason),{once:true});});
   if(window.fail)throw new Error('Unavailable');
   if(path==='/api/settings')return {defaults:{},sandbox:{available:true}};
   if(path==='/api/backends')return {backends:[]};
   if(path==='/api/projects/full')return {projects:surface==='schedule'?[{name:'demo'}]:[]};
   if(path==='/api/schedules')return {schedules:[{id:'a',name:'Daily',project:'demo',schedule:'0 9 * * *',prompt:'Task',enabled:true}]};
   if(path==='/api/source-path')return {path:'/tmp/demo'};
   if(path.startsWith('/api/accounts'))return {accounts:[]};
   return {};
  }};
  const context={api,host,status(){},busy(){},changed(){}};
  window.api=api;window.host=host;
  if(surface==='task-settings'){
   const {TaskSettingsView}=await import('/tasks/settings/task-settings.js');
   window.view=new TaskSettingsView(api,{beforeOpen(){},history:{push(){}},opened(){},changed(){},closed(){}});
   await window.view.open({id:'a',name:'Daily'},{history:false});window.fail=true;await window.view.load();window.fail=false;
  }
  if(surface==='events'){
   const {WorkspaceEvents}=await import('/workspace/events/workspace-events.js');
   const request=()=>new Promise((resolve,reject)=>window.pending.push({resolve:()=>resolve(new Response(new ReadableStream(),{headers:{'content-type':'text/event-stream'}})),reject}));
   window.view=new WorkspaceEvents({request},{refresh:async()=>{if(window.fail)window.view.problem('Snapshot unavailable');else window.view.healthy();}});
   window.view.running=true;window.view.problem('Snapshot unavailable');
  }
  if(surface==='general'){const {GeneralSettings}=await import('/settings/settings-general.js');window.view=new GeneralSettings(api,host,()=>{},()=>{},x=>x,()=>{});}
  if(surface==='sandbox'){const {SandboxSettings}=await import('/settings/sandbox/settings-sandbox.js');window.view=new SandboxSettings(context);}
  if(surface==='update'){const {UpdateSettings}=await import('/settings/system/settings-update.js');window.view=new UpdateSettings(context);}
  if(surface==='schedule'){const {ScheduleSettings}=await import('/settings/schedules/settings-schedules.js');window.view=new ScheduleSettings(context);}
  if(surface==='maintenance'){const {MaintenanceSettings}=await import('/settings/system/settings-maintenance.js');window.view=new MaintenanceSettings(context);}
  if(surface==='new-task'){const {NewTaskView}=await import('/tasks/create/new-task.js');window.view=new NewTaskView(api,()=>{},()=>{});window.view.open('demo',()=>{});await window.view.loadBackends('');}
  if(surface==='download'){
   const panel=node('section');panel.id='review-panel';main.append(panel);const notice=node('p');notice.id='notice';main.append(notice);
   const {TaskReview}=await import('/review/review.js');api.request=async(path,config)=>{await api.json(path,config);return new Response('Report');};
   window.view=new TaskReview(api,()=>{});window.view.task={id:'a'};await window.view.openArtifact(window.view.task,{filename:'report.txt',name:'Report',size:6,type:'text'});
  }
  if(surface==='directory'){
   const {TaskChanges}=await import('/tasks/changes/task-changes.js');const container=node('div'),row=button('Directory',()=>window.attempt=window.view.expandDirectory({path:'folder'},row,container));container.append(row);host.append(container);
   window.view=Object.assign(Object.create(TaskChanges.prototype),{api,task:{id:'a'},signal:new AbortController().signal,counts:{results:new Map()},observeRows(){}});
  }
  if(surface==='sessions'){
   const {ClaudeSessions}=await import('/tasks/settings/claude-sessions.js');
   api.json=async(path,config={})=>{if(config.method)return new Promise((resolve,reject)=>window.pending.push({resolve,reject}));return {sessions:[{id:'one',title:'Other session'}]};};window.view=new ClaudeSessions(api,()=>{});await window.view.open({id:'a',name:'Daily'});
  }
  if(surface==='primary'){
   for(const id of ['terminal-state','terminal-feedback','fit-session','reconnect-terminal']){const item=node(id.includes('session')||id.includes('reconnect')?'button':'div','',id);item.id=id;main.append(item);}
   const {WorkspaceTaskView}=await import('/tasks/task-view.js');window.view=new WorkspaceTaskView({setInteractive(){}},{});
   window.view.state({message:'Error',kind:'error',interactive:true},{id:'a'});
  }
  if(surface==='secondary'){
   for(const id of ['agent-panel','terminal','fit-session']){const item=node('div');item.id=id;main.append(item);}
   const {SplitWorkspace}=await import('/workspace/split/split-workspace.js');
   window.view=new SplitWorkspace(()=>({releaseSecondary(){},attachSecondary(task,host,state){window.secondaryState=state;state({message:'Error',kind:'error',interactive:true});}}));window.view.enabled=true;window.view.choices.set('a','b');
   window.view.select({id:'a'},[{id:'a',name:'Daily'},{id:'b',name:'Weekly'}],{});
  }
  if(surface==='composer'){
   const wrap=node('div','composer-wrap'),form=node('form');form.id='composer';wrap.append(form);main.append(wrap);form.append(draft);
   for(const id of ['send','attach-files','composer-label','composer-status']){const item=node(['send','attach-files'].includes(id)?'button':'div','',id);item.id=id;form.append(item);}
   const toggle=button('',()=>{});toggle.id='context-toggle';toggle.append(node('span','context-ring'));form.append(toggle);
   const panel=node('div');panel.id='agent-panel';main.append(panel);
   const {TaskComposer}=await import('/composer/composer.js');window.view=new TaskComposer(async()=>{},api);window.view.select({id:'a',live:true});window.view.setInteractive(true);draft.value='Message';window.view.render();
  }
  if(surface==='selection'){
   const nav=node('nav');nav.id='navigation';main.append(nav);nav.append(filter);
   for(const id of ['projects-warning','search-clear','search','archive-filter','active-count','archive-count','task-count','task-groups']){const item=node(id.endsWith('filter')?'button':'div','',id);item.id=id;nav.append(item);}
   const {TaskNavigation}=await import('/workspace/rail/tasks.js');window.view=new TaskNavigation(()=>{});window.view.update([{id:'a',name:'Daily'}]);window.view.selection.enter();window.view.selection.set(window.view.tasks,true);
  }
 },surface);
}
async function release(page,failed=false){await page.evaluate(failed=>{window.hold=false;window.pending.splice(0).forEach(item=>failed?item.reject(new Error('Unavailable')):item.resolve({}));},failed);}

for(const outside of [false,true])test(`task-settings request timeout keeps mounted view ownership outside=${outside}`,async({page})=>{
 await setup(page,'task-settings');
 await page.evaluate(()=>{
  const timeout=AbortSignal.timeout.bind(AbortSignal);window.timeoutAbort=new AbortController();AbortSignal.timeout=ms=>ms===15000?window.timeoutAbort.signal:timeout(ms);
  window.view.api.json=(path,{signal})=>new Promise((resolve,reject)=>signal.addEventListener('abort',()=>reject(signal.reason),{once:true}));
 });
 const retry=page.getByRole('button',{name:'Retry task settings',exact:true});await retry.focus();await retry.press('Enter');
 if(outside)await page.getByRole('button',{name:'Back to task',exact:true}).focus();
 await page.evaluate(()=>window.timeoutAbort.abort(new DOMException('Timed out','TimeoutError')));
 await expect(retry).toHaveCount(1);await expect(outside?page.getByRole('button',{name:'Back to task',exact:true}):retry).toBeFocused();
});
test('task-settings actual request deadline restores Retry',async({page})=>{
 await setup(page,'task-settings');await page.evaluate(()=>{window.view.api.json=(path,{signal})=>new Promise((resolve,reject)=>signal.addEventListener('abort',()=>reject(signal.reason),{once:true}));});
 const retry=page.getByRole('button',{name:'Retry task settings',exact:true}),old=await retry.elementHandle();await retry.focus();await retry.press('Enter');
 await page.waitForFunction(item=>!item.isConnected,old,{timeout:20000});await expect(retry).toBeFocused();
});
for(const mode of ['healthy','stream','manual','failed'])for(const outside of [false,true])test(`connection ${mode} recovery respects ownership outside=${outside}`,async({page})=>{
 await setup(page,'events');const retry=page.getByRole('button',{name:'Retry connection',exact:true});await retry.focus();
 if(mode==='healthy'){
  if(outside)await page.getByRole('button',{name:'Outside',exact:true}).focus();await page.evaluate(()=>window.view.healthy());
 }else{
  await page.evaluate(mode=>{window.attempt=mode==='stream'?window.view.open():window.view.reconnect();},mode);await page.waitForFunction(()=>window.pending.length>0);
  if(outside)await page.getByRole('button',{name:'Outside',exact:true}).focus();await page.evaluate(async mode=>{window.fail=mode==='failed';window.pending.splice(0).forEach(item=>mode==='failed'?item.reject(new Error('Failed')):item.resolve());await window.attempt;},mode);
 }
 await expect(outside?page.getByRole('button',{name:'Outside',exact:true}):mode==='failed'?retry:page.getByRole('textbox',{name:'Draft',exact:true})).toBeFocused();
});
for(const surface of ['general','sandbox','update','maintenance'])for(const failed of [false,true])test(`${surface} busy controls recover after settlement failed=${failed}`,async({page})=>{
 await setup(page,surface);const name={general:'Save General',sandbox:'Save sandbox',update:'Reload saved source path',maintenance:'Clean up completed tasks'}[surface];
 const control=page.getByRole('button',{name,exact:true});await expect(control).toBeEnabled();await page.evaluate(()=>window.hold=true);await control.focus();await control.press('Enter');
 if(surface==='maintenance'){await page.getByRole('button',{name:'Confirm',exact:true}).focus();await page.keyboard.press('Enter');}
 await page.waitForFunction(()=>window.pending.length>0);await release(page,failed);await expect(control).toBeEnabled();await expect(control).toBeFocused();
});
test('new-task failed submission restores the Start control',async({page})=>{
 await setup(page,'new-task');await page.getByLabel('Prompt',{exact:true}).fill('Task');const start=page.getByRole('button',{name:'Start task',exact:true});await page.evaluate(()=>window.hold=true);await start.focus();await start.press('Enter');
 await page.waitForFunction(()=>window.pending.length>0);await release(page,true);await expect(start).toBeEnabled();await expect(start).toBeFocused();
});
for(const control of ['fit-session','reconnect-terminal'])test(`primary terminal state recovers ${control}`,async({page})=>{
 await setup(page,'primary');await page.locator('#'+control).focus();await page.evaluate(()=>window.view.state({message:'Recorded',kind:'recorded',interactive:false},{id:'a'}));await expect(page.getByRole('textbox',{name:'Draft',exact:true})).toBeFocused();
});
for(const action of ['Fit secondary session','Reconnect secondary terminal'])test(`secondary terminal state recovers ${action}`,async({page})=>{
 await setup(page,'secondary');await page.getByRole('button',{name:action,exact:true}).focus();await page.evaluate(()=>window.secondaryState({message:'Recorded',kind:'recorded',interactive:false}));await expect(page.getByRole('combobox',{name:'Second task'})).toBeFocused();
});
test('composer state recovers a disabled Send control',async({page})=>{
 await setup(page,'composer');await page.locator('#send').focus();await page.evaluate(()=>window.view.setInteractive(false));await expect(page.getByRole('textbox',{name:'Draft',exact:true})).toBeFocused();
});
test('selection pruning recovers the disabled bulk action',async({page})=>{
 await setup(page,'selection');await page.getByRole('button',{name:'Archive selected tasks',exact:true}).focus();await page.evaluate(()=>window.view.update([]));await expect(page.getByRole('button',{name:'Active filter',exact:true})).toBeFocused();
});

for(const name of ['Save name','Save status'])test(`task-settings direct mutation recovers ${name}`,async({page})=>{
 await setup(page,'task-settings');await page.evaluate(async()=>{window.view.api.json=window.api.json;await window.view.load();window.hold=true;});
 const control=page.getByRole('button',{name,exact:true});if(name==='Save name')await page.getByLabel('Task name',{exact:true}).fill('Renamed');
 await control.focus();await control.press('Enter');await page.waitForFunction(()=>window.pending.length>0);await release(page);await expect(control).toBeEnabled();await expect(control).toBeFocused();
});
for(const failed of [false,true])test(`directory load restores its disabled parent failed=${failed}`,async({page})=>{
 await setup(page,'directory');const control=page.getByRole('button',{name:'Directory',exact:true});await page.evaluate(()=>window.hold=true);await control.focus();await control.press('Enter');await page.waitForFunction(()=>window.pending.length>0);await release(page,failed);await expect(control).toBeEnabled();await expect(control).toBeFocused();
});
test('failed Claude session switch recovers its unlocked action',async({page})=>{
 await setup(page,'sessions');const control=page.getByRole('button',{name:/Other session/});await control.focus();await control.press('Enter');await page.waitForFunction(()=>window.pending.length>0);await release(page,true);await expect(control).toBeEnabled();await expect(control).toBeFocused();
});
test('source path Save recovers its unlocked action',async({page})=>{
 await setup(page,'update');const control=page.getByRole('button',{name:'Save source path',exact:true});await expect(control).toBeEnabled();await page.evaluate(()=>window.hold=true);await control.focus();await control.press('Enter');await page.waitForFunction(()=>window.pending.length>0);await release(page);await expect(control).toBeEnabled();await expect(control).toBeFocused();
});

for(const surface of ['general','sandbox','update','maintenance'])test(`${surface} settled busy state remembers departed and removed focus`,async({page})=>{
 await setup(page,surface);const name={general:'Save General',sandbox:'Save sandbox',update:'Reload saved source path',maintenance:'Clean up completed tasks'}[surface],control=page.getByRole('button',{name,exact:true});await expect(control).toBeEnabled();await page.evaluate(()=>window.hold=true);await control.focus();await control.press('Enter');
 if(surface==='maintenance'){await page.getByRole('button',{name:'Confirm',exact:true}).focus();await page.keyboard.press('Enter');}
 await page.waitForFunction(()=>window.pending.length>0);await page.getByRole('button',{name:'Outside',exact:true}).focus();await page.getByRole('button',{name:'Outside',exact:true}).evaluate(item=>item.remove());await release(page,true);await expect(control).toBeEnabled();await expect(page.locator('body')).toBeFocused();
});
for(const transition of ['cancel','supersede'])test(`task-settings ownership ends on ${transition}`,async({page})=>{
 await setup(page,'task-settings');await page.evaluate(()=>{window.view.api.json=(path,{signal})=>new Promise((resolve,reject)=>signal.addEventListener('abort',()=>reject(signal.reason),{once:true}));});
 const retry=page.getByRole('button',{name:'Retry task settings',exact:true});await retry.focus();await retry.press('Enter');
 if(transition==='cancel'){
  await page.getByRole('button',{name:'Outside',exact:true}).focus();await page.evaluate(()=>window.view.dismiss());await expect(page.getByRole('button',{name:'Outside',exact:true})).toBeFocused();
 }else{
  await page.evaluate(async()=>{window.view.api.json=async()=>({});await window.view.load();});await expect(page.getByRole('button',{name:'Back to task',exact:true})).toBeFocused();
 }
});
for(const stopped of [false,true])test(`connection attempt does not reclaim removed outside focus stopped=${stopped}`,async({page})=>{
 await setup(page,'events');await page.getByRole('button',{name:'Retry connection',exact:true}).focus();await page.evaluate(()=>{window.attempt=window.view.reconnect();});await page.waitForFunction(()=>window.pending.length>0);
 await page.getByRole('button',{name:'Outside',exact:true}).focus();await page.getByRole('button',{name:'Outside',exact:true}).evaluate(item=>item.remove());
 await page.evaluate(async stopped=>{if(stopped)window.view.stop();window.pending.splice(0).forEach(item=>item.resolve());await window.attempt;},stopped);await expect(page.locator('body')).toBeFocused();
});
test('new-task request abort restores the mounted form',async({page})=>{
 await setup(page,'new-task');await page.getByLabel('Prompt',{exact:true}).fill('Task');const start=page.getByRole('button',{name:'Start task',exact:true});await page.evaluate(()=>window.hold=true);await start.focus();await start.press('Enter');await page.waitForFunction(()=>window.pending.length>0);
 await page.evaluate(()=>window.view.abort.abort(new DOMException('Timed out','TimeoutError')));await expect(start).toBeEnabled();await expect(start).toBeFocused();
});
test('background connection success never claims initial focus',async({page})=>{
 await setup(page,'events');await page.evaluate(()=>window.view.healthy());await expect(page.locator('body')).toBeFocused();
});

test('successful update with locked controls restores focus to build output',async({page})=>{
 await setup(page,'update');const control=page.getByRole('button',{name:'Update Argus',exact:true});await expect(control).toBeEnabled();await page.evaluate(()=>{window.hold=true;window.view.api.request=async()=>{await window.api.json('/pending');return new Response(JSON.stringify({restart:true,output:'Done'}),{headers:{'content-type':'application/json'}});};});
 await control.focus();await control.press('Enter');await page.getByRole('button',{name:'Confirm',exact:true}).focus();await page.keyboard.press('Enter');await page.waitForFunction(()=>window.pending.length>0);await release(page);await expect(page.locator('#update-output')).toBeFocused();
});
test('clearing the selected task recovers focus from composer controls',async({page})=>{
 await setup(page,'composer');await page.locator('#send').focus();await page.evaluate(()=>window.view.select(null));await expect(page.getByRole('button',{name:'Agent tab',exact:true})).toBeFocused();
});

for(const failed of [false,true])for(const outside of [false,true])test(`schedule editor settlement keeps ownership failed=${failed} outside=${outside}`,async({page})=>{
 await setup(page,'schedule');await page.getByRole('button',{name:'Edit Daily',exact:true}).click();const save=page.getByRole('button',{name:'Save schedule',exact:true});await page.evaluate(()=>window.hold=true);await save.focus();await save.press('Enter');await page.waitForFunction(()=>window.pending.length>0);
 if(outside){await page.getByRole('button',{name:'Outside',exact:true}).focus();await page.getByRole('button',{name:'Outside',exact:true}).evaluate(item=>item.remove());}
 await release(page,failed);if(failed)await expect(save).toBeEnabled();else await expect(save).toHaveCount(0);
 await expect(outside?page.locator('body'):failed?save:page.getByRole('button',{name:'Edit Daily',exact:true})).toBeFocused();
});
test('bulk Stop retains focus in its progress context',async({page})=>{
 await page.goto('/');await page.evaluate(async()=>{document.body.replaceChildren();const {confirmDialog}=await import('/shared/confirm-dialog.js');confirmDialog({tasks:[{id:'a',name:'Daily'}],taskAction:'archive',request:()=>new Promise(resolve=>window.releaseStop=resolve)});});
 await page.getByRole('button',{name:'Confirm',exact:true}).click();const stop=page.getByRole('button',{name:'Stop',exact:true});await stop.focus();await stop.press('Enter');
 try{await expect(stop).toBeDisabled();await expect(page.locator('.task-action-progress')).toBeFocused();}finally{await page.evaluate(()=>window.releaseStop?.({}));}
});

for(const failed of [false,true])test(`Review download restores its unlocked control failed=${failed}`,async({page})=>{
 await setup(page,'download');const control=page.getByRole('button',{name:'Download',exact:true});await page.evaluate(()=>window.hold=true);await control.focus();await control.press('Enter');await page.waitForFunction(()=>window.pending.length>0);await release(page,failed);await expect(control).toBeEnabled();await expect(control).toBeFocused();
});
for(const target of ['Split view','Fit secondary session','Reconnect secondary terminal'])test(`split task removal recovers ${target}`,async({page})=>{
 await setup(page,'secondary');await page.getByRole('button',{name:target,exact:true}).focus();await page.evaluate(()=>window.view.select(null,[],{}));await expect(page.getByRole('button',{name:'Agent tab',exact:true})).toBeFocused();
});
