import {test,expect} from '@playwright/test';
import {actorPage} from '../../_lifecycle-fixture.ts';
const cleanup=[];test.afterEach(async()=>{for(const dispose of cleanup.splice(0))await dispose();});
test('roster context thresholds, timing, dead rows and recycle budget remain accessible without per-row reads',async({browser})=>{
 const page=await actorPage(browser,cleanup,'<div id="rows"></div>');let reads=0;page.on('request',request=>{if(request.url().includes('/api/'))reads++;});
 await page.evaluate(async()=>{const {compactTaskRow}=await import('/workspace/rail/task-row.js');const host=document.querySelector('#rows');for(const pct of [39,40,64,65,89,90,null]){const task={id:String(pct),name:'Worker '+pct,live:true};const role={live:true,kind:'worker',context:{tokens:40000,percent:pct,denominator_tokens:pct===null?null:100000,denominator_kind:'worker_window',observed_at:null}};host.append(compactTaskRow({task,role,select:()=>{}}));}for(const [id,task,role] of [['dead',{live:false},{}],['archived',{archived:true},{}],['archived-role',{live:true},{archived:true,context:{percent:90,tokens:90000,denominator_tokens:100000}}],['coordinator',{live:true},{kind:'coordinator',context:{tokens:100,percent:50,denominator_tokens:200,denominator_kind:'coordinator_recycle_budget',observed_at:null}}],['missing',{live:true},{context:null}],['stale',{live:true},{roster_stale:true}]])host.append(compactTaskRow({task:{id,name:id,...task},role:{live:true,kind:'worker',...role},select:()=>{}}));});
 for(const [id,tier] of [['39','low'],['40','warm'],['64','warm'],['65','hot'],['89','hot'],['90','critical'],['null','unknown'],['stale','stale']])await expect(page.locator(`[data-task-id="${id}"] .task-context-pressure`)).toHaveAttribute('data-pressure',tier);
 for(const pct of [39,40,64,65,89,90]){
  const pressure=page.locator(`[data-task-id="${pct}"] .task-context-pressure`);
  await expect(pressure).toHaveText(`${pct}%`);await expect(pressure).toHaveAccessibleName(new RegExp(`, ${pct}% (low|warm|hot|critical),`));
 }
 await expect(page.locator('[data-task-id="40"] .task-context-pressure')).toHaveAccessibleName(/40,000 tokens.*100,000 tokens.*timing unknown/);
 for(const id of ['dead','archived','archived-role'])await expect(page.locator(`[data-task-id="${id}"] .task-context-pressure`)).toHaveCount(0);
 await expect(page.locator('[data-task-id="coordinator"] .task-context-pressure')).toHaveAccessibleName(/recycle-budget usage/);await expect(page.locator('[data-task-id="missing"] .task-context-pressure')).toHaveAccessibleName('Context unavailable');expect(reads).toBe(0);
 await page.emulateMedia({reducedMotion:'reduce'});expect(await page.locator('.task-context-pressure').evaluateAll(nodes=>nodes.every(node=>node.getAnimations().length===0))).toBe(true);
});
test('team Info distinguishes requested, resolved, default, warning and planned choices',async({browser})=>{
 const page=await actorPage(browser,cleanup,'<dl id="rows"></dl>');
 await page.evaluate(async()=>{const {teamModelRows}=await import('/tasks/team-models.js');window.roster={orchestrators:[{id:1,roles:[{role_id:1,live:true,kind:'coordinator',task_id:'a',name:'Coordinator',archetype:'orchestrator',model_resolution:{requested_model:'requested',effective_model:'effective',source:'profile',basis:'current_config',profile:'lean',effort:'high'}},{name:'Builder',model_resolution:{effective_model:null,source:'cli_default',basis:'planned_intent'}},{name:'Unavailable worker',model_resolution:{source:'unavailable',warning:'Safe warning'}}]}]};window.draw=()=>document.querySelector('#rows').replaceChildren(...teamModelRows({id:'a'},window.roster).map(([label,value])=>{const row=document.createElement('div');const term=document.createElement('dt'),definition=document.createElement('dd');term.textContent=label;definition.textContent=value;row.append(term,definition);return row;}));window.draw();});
 await expect(page.locator('#rows div').filter({has:page.getByText('Builder · Basis',{exact:true})}).locator('dd')).toHaveText('Planned');
 await expect(page.locator('#rows div').filter({has:page.getByText('Coordinator · Basis',{exact:true})}).locator('dd')).toHaveText('Resolved from current settings');
 for(const value of ['requested','effective','CLI default','Safe warning','Planned','Resolved from current settings','orchestrator','lean','high'])await expect(page.locator('#rows')).toContainText(value);
 await page.evaluate(()=>{window.roster.stale=true;window.draw();});await expect(page.locator('#rows')).toContainText('Stale. Roster refresh failed.');
});
test('Info refresh uses shared roster and marks retained team details stale after authentication failure',async({browser})=>{
 const page=await browser.newPage({viewport:{width:1440,height:900}});cleanup.push(()=>page.close());let fail=false,model='initial-effective',reads=0;
 const task={id:'a',name:'Coordinator task',project:'demo',backend:'test',status:'complete',live:false};
 await page.route('**/api/**',route=>route.fulfill({status:404,json:{}}));
 await page.route('**/api/tasks?archived=all',route=>route.fulfill({json:{tasks:[task]}}));await page.route('**/api/projects/full',route=>route.fulfill({json:{projects:[{name:'demo'}]}}));
 await page.route('**/api/tasks/a/raw',route=>route.fulfill({json:{id:'a',model:'raw-requested'}}));await page.route('**/api/hera',route=>{reads++;return route.fulfill({status:fail?401:200,json:fail?{}:{orchestrators:[{id:1,roles:[{role_id:1,name:'Coordinator',kind:'coordinator',task_id:'a',live:true,model_resolution:{effective_model:model,basis:'current_config',source:'profile'}}]}]}});});
 await page.goto(process.env.WORKSPACE_TEST_URL);await page.locator('#token').fill('fixture-token');await page.locator('#connect-button').click();await page.locator('.task-row').first().waitFor();await page.locator('#tool-status').click();
 await expect(page.locator('#tool-panel')).toContainText('initial-effective');model='refreshed-effective';const before=reads;await page.getByRole('button',{name:'Refresh identity'}).click();await expect(page.locator('#tool-panel')).toContainText('refreshed-effective');expect(reads).toBeGreaterThan(before);
 fail=true;await page.getByRole('button',{name:'Refresh identity'}).click();await expect(page.locator('#tool-panel')).toContainText('Stale. Roster refresh failed.');await expect(page.locator('#tool-panel')).toContainText('refreshed-effective');await expect(page.locator('#hierarchy-warning')).toContainText('authentication');
});
for(const status of [401,403])test(`ordinary task refresh marks retained roster stale after ${status} and recovers`,async({browser})=>{
 const page=await browser.newPage({viewport:{width:1440,height:900}});cleanup.push(()=>page.close());let failure=0,model='retained-model';
 const task={id:'a',name:'Coordinator task',project:'demo',status:'in_progress',live:true,idle:false};
 await page.addInitScript(()=>{
  window.EventSource=class{constructor(){queueMicrotask(()=>this.onopen?.());}addEventListener(){}close(){}};
  const original=window.fetch.bind(window);window.fetch=(url,options)=>String(url).startsWith('/api/events/stream')?Promise.resolve(new Response(new ReadableStream({start(controller){window.eventsController=controller;}}),{headers:{'Content-Type':'text/event-stream'}})):original(url,options);
 });
 await page.route('**/api/**',route=>{
  const path=new URL(route.request().url()).pathname;
  if(path==='/api/tasks')return route.fulfill({json:{tasks:[task]}});
  if(path==='/api/tasks/a'||path.endsWith('/raw'))return route.fulfill({json:task});
  if(path.endsWith('/size'))return route.fulfill({json:{cols:80,rows:24}});
  if(path.endsWith('/output'))return route.fulfill({body:'',headers:{'X-Output-Total':'0'}});
  if(path==='/api/projects/full')return route.fulfill({json:{projects:[{name:'demo'}]}});
  if(path==='/api/hera')return route.fulfill({status:failure||200,json:failure?{}:{orchestrators:[{id:1,roles:[{role_id:1,name:'Coordinator',kind:'coordinator',task_id:'a',live:true,model_resolution:{effective_model:model,basis:'current_config',source:'profile'},context:{percent:90,tokens:90000,denominator_tokens:100000,denominator_kind:'coordinator_recycle_budget'}}]}]}});
  return route.fulfill({json:{}});
 });
 await page.goto(process.env.WORKSPACE_TEST_URL);await page.locator('#token').fill('fixture-token');await page.locator('#connect-button').click();await page.locator('#tool-status').click();
 await expect(page.locator('#tool-panel')).toContainText('retained-model');await page.clock.install();
 await page.evaluate(()=>window.eventsController.error(new Error('fixture stream outage')));failure=status;
 await page.getByRole('button',{name:'Retry connection',exact:true}).click();
 await expect(page.locator('#connection-state')).toHaveAttribute('data-state','error');
 await expect(page.locator('#tool-panel')).toContainText('Stale. Roster refresh failed.');
 await expect(page.locator('[data-task-id="a"] .task-context-pressure')).toHaveAttribute('data-pressure','stale');
 failure=0;model='recovered-model';await page.getByRole('button',{name:'Retry connection',exact:true}).click();
 await expect(page.locator('#tool-panel')).toContainText('recovered-model');await expect(page.locator('#tool-panel')).not.toContainText('Stale. Roster refresh failed.');
 await expect(page.locator('[data-task-id="a"] .task-context-pressure')).toHaveAttribute('data-pressure','critical');
});
