import {test,expect} from '@playwright/test';

async function fixture(page,surface) {
  await page.goto('/');
  await page.evaluate(async surface=>{
    document.body.replaceChildren();
    const {node,button}=await import('/shared/dom.js');
    const main=node('main');main.id='main';document.body.append(main);
    const outside=button('Outside',()=>{});outside.className='settings-back';main.append(outside);
    const host=node('section','settings-section');main.append(host);
    const rows=[{id:'a',name:'Daily',label:'Daily',entries:[]}];
    window.failRead=false;window.holdRead=false;
    const api={invalidateReads(){},async request(){if(window.failRead)throw new Error('Unavailable');return new Response('hello');},async json(path){
      if(window.holdRead)await new Promise(resolve=>{(window.pendingReads??=[]).push(resolve);window.releaseRead=()=>window.pendingReads.splice(0).forEach(release=>release());});
      if(window.failRead)throw new Error('Unavailable');
      if(path==='/api/settings')return {sandbox:{available:true},defaults:{}};
      if(path==='/api/projects/full')return {projects:[{name:'demo',path:'/tmp/demo'}]};
      if(path==='/api/backends')return {backends:[]};
      if(path.endsWith('/artifacts'))return {artifacts:[{filename:'report.txt',type:'text',size:5}]};
      if(path==='/api/schedules')return {schedules:rows};
      if(path==='/api/tokens')return {tokens:rows};
      if(path==='/api/push/subscriptions')return {subscriptions:rows};
      if(path.startsWith('/api/sandbox/proposals'))return {proposals:[]};
      if(path.includes('/meta'))return {entries:[{namespace:'pr',key:'url',value:'https://github.com/example/repo/pull/1'}]};
      if(path.includes('/links'))return {links:[{url:'https://github.com/example/repo/pull/1'}]};
      return {};
    }};
    const context={api,host,status(){},busy(){},changed(){}};
    if(surface==='push'){
      Object.defineProperty(navigator,'serviceWorker',{configurable:true,value:{register(){},getRegistration:()=>new Promise(resolve=>window.releaseBrowser=()=>resolve({pushManager:{getSubscription:async()=>window.subscribed?{}:null}}))}});
      window.PushManager=class {};window.Notification={requestPermission(){}};
      const {BrowserPushSettings}=await import('/settings/notifications/settings-push.js');window.view=new BrowserPushSettings(context);
    }
    if(surface==='agents'){const {AgentSettings}=await import('/settings/agents/settings-agents.js');window.view=new AgentSettings(context);}
    if(surface==='tokens'){const {DeviceTokens}=await import('/settings/notifications/settings-tokens.js');window.view=new DeviceTokens(context);}
    if(surface==='general'){const {GeneralSettings}=await import('/settings/settings-general.js');window.view=new GeneralSettings(api,host,()=>{},()=>{},x=>x,()=>{});}
    if(surface==='projects'){const {ProjectSettings}=await import('/settings/projects/settings-projects.js');window.view=new ProjectSettings(api,host,()=>{},()=>{},()=>{},{target:'demo'});}
    if(surface==='sandbox'){const {SandboxSettings}=await import('/settings/sandbox/settings-sandbox.js');window.view=new SandboxSettings(context);}
    if(surface==='schedules'){const {ScheduleSettings}=await import('/settings/schedules/settings-schedules.js');window.view=new ScheduleSettings(context);}
    if(surface==='proposals'){const {SandboxProposals}=await import('/settings/sandbox/settings-proposals.js');window.view=new SandboxProposals(context);}
    if(surface==='navigation'){const {SettingsNavigation}=await import('/settings/settings-navigation.js');window.view=new SettingsNavigation(api,()=>{},()=>{});host.append(window.view.element);await window.view.load();}
    if(surface==='links'){const title=node('h1');title.id='task-title';host.append(title);const {TaskHeaderLinks}=await import('/tasks/task-links.js');window.view=new TaskHeaderLinks(api);await window.view.select({id:'a',branch:'feature',pr_state:'open'});}
    if(surface==='attention'){const {WorkspaceAttention}=await import('/workspace/attention/workspace-attention.js');window.view=new WorkspaceAttention(()=>{});window.view.update([{id:'a',name:'Daily',needs_input:true}]);window.view.notify({kind:'summary',ids:['a']});}
    if(surface==='terminal-revive'){
      const container=node('div');container.style.cssText='height:240px;width:600px';host.append(container);
      const {AgentTerminal}=await import('/terminal/terminal.js');
      window.view=new AgentTerminal({async json(){return {cols:80,rows:24};},async request(){throw new Error('Unavailable snapshot');}},container);
      await window.view.attach({id:'a',live:false,status:'in_progress'});
    }
    if(surface==='tools'){
      const panel=node('div');panel.id='tool-panel';host.append(panel);const tab=button('Artifacts',()=>{});tab.id='tool-artifacts';host.append(tab);
      const {TaskTools}=await import('/tasks/tools.js');window.view=Object.assign(Object.create(TaskTools.prototype),{task:{id:'a'},tool:'artifacts',abort:new AbortController(),review:{},live:{trigger(){}}});window.view.failed(new Error('Unavailable'));
    }
    if(['review','preview','manifest'].includes(surface)){
      const tab=button('Review tab',()=>{});tab.id='tab-review';host.append(tab);
      const panel=node('div');panel.id='review-panel';host.append(panel);
      const {TaskReview}=await import('/review/review.js');window.view=new TaskReview(api,()=>{});window.view.task={id:'a'};window.failRead=true;if(surface==='preview')await window.view.openArtifact(window.view.task,{filename:'report.txt',type:'text',size:5});else if(surface==='manifest')await window.view.restoreArtifact(window.view.task,'report.txt');else await window.view.openDiff(window.view.task,'app.js');window.failRead=false;
    }
    if(surface==='rail'){
      const nav=node('nav');nav.id='navigation';host.append(nav);
      for(const id of ['projects-warning','search-clear','search','active-filter','archive-filter','active-count','archive-count','task-count','task-groups']){const element=node(id.endsWith('filter')?'button':'div','',id);element.id=id;nav.append(element);}
      const {TaskNavigation}=await import('/workspace/rail/tasks.js');window.view=new TaskNavigation(()=>{});window.view.update([{id:'a',name:'Daily',project:'demo'},{id:'b',name:'Weekly',project:'demo'}]);
    }
    if(surface==='new-task'){
      const {NewTaskView}=await import('/tasks/create/new-task.js');window.view=new NewTaskView(api,()=>{},()=>{});window.view.open('demo',()=>{});await window.view.loadBackends('');
      window.view.backends=[{name:'claude',models:['one','two']}];window.view.backend.append(new Option('claude','claude'));window.view.backend.value='claude';window.view.renderModels();
    }
    if(surface==='task-settings'){
      const {TaskSettingsView}=await import('/tasks/settings/task-settings.js');window.view=new TaskSettingsView(api,{beforeOpen(){},history:{push(){}},opened(){}});await window.view.open({id:'a',name:'Daily'},{history:false});
    }
    if(surface==='split'){
      for(const id of ['agent-panel','terminal','fit-session']){const element=node('div');element.id=id;host.append(element);}
      const {SplitWorkspace}=await import('/workspace/split/split-workspace.js');window.view=new SplitWorkspace(()=>({releaseSecondary(){},attachSecondary(){}}));window.view.enabled=true;
      window.tasks=[{id:'c',name:'Coordinator'},{id:'w',name:'Worker'},{id:'x',name:'Other'}];
      window.roster={orchestrators:[{id:1,name:'Team',roles:[{role_id:1,kind:'coordinator',live:true,task_id:'c'},{role_id:2,kind:'worker',live:true,task_id:'w'},{role_id:3,kind:'worker',live:true,task_id:'x'}]}]};
      window.view.select(window.tasks[0],window.tasks,window.roster);window.view.rosterDetails.open=true;window.view.renderRoster();
    }
  },surface);
}

for(const action of ['Enable desktop alerts','Send test notification'])for(const owned of [true,false])test(`subscription lookup keeps ${action} ownership=${owned}`,async({page})=>{
  await fixture(page,'push');
  await page.getByRole('button',{name:owned?action:'Outside',exact:true}).focus();
  await page.evaluate(()=>{window.subscribed=true;window.releaseBrowser();});
  await expect(page.getByText('This browser has a push subscription.')).toBeVisible();
  await expect(page.getByRole('button',{name:owned?(action==='Enable desktop alerts'?'Disable desktop alerts':action):'Outside',exact:true})).toBeFocused();
});
for(const change of ['deleted','unbound','last','roster'])test(`split recovers focused worker after ${change}`,async({page})=>{
  await fixture(page,'split');await page.locator('[data-role-id="2"]').focus();
  await page.evaluate(change=>{
    if(change==='roster')window.roster={};
    else {window.tasks=window.tasks.filter(task=>task.id!=='w'&&(change!=='last'||task.id!=='x'));if(change==='unbound')window.roster.orchestrators[0].roles[1].live=false;}
    window.view.update(window.tasks,window.roster);window.view.renderRoster();
  },change);
  await expect(change==='roster'?page.getByRole('combobox',{name:'Second task'}):change==='last'?page.locator('#split-roster summary'):page.locator('[data-role-id="3"]')).toBeFocused();
});
test('split roster updates leave outside focus alone',async({page})=>{
  await fixture(page,'split');await page.getByRole('button',{name:'Outside',exact:true}).focus();
  await page.evaluate(()=>{window.view.update(window.tasks,{});window.view.renderRoster();});
  await expect(page.getByRole('button',{name:'Outside',exact:true})).toBeFocused();
});
for(const surface of ['general','projects','sandbox','schedules','proposals'])test(`${surface} Retry read recovers focus`,async({page})=>{
  await fixture(page,surface);
  await page.evaluate(async()=>{window.failRead=true;await window.view.load();window.failRead=false;});
  const retry=page.getByRole('button',{name:/Retry/}).last();await retry.focus();await retry.press('Enter');
  await expect(retry).toHaveCount(0);
  const target={general:page.getByLabel('Default agent',{exact:true}),projects:page.getByLabel('Repository path',{exact:true}),sandbox:page.getByLabel('Sandbox enabled',{exact:true}),schedules:page.getByRole('button',{name:'Edit Daily',exact:true}),proposals:page.getByRole('button',{name:'Outside',exact:true})}[surface];
  await expect(target).toBeFocused();
});
for(const surface of ['push','tokens'])for(const failed of [false,true])test(`${surface} delayed list refresh failure=${failed}`,async({page})=>{
  await fixture(page,surface);
  const action=surface==='push'?'Remove device Daily':'Revoke Daily';await page.getByRole('button',{name:action,exact:true}).waitFor();
  await page.evaluate(()=>{window.holdRead=true;window.readPromise=window.view.loadDevices?window.view.loadDevices():window.view.load();});
  await page.getByRole('button',{name:action,exact:true}).focus();
  await page.evaluate(async failed=>{window.holdRead=false;window.failRead=failed;window.releaseRead();await window.readPromise;},failed);
  await expect(failed&&surface==='tokens'?page.getByLabel('Token label',{exact:true}):page.getByRole('button',{name:failed?'Refresh devices':action,exact:true})).toBeFocused();
});
for(const removed of [false,true])test(`header PR refresh removed=${removed}`,async({page})=>{
  await fixture(page,'links');await page.getByRole('link',{name:'Open pull request'}).focus();
  await page.evaluate(async removed=>{if(removed)window.view.api.json=async()=>({});await window.view.select({id:'a',branch:'feature',pr_state:'approved'});},removed);
  await expect(removed?page.getByRole('button',{name:'Copy branch feature'}):page.getByRole('link',{name:'Open pull request'})).toBeFocused();
});
test('navigation repository disappearance falls back to surviving navigation',async({page})=>{
  await fixture(page,'navigation');await page.getByRole('button',{name:'demo',exact:true}).focus();
  await page.evaluate(async()=>{window.view.api.json=async()=>({projects:[]});await window.view.load();});
  await expect(page.locator('.settings-nav-entry:focus')).toHaveCount(1);
});
test('attention summary refresh and expiry retain a useful focus target',async({page})=>{
  await fixture(page,'attention');await page.getByRole('button',{name:'Dismiss',exact:true}).focus();
  await page.evaluate(()=>window.view.notify({kind:'summary',ids:['a']}));
  await expect(page.getByRole('button',{name:'Dismiss',exact:true})).toBeFocused();
  await page.evaluate(()=>window.view.update([]));
  await expect(page.getByRole('button',{name:'Outside',exact:true})).toBeFocused();
});

test('new-task choices refresh preserves replaced model focus',async({page})=>{
  await fixture(page,'new-task');await page.getByLabel('Model',{exact:true}).focus();
  await page.evaluate(async()=>{window.view.api.json=async path=>path==='/api/backends'?{backends:[{name:'claude',models:['one','two']}]}:{};await window.view.loadBackends('claude');});
  await expect(page.getByLabel('Model',{exact:true})).toBeFocused();
});
test('task-settings Retry read recovers focus',async({page})=>{
  await fixture(page,'task-settings');await page.evaluate(async()=>{window.failRead=true;await window.view.load();window.failRead=false;});
  const retry=page.getByRole('button',{name:'Retry task settings',exact:true});await retry.focus();await retry.press('Enter');
  await expect(retry).toHaveCount(0);await expect(page.getByRole('button',{name:'Back to task',exact:true})).toBeFocused();
});

for(const failed of [false,true])test(`Tools Retry settlement failure=${failed}`,async({page})=>{
  await fixture(page,'tools');await page.getByRole('button',{name:'Retry',exact:true}).focus();
  await page.evaluate(failed=>{if(failed)window.view.failed(new Error('Again'));else window.view.apply({artifacts:[{filename:'report.txt',name:'Report',type:'text',size:2}]});},failed);
  await expect(page.getByRole('button',{name:failed?'Retry':/Report/})).toBeFocused();
});
test('Review Retry settles on a diff control',async({page})=>{
  await fixture(page,'review');const retry=page.getByRole('button',{name:'Retry',exact:true});await retry.focus();await retry.press('Enter');
  await expect(page.getByRole('button',{name:'Unified',exact:true})).toBeFocused();
});
for(const empty of [false,true])test(`rail row disappearance empty=${empty}`,async({page})=>{
  await fixture(page,'rail');await page.locator('.task-row[data-task-id="a"]').focus();
  await page.evaluate(empty=>window.view.update(empty?[]:[{id:'b',name:'Weekly',project:'demo'}]),empty);
  await expect(empty?page.locator('#active-filter'):page.locator('.task-row[data-task-id="b"]')).toBeFocused();
});

test('Agents refresh keeps the replacement action focused',async({page})=>{
  await fixture(page,'agents');const refresh=page.getByRole('button',{name:'Refresh agents',exact:true});await refresh.focus();await refresh.press('Enter');await expect(refresh).toBeFocused();
});
for(const surface of ['push','tokens','general','projects','sandbox','schedules','proposals','agents','navigation','attention','split','tools','review'])test(`${surface} initial read leaves body focus alone`,async({page})=>{
  await fixture(page,surface);
  if(surface==='push')await page.evaluate(()=>window.releaseBrowser());
  await expect(page.locator('body')).toBeFocused();
});
for(const surface of ['general','projects','sandbox','schedules','proposals'])test(`${surface} delayed Retry respects outside focus`,async({page})=>{
  await fixture(page,surface);await page.evaluate(async()=>{window.failRead=true;await window.view.load();window.failRead=false;window.holdRead=true;const load=window.view.load.bind(window.view);window.view.load=()=>window.reload=load();});
  const retry=page.getByRole('button',{name:/Retry/}).last();await retry.focus();await retry.press('Enter');await page.waitForFunction(()=>typeof window.releaseRead==='function');
  await page.getByRole('button',{name:'Outside',exact:true}).focus();
  await page.evaluate(async()=>{window.holdRead=false;window.releaseRead();await window.reload;});
  await expect(page.getByRole('button',{name:'Outside',exact:true})).toBeFocused();
});

for(const surface of ['preview','manifest'])test(`${surface} Retry restores a surviving Review action`,async({page})=>{
  await fixture(page,surface);const retry=page.getByRole('button',{name:surface==='preview'?'Retry preview':'Retry',exact:true});await retry.focus();await retry.press('Enter');
  await expect(page.getByRole('button',{name:surface==='preview'?'Download':'Review tab',exact:true})).toBeFocused();
});

test('same-task branch update retains focused PR across header rebuild',async({page})=>{
  await fixture(page,'links');await page.getByRole('link',{name:'Open pull request'}).focus();
  await page.evaluate(()=>window.view.select({id:'a',branch:'renamed',pr_state:'approved'}));
  await expect(page.getByRole('link',{name:'Open pull request'})).toBeFocused();
});

for(const outside of [false,true])test(`superseded branch read respects focus ownership outside=${outside}`,async({page})=>{
  await fixture(page,'links');await page.getByRole('link',{name:'Open pull request'}).focus();
  await page.evaluate(()=>{window.holdRead=true;window.firstBranch=window.view.select({id:'a',branch:'renamed',pr_state:'approved'});window.nextBranch=window.view.select({id:'a',branch:'renamed',pr_state:'merged-closed'});});
  if(outside)await page.getByRole('button',{name:'Outside',exact:true}).focus();
  await page.evaluate(async()=>{window.holdRead=false;window.releaseRead();await Promise.all([window.firstBranch,window.nextBranch]);});
  await expect(outside?page.getByRole('button',{name:'Outside',exact:true}):page.getByRole('link',{name:'Open pull request'})).toBeFocused();
});

for(const outside of [false,true])test(`terminal revival replaces failed viewer without dropping focus outside=${outside}`,async({page})=>{
  await fixture(page,'terminal-revive');await page.locator('.xterm-helper-textarea').focus();
  if(outside)await page.getByRole('button',{name:'Outside',exact:true}).focus();
  await page.evaluate(()=>window.view.attach({id:'a',live:true,status:'in_progress'}));
  await expect(outside?page.getByRole('button',{name:'Outside',exact:true}):page.locator('.xterm-helper-textarea')).toBeFocused();
});
