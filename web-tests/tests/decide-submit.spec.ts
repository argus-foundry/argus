import {test,expect,Page} from '@playwright/test';
import {readFileSync,mkdirSync} from 'node:fs';
import {resolve} from 'node:path';
const shell=readFileSync(resolve(__dirname,'../../internal/api/static/decide.html'),'utf8');
const rev='2026-11-20T00:00:00Z';
const item=(id:number,kind='decision',role_id=1)=>({id,role_id,kind,updated_at:rev,subject:'Item '+id,body:'A human response is needed.',...(kind==='decision'?{structured:{options:[{id:'yes',label:'Go ahead'}]}}:{})});
async function setup(page:Page,items:any[],failures:Record<number,string>={}){
 const sends:any[]=[];let invisible=false;
 await page.addInitScript(()=>{window.close=()=>{(window as any).closedBySubmit=true;};Object.defineProperty(navigator,'clipboard',{value:{writeText:async(t:string)=>{(window as any).copied=t;}}});});
 await page.route('**/*',async route=>{
  const path=new URL(route.request().url()).pathname;
  if(path==='/decide')return route.fulfill({contentType:'text/html',body:shell});
  if(path.endsWith('decision-session'))return route.fulfill({json:{session:'test',initial_role_id:1}});
  if(path.endsWith('decision-navigation'))return route.fulfill({json:{roles:[1,2].filter(role=>role!==2||!invisible).map(role=>({role_id:role,orchestrator:role===1?'Current board':'Other board',coordinator:'coord',...Object.fromEntries(['decision','fyi','action','todo'].map((kind,n)=>[['decisions','fyis','actions','todos'][n],items.filter(i=>i.role_id===role&&i.kind===kind&&!i.resolved).length]))}))}});
  const role=Number(path.match(/roles\/(\d+)/)?.[1]);
  if(path.endsWith('decide-items'))return route.fulfill({json:{items:items.filter(i=>i.role_id===role&&!i.resolved)}});
  if(route.request().method()==='POST'){
   const body=route.request().postDataJSON(),actions=body.answers||body.actions;sends.push(...actions);
   if(role===2&&failures[22]==='hidden'){invisible=true;return route.fulfill({status:403,json:{error:'Coordinator is hidden or archived'}});}
   const results=actions.map((a:any)=>{
    const i=items.find(i=>i.id===a.item_id),failure=failures[a.item_id];
    if(failure==='already_resolved'){i.resolved=true;return {item_id:i.id,action_id:a.action_id,outcome:'already_resolved'};}
    if(failure==='closed'){i.resolved=true;return {item_id:i.id,action_id:a.action_id,outcome:'invalid',item:{...i}};}
    if(failure==='conflict'){i.updated_at='2026-11-21T00:00:00Z';i.body='The coordinator edited the request.';delete failures[a.item_id];return {item_id:i.id,action_id:a.action_id,outcome:'conflict',item:{...i}};}
    i.resolved=a.action!=='comment';return {item_id:i.id,action_id:a.action_id,outcome:i.resolved?'resolved':'commented',item:{...i}};
   });return route.fulfill({json:{results}});
  }
  return route.fulfill({status:404});
 });
 await page.goto('http://decide.test/decide?role=1#t=launch');await expect(page.locator('#decision-'+items[0].id)).toBeVisible();
 return {sends};
}
async function choose(page:Page,id:number){await page.locator('#decision-'+id+' input[value=yes]').check();}
async function board(page:Page,role:number){if((page.viewportSize()?.width||0)<700){await page.locator('#drawer-trigger').click();await page.locator('#phone-nav [data-role="'+role+'"]').click();}else await page.locator('#desktop-nav [data-role="'+role+'"]').click();}
async function shot(page:Page,name:string,project:string){await expect(page.locator('.submit-note')).toBeVisible();const folder=process.env.DECIDE_SCREENSHOT_DIR||resolve(__dirname,'../test-results/decide-submit-screenshots');mkdirSync(folder,{recursive:true});await page.screenshot({path:folder+'/'+'after'+'-'+name+'-'+project+'.png',fullPage:true});}
for(const left of ['none','same','other'])test('accepted responses stay on board with '+left+' remaining',async({page},info)=>{
 await setup(page,[item(11),...(left==='same'?[item(12)]:left==='other'?[item(12,'fyi')]:[])]);await choose(page,11);await page.locator('#submit').click();
 await shot(page,'accepted-'+left,info.project.name);
 await expect(page.locator('#main h1')).toHaveText('Current board');await expect(page.getByRole('tab',{name:'Decisions, '+(left==='same'?1:0)+' open',exact:true})).toHaveAttribute('aria-selected','true');
 await expect(page.locator('#decision-11')).toHaveCount(0);await expect(page.locator('#main')).toContainText('Answers sent to Argus');
 await expect(page.locator('#confirmed')).toHaveCount(0);await expect(page.locator('#submit')).toBeDisabled();
 if(left==='same'){await expect(page.locator('#decision-12')).toBeVisible();await choose(page,12);await expect(page.locator('.submit-note')).toHaveCount(0);}
 if(left==='other'){await expect(page.getByRole('tab',{name:'FYIs, 1 open',exact:true})).toBeVisible();await page.getByRole('tab',{name:'FYIs, 1 open',exact:true}).click();await expect(page.locator('.submit-note')).toHaveCount(0);}
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
});
test('mixed conflict stays on current tab and reanswer uses new revision',async({page},info)=>{
 const {sends}=await setup(page,[item(11),item(12)],{12:'conflict'});await choose(page,11);await choose(page,12);await page.locator('#note-12').fill('Keep my reasoning');await page.locator('#submit-close').click();
 await shot(page,'mixed',info.project.name);await expect(page.locator('#main h1')).toHaveText('Current board');await expect(page.locator('#main')).toContainText('1 sent · 1 needs another look');
 await expect(page.locator('#decision-12')).toContainText('Item changed');await expect(page.locator('#note-12')).toHaveValue('Keep my reasoning');expect(await page.evaluate(()=>(window as any).closedBySubmit)).toBeUndefined();
 await page.locator('#review').click();await expect(page.locator('#main')).not.toContainText('Item 12');await page.getByRole('button',{name:'← Keep reviewing boards'}).click();
 await choose(page,12);await page.locator('#submit-close').click();await expect(page.locator('h1')).toHaveText('Actions sent — you can close this tab');expect(await page.evaluate(()=>(window as any).closedBySubmit)).toBe(true);expect(sends.map(a=>a.item_id)).toEqual([11,12,12]);expect(sends[2].expected_updated_at).not.toBe(sends[1].expected_updated_at);
});
test('failure on another coordinator marks rail and owning tab',async({page},info)=>{
 await setup(page,[item(11),item(22,'fyi',2)],{22:'conflict'});await choose(page,11);await board(page,2);await page.getByRole('tab',{name:'FYIs, 1 open',exact:true}).click();await page.locator('#note-22').fill('Please keep this');await page.getByRole('button',{name:'Comment',exact:true}).click();await board(page,1);await page.getByRole('tab',{name:'Decisions, 1 open',exact:true}).click();await page.locator('#submit').click();
 await shot(page,'remote-failure',info.project.name);await expect(page.locator('#main h1')).toHaveText('Current board');await expect(page.locator('#desktop-nav [data-role="2"] .queued-dot')).toHaveCount(1);
 await board(page,2);await expect(page.getByRole('tab',{name:/FYIs, 1 open/})).toHaveAttribute('data-attention','true');await page.getByRole('tab',{name:/FYIs, 1 open/}).click();await expect(page.locator('#decision-22')).toContainText('Item changed');
});
for(const unavailable of [false,true])test('closed drafts preserve written feedback and drop empty choices '+unavailable,async({page},info)=>{
 await setup(page,[item(11),item(12,'fyi'),...(unavailable?[item(22,'fyi',2)]:[])],unavailable?{22:'hidden'}:{12:'closed'});await choose(page,11);await page.getByRole('tab',{name:'FYIs, 1 open',exact:true}).click();
 if(unavailable){await board(page,2);}await page.locator('#note-'+(unavailable?22:12)).fill('A note worth keeping');await page.getByRole('button',{name:'Comment',exact:true}).click();await board(page,1);await page.locator('#submit').click();
 await shot(page,'recovery-'+unavailable,info.project.name);await expect(page.locator('#main h1')).toHaveText('Current board');await expect(page.locator('summary')).toContainText('1 unsent note');await expect(page.locator('.unsent-recovery')).not.toHaveAttribute('open','');await expect(page.locator('#submit')).toBeDisabled();
 await page.locator('summary').click();await expect(page.locator('.unsent-recovery')).toContainText('A note worth keeping');await page.getByRole('button',{name:'Copy unsent note',exact:true}).click();expect(await page.evaluate(()=>(window as any).copied)).toContain('A note worth keeping');await page.reload();await expect(page.locator('summary')).toContainText('1 unsent note');await page.locator('summary').click();await page.getByRole('button',{name:'Discard unsent note',exact:true}).click();await expect(page.locator('summary')).toHaveCount(0);
});
test('closed choice-only and hidden Done drafts do not block next submit',async({page})=>{
 await setup(page,[item(11),item(22,'action',2)],{22:'hidden'});await board(page,2);await page.getByRole('tab',{name:'Actions, 1 open',exact:true}).click();await page.getByRole('button',{name:'Done',exact:true}).click();await board(page,1);await page.getByRole('tab',{name:'Decisions, 1 open',exact:true}).click();await choose(page,11);await page.locator('#submit-close').click();await expect(page.locator('#submit')).toBeDisabled();await expect(page.locator('summary')).toHaveCount(0);expect(await page.evaluate(()=>(window as any).closedBySubmit)).toBeUndefined();
});
test('submit and close finishes with unanswered items still open',async({page})=>{
 await setup(page,[item(11),item(12,'fyi')]);await choose(page,11);await expect(page.locator('#tally')).toContainText('1 board');await expect(page.locator('#queued-description')).toContainText('1 coordinator.');await page.locator('#submit-close').click();await expect(page.locator('h1')).toHaveText('Actions sent — you can close this tab');expect(await page.evaluate(()=>(window as any).closedBySubmit)).toBe(true);
});

test('already resolved Done keeps an undelivered written note recoverable',async({page})=>{
 await setup(page,[item(11,'action')],{11:'already_resolved'});await page.locator('#note-11').fill('Do not lose this completion note');await page.getByRole('button',{name:'Done',exact:true}).click();await page.locator('#submit-close').click();await expect(page.locator('.submit-note')).toContainText('0 sent · 1 needs another look');await expect(page.locator('summary')).toContainText('1 unsent note');await page.locator('summary').click();await expect(page.locator('.unsent-recovery')).toContainText('Do not lose this completion note');expect(await page.evaluate(()=>(window as any).closedBySubmit)).toBeUndefined();
});
test('already resolved Done without written text drops the draft',async({page})=>{
 await setup(page,[item(11,'action')],{11:'already_resolved'});await page.getByRole('button',{name:'Done',exact:true}).click();await page.locator('#submit').click();await expect(page.locator('.submit-note')).toContainText('Answers sent to Argus');await expect(page.locator('#submit')).toBeDisabled();await expect(page.locator('summary')).toHaveCount(0);
});
test('explicit review groups queued items once per coordinator with one status per card',async({page})=>{
 await setup(page,[item(11),item(12),item(22,'fyi',2)]);await choose(page,11);await choose(page,12);await board(page,2);await page.getByRole('tab',{name:'FYIs, 1 open',exact:true}).click();await page.getByRole('button',{name:'Comment',exact:true}).click();await page.locator('#review').click();await expect(page.locator('.review-group')).toHaveCount(2);await expect(page.locator('.receipt')).toHaveCount(3);await expect(page.locator('.review-group').first().locator('.receipt')).toHaveCount(2);await expect(page.locator('.receipt .hint')).toHaveCount(1);await expect(page.locator('.review-actions').first()).toHaveCSS('justify-content','flex-start');
});

test('accepted responses stay cleared when board refresh fails',async({page})=>{
 const {sends}=await setup(page,[item(11)]);await choose(page,11);await page.route('**/decision-navigation',route=>route.fulfill({status:500,json:{error:'Offline during refresh'}}));await page.locator('#submit').click();await expect(page.locator('.submit-note')).toContainText('Answers sent to Argus');await expect(page.locator('#status')).toContainText('Board refresh failed: Offline during refresh');await expect(page.locator('#decision-11')).toHaveCount(0);await expect(page.locator('#submit')).toBeDisabled();expect(sends).toHaveLength(1);
});

test('old saved conflicts and closed drafts normalize on the board without requeueing',async({page})=>{
 await page.addInitScript(({open,closed,empty})=>localStorage.setItem('board-responses',JSON.stringify({
  '1:11':{role:1,coordinator:'coord',orchestrator:'Current board',item:open,draft:{choice:'',action:'',note:'My prior draft',annotations:[]},status:'Item changed. Note and annotations kept; review the stale draft and select your response again.'},
  '1:12':{role:1,coordinator:'coord',orchestrator:'Current board',item:closed,closed:true,draft:{choice:'',action:'comment',note:'Closed feedback',annotations:[]},status:'Already resolved — your response was not saved.'},
  '1:13':{role:1,coordinator:'coord',orchestrator:'Current board',item:empty,draft:{choice:'',action:'done',note:'',annotations:[]},status:'Item changed.'}
 })),{open:item(11),closed:item(12,'fyi'),empty:item(13,'action')});
 await setup(page,[item(11)]);await expect(page.locator('#decision-11 .state')).toHaveText('Needs another look');await expect(page.locator('#decision-11 .item-status')).toHaveCount(1);await expect(page.locator('#note-11')).toHaveValue('My prior draft');await expect(page.locator('summary')).toHaveCount(1);await expect(page.locator('#tally')).toContainText('0 queued');await page.locator('#review').click();await expect(page.locator('.receipt')).toHaveCount(0);await page.locator('summary').click();await expect(page.locator('.unsent-recovery')).toContainText('Closed feedback');
});

test('comment and dismiss counts as one sent response in a mixed round',async({page})=>{
 await setup(page,[item(12),item(11,'fyi')],{12:'conflict'});await choose(page,12);await page.getByRole('tab',{name:'FYIs, 1 open',exact:true}).click();await page.locator('#note-11').fill('Feedback with dismissal');await page.getByRole('button',{name:'Comment and dismiss',exact:true}).click();await page.locator('#submit').click();await expect(page.locator('.submit-note')).toContainText('1 sent · 1 needs another look');await expect(page.getByRole('tab',{name:'FYIs, 0 open',exact:true})).toHaveAttribute('aria-selected','true');
});
test('changed Done card preserves its optional note and requeues a new action ID',async({page})=>{
 const {sends}=await setup(page,[item(11,'action')],{11:'conflict'});await page.locator('#note-11').fill('The step was completed');await page.getByRole('button',{name:'Done',exact:true}).click();await page.locator('#submit').click();await expect(page.locator('#decision-11')).toContainText('Item changed');await expect(page.locator('#note-11')).toHaveValue('The step was completed');await page.getByRole('button',{name:'Done',exact:true}).click();await expect(page.locator('.submit-note')).toHaveCount(0);await page.locator('#submit-close').click();await expect(page.locator('h1')).toHaveText('Actions sent — you can close this tab');expect(sends).toHaveLength(2);expect(sends[1].action_id).not.toBe(sends[0].action_id);expect(sends[1].expected_updated_at).not.toBe(sends[0].expected_updated_at);
});
