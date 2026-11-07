import {test,expect} from '@playwright/test';

async function setup(page, surface, options={}) {
  await page.goto('/');
  await page.evaluate(async ({surface,options})=>{
    document.body.replaceChildren();
    const {node,button}=await import('/shared/dom.js');
    const main=node('main');main.id='main';document.body.append(main);
    const back=button('Back',()=>{});back.className='settings-back';
    const host=node('section','settings-section');main.append(back,host);
    const rows=[{id:'a',name:'Daily',label:'Daily',project:'demo',entries:[{path:'/tmp/fixture'}]},{id:'b',name:'Weekly',label:'Weekly',project:'demo',entries:[{path:'/tmp/fixture'}]}];
    let mutated=false,removed=false;
    const api={invalidateReads(){},async request(path,config){return this.json(path,config);},async json(path,config={}){
      if(config.method){mutated=true;removed=config.method==='DELETE'||path.endsWith('/approve')||path.endsWith('/reject');if(options.delayed)await new Promise(resolve=>window.releaseMutation=resolve);return {};}
      const remaining=removed?(options.empty?[]:rows.slice(1)):rows;
      if(path==='/api/schedules')return {schedules:remaining};
      if(path==='/api/projects/full')return {projects:mutated&&surface==='remove-project'?[]:[{name:'demo',path:'/tmp/demo'}]};
      if(path==='/api/backends')return {backends:[{name:'claude',kind:'claude'}]};
      if(path==='/api/tokens')return {tokens:remaining};
      if(path==='/api/push/subscriptions')return {subscriptions:remaining};
      if(path.startsWith('/api/sandbox/proposals'))return {proposals:remaining};
      if(path.startsWith('/api/accounts')&&mutated&&options.unavailable)return {accounts:[]};
      if(path.startsWith('/api/accounts'))return {accounts:[{name:'two',supports:{claude:true}}]};
      if(path.startsWith('/api/tasks'))return {id:'a',name:'Daily',project:'demo',backend:'claude',status:'in_progress'};
      return {};
    }};
    const context={api,host,status(){},busy(){},changed(){}};
    if(surface==='schedule') {const {ScheduleSettings}=await import('/settings/schedules/settings-schedules.js');window.view=new ScheduleSettings(context);}
    if(surface==='token'){const {DeviceTokens}=await import('/settings/notifications/settings-tokens.js');window.view=new DeviceTokens(context);}
    if(surface==='device'){const {BrowserPushSettings}=await import('/settings/notifications/settings-push.js');window.view=new BrowserPushSettings(context);}
    if(surface==='proposal'){const {SandboxProposals}=await import('/settings/sandbox/settings-proposals.js');window.view=new SandboxProposals(context);}
    if(surface==='save-project'||surface==='remove-project'){const {ProjectSettings}=await import('/settings/projects/settings-projects.js');window.view=new ProjectSettings(api,host,()=>{},()=>{},()=>{},{target:'demo'});}
    if(surface==='session'){
      const {TaskSettingsView}=await import('/tasks/settings/task-settings.js');
      window.view=new TaskSettingsView(api,{beforeOpen(){},history:{push(){}},opened(){},changed(){}});
      await window.view.open({id:'a',name:'Daily',project:'demo'},{history:false});
    }
    if(surface==='archive'){
      host.id='task-groups';host.className='';back.className='';const hiddenBack=button('Hidden Settings Back',()=>{});hiddenBack.className='settings-back';hiddenBack.hidden=true;main.append(hiddenBack);back.id=options.archived?'archive-filter':'active-filter';
      const first=button('Archive Daily',()=>window.view.open({id:'a',name:'Daily',archived:!!options.archived}));
      const second=button('Archive Weekly',()=>{});first.className=second.className='task-archive';host.append(first,...(options.empty?[]:[second]));
      const {TaskArchive}=await import('/tasks/task-archive.js');
      const notice=node('p');notice.id='notice';main.append(notice);
      window.view=new TaskArchive(api,async()=>first.remove());
    }
  },{surface,options});
}
async function activate(page,name){const control=page.getByRole('button',{name,exact:true}).first();await control.focus();await page.keyboard.press('Enter');}
async function confirm(page,name='Confirm'){await activate(page,name);await expect(page.locator('dialog')).toHaveCount(0);}

for(const archived of [false,true])for(const empty of [false,true])test(`archive recovery restored=${archived} empty=${empty}`,async({page})=>{
  await setup(page,'archive',{archived,empty});await activate(page,'Archive Daily');await confirm(page,archived?'Restore task':'Archive task');
  await expect(page.getByRole('button',{name:empty?'Back':'Archive Weekly',exact:true})).toBeFocused({timeout:1000});
});
for(const action of ['Stop agent','Resume agent','Move account'])test(`session ${action} retains replacement`,async({page})=>{
  await setup(page,'session');await page.getByRole('button',{name:action,exact:true}).waitFor();await activate(page,action);await confirm(page);await expect(page.getByRole('button',{name:action,exact:true})).toBeFocused({timeout:1000});
});
for(const action of ['Run Daily now','Delete Daily'])for(const empty of [false,true])test(`schedule ${action} empty=${empty}`,async({page})=>{
  await setup(page,'schedule',{empty});const original=await page.getByRole('button',{name:action,exact:true}).elementHandle();
  await activate(page,action);if(action.startsWith('Delete'))await confirm(page);
  await page.waitForFunction(control=>!control.isConnected,original);
  await expect(page.getByRole('button',{name:action.startsWith('Run')?'Run Daily now':empty?'New schedule':'Delete Weekly',exact:true})).toBeFocused({timeout:1000});
});
for(const [surface,action,next] of [['save-project','Save project','Save project'],['remove-project','Remove demo','Back'],['token','Revoke Daily','Revoke Weekly'],['device','Remove device Daily','Remove device Weekly'],['proposal','Approve paths','Approve paths'],['proposal','Reject proposal','Reject proposal']])test(`${surface} ${action} reload recovers focus`,async({page})=>{
  await setup(page,surface);const original=await page.getByRole('button',{name:action,exact:true}).first().elementHandle();
  await activate(page,action);if(surface!=='save-project')await confirm(page,surface==='remove-project'?'Remove project':'Confirm');
  await page.waitForFunction(control=>!control.isConnected,original);
  await expect(page.getByRole('button',{name:next,exact:true}).first()).toBeFocused({timeout:1000});
});
for(const surface of ['schedule','save-project','token','device','proposal'])test(`${surface} delayed mutation respects outside focus`,async({page})=>{
  await setup(page,surface,{delayed:true});
  const action={schedule:'Run Daily now','save-project':'Save project',token:'Revoke Daily',device:'Remove device Daily',proposal:'Reject proposal'}[surface];
  const original=await page.getByRole('button',{name:action,exact:true}).first().elementHandle();
  await activate(page,action);if(['token','device','proposal'].includes(surface))await activate(page,'Confirm');
  await page.waitForFunction(()=>typeof window.releaseMutation==='function');
  await page.getByRole('button',{name:'Back',exact:true}).focus();await page.evaluate(()=>window.releaseMutation());
  await page.waitForFunction(control=>!control.isConnected,original);
  const replacement={schedule:'Run Daily now','save-project':'Save project',token:'Revoke Weekly',device:'Remove device Weekly',proposal:'Reject proposal'}[surface];
  await expect(page.getByRole('button',{name:replacement,exact:true}).first()).toBeVisible();
  await expect(page.getByRole('button',{name:'Back',exact:true})).toBeFocused({timeout:1000});
});

for(const archived of [false,true])test(`archive Cancel resolves replaced opener restored=${archived}`,async({page})=>{
  await setup(page,'archive',{archived});await activate(page,'Archive Daily');
  await page.evaluate(()=>{const old=document.querySelector('#task-groups button');old.replaceWith(old.cloneNode(true));});
  await activate(page,'Cancel');await expect(page.getByRole('button',{name:'Archive Daily',exact:true})).toBeFocused({timeout:1000});
});
test('unavailable account action falls back to Back to task after reload',async({page})=>{
  await setup(page,'session',{unavailable:true});await activate(page,'Move account');await confirm(page);
  await expect(page.getByRole('button',{name:'Back to task',exact:true})).toBeFocused({timeout:1000});
});

for(const archived of [false,true])test(`archive event removes opener before response restored=${archived}`,async({page})=>{
  await setup(page,'archive',{archived,delayed:true});await activate(page,'Archive Daily');
  await activate(page,archived?'Restore task':'Archive task');await page.waitForFunction(()=>typeof window.releaseMutation==='function');
  await page.evaluate(()=>{document.querySelector('#task-groups button').remove();window.releaseMutation();});
  await expect(page.locator('dialog')).toHaveCount(0);await expect(page.getByRole('button',{name:'Archive Weekly',exact:true})).toBeFocused({timeout:1000});
});
for(const [surface,action] of [['token','Revoke Daily'],['device','Remove device Daily'],['proposal','Reject proposal']])test(`${surface} final record uses surviving section control`,async({page})=>{
  await setup(page,surface,{empty:true});await activate(page,action);await confirm(page);
  const target=surface==='token'?page.getByLabel('Token label',{exact:true}):surface==='device'?page.getByRole('button',{name:'Refresh devices',exact:true}):page.getByRole('button',{name:'Back',exact:true});
  await expect(target).toBeFocused({timeout:1000});
});
