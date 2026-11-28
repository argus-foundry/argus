import {test,expect,Page} from '@playwright/test';
import {readFileSync} from 'node:fs';
import {resolve} from 'node:path';

const shell=readFileSync(resolve(__dirname,'../../internal/api/static/decide.html'),'utf8');
const origin='http://decide.test';
const decision={id:11,role_id:1,kind:'decision',updated_at:'r1',subject:'Choose a path',body:'Context',structured:{options:[{id:'a',label:'Path A'},{id:'b',label:'Path B'}],recommended:'a'}};
const fyi={id:22,role_id:2,kind:'fyi',updated_at:'r2',subject:'Preview ready',body:'**Ready** to review. <img src=x>',structured:null};
const rows=[{role_id:1,parent_role_id:null,orchestrator:'Parent',coordinator:'coord',decisions:1,fyis:0},{role_id:2,parent_role_id:1,orchestrator:'Child',coordinator:'coord',decisions:0,fyis:1},{role_id:3,parent_role_id:null,orchestrator:'Empty',coordinator:'coord',decisions:0,fyis:0}];

async function rail(page:Page,{failFYI=false,hidden=false,omitResult=false}={}){
 const sends:any[]=[];let calls=0;
 await page.addInitScript(()=>{window.close=()=>{(globalThis as any).attemptedClose=true;};});
 await page.route('**/*',async route=>{
  const path=new URL(route.request().url()).pathname;
  if(path==='/decide')return route.fulfill({contentType:'text/html',body:shell});
  if(path.endsWith('/decision-session'))return route.fulfill({json:{session:'rail-session',initial_role_id:1}});
  expect(route.request().headers().authorization).toBe('Bearer rail-session');
  if(path.endsWith('/decision-navigation'))return route.fulfill({json:{roles:rows}});
  if(path.endsWith('/decide-items'))return route.fulfill({json:{items:path.includes('/1/')?[decision]:path.includes('/2/')?[fyi]:[],coordinator:'coord',orchestrator:'team'}});
  const body=route.request().postDataJSON();sends.push({path,body});
  if(path.endsWith('/answers'))return route.fulfill({json:{results:body.answers.map((a:any)=>({item_id:a.item_id,outcome:'resolved'}))}});
  if(path.endsWith('/fyi-actions')){
   if(hidden)return route.fulfill({status:403,json:{code:'board_not_visible',error:'Coordinator is hidden or archived'}});
   if(failFYI&&calls++===0)return route.abort('failed');
   return route.fulfill({json:{results:omitResult?[]:body.actions.map((a:any)=>({action_id:a.action_id,item_id:a.item_id,outcome:a.action==='comment'?'commented':'resolved'}))}});
  }
  throw Error('Unexpected '+path);
 });
 return sends;
}

test('rail traversal includes nested and empty rows; typing retains native arrow behavior',async({page})=>{
 await rail(page);await page.goto(origin+'/decide#t=launch');
 await expect(page.locator('#decision-11')).toBeVisible();
 await page.keyboard.press('Control+ArrowDown');
 await expect(page.locator('#main h1')).toHaveText('Child');
 // A chosen tab survives coordinator movement, including an empty board.
 await expect(page.getByRole('tab',{name:'Decisions, 0 open'})).toHaveAttribute('aria-selected','true');
 await page.keyboard.press('Control+ArrowRight');await expect(page.locator('#decision-22')).toBeVisible();
 await page.locator('#note-22').focus();await page.keyboard.press('Control+ArrowDown');
 await expect(page.locator('#main h1')).toHaveText('Child');
 await page.locator('#main').focus();await page.keyboard.press('Control+ArrowDown');
 await expect(page.locator('#main')).toContainText('Nothing open for Empty');
 await page.keyboard.press('Control+ArrowDown');await expect(page.locator('#main h1')).toHaveText('Empty');
 await expect(page.locator('#desktop-nav .empty')).toHaveCount(1);
});

test('phone drawer traps focus, restores trigger and preserves queued decisions across tabs',async({page})=>{
 await page.setViewportSize({width:390,height:844});await rail(page);await page.goto(origin+'/decide#t=launch');
 await page.locator('#decision-11 input[value=a]').check();
 await page.locator('#drawer-trigger').click();await expect(page.locator('#drawer')).toBeVisible();
 await page.locator('#drawer-close').focus();await page.keyboard.press('Shift+Tab');
 await expect(page.locator('#phone-nav button').last()).toBeFocused();
 await page.keyboard.press('Escape');await expect(page.locator('#drawer-trigger')).toBeFocused();
 await page.locator('#drawer-trigger').click();await page.locator('#phone-nav button[data-role="2"]').click();
 await expect(page.locator('#drawer')).not.toBeVisible();
 await page.getByRole('tab',{name:'FYIs, 1 open'}).click();await page.locator('#note-22').fill('Thanks');
 await expect(page.locator('#tally')).toContainText('1 queued');
 await page.getByRole('button',{name:'Comment',exact:true}).click();
 await page.getByRole('button',{name:'Review queued',exact:true}).click();
 await expect(page.locator('#main')).toContainText('Parent / coord');await expect(page.locator('#main')).toContainText('Child / coord');
 await expect(page.locator('#main')).toContainText('Thanks');
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
 await page.getByRole('button',{name:'Submit & close',exact:true}).click();
 await expect(page.locator('h1')).toHaveText('Actions sent — you can close this tab');
 await expect(page.locator('main')).toContainText('1 answers · 1 comments · 0 dismissals');
 await expect(page.getByRole('link',{name:'Return to Argus'})).toHaveAttribute('href','/');
});

test('network uncertainty survives reload with stable ID and immutable payload; accepted answers are not resent',async({page})=>{
 const sends=await rail(page,{failFYI:true});await page.goto(origin+'/decide#t=launch');
 await page.locator('#decision-11 input[value=a]').check();await page.locator('#main').focus();await page.keyboard.press('Control+ArrowDown');await page.keyboard.press('Control+ArrowRight');
 await page.locator('#note-22').fill('Do this once');await page.getByRole('button',{name:'Comment',exact:true}).click();
 await page.getByRole('button',{name:'Submit & close',exact:true}).click();await expect(page.locator('.submit-note')).toContainText('1 sent · 1 needs another look');
 await expect(page.locator('#confirmed')).toHaveCount(0);
 const first=sends.find(s=>s.path.endsWith('/fyi-actions')).body.actions[0];
 await page.reload();await expect(page.locator('#main h1')).toHaveText('Parent');
 await page.keyboard.press('Control+ArrowDown');await page.keyboard.press('Control+ArrowRight');
 await expect(page.locator('#note-22')).toBeDisabled();
 await page.getByRole('button',{name:'Submit & close',exact:true}).click();
 await expect(page.locator('h1')).toHaveText('Actions sent — you can close this tab');
 await expect(page.locator('main')).toContainText('1 answers · 1 comments · 0 dismissals');
 expect(sends.filter(s=>s.path.endsWith('/answers'))).toHaveLength(1);
 expect(sends.filter(s=>s.path.endsWith('/fyi-actions')).map(s=>s.body.actions[0])).toEqual([first,first]);
 expect(await page.evaluate(()=>JSON.stringify(localStorage))).not.toContain('rail-session');
});

for(const mode of ['hidden','missing'])test(`${mode} board result prevents full success while other boards commit`,async({page})=>{
 const sends=await rail(page,{hidden:mode==='hidden',omitResult:mode==='missing'});await page.goto(origin+'/decide#t=launch');
 await page.locator('#decision-11 input[value=a]').check();await page.locator('#main').focus();await page.keyboard.press('Control+ArrowDown');await page.keyboard.press('Control+ArrowRight');
 await page.getByRole('button',{name:'Dismiss',exact:true}).click();await page.getByRole('button',{name:'Submit & close',exact:true}).click();
 await expect(page.locator('#main h1')).toHaveText('Child');
 await expect(page.locator('.submit-note')).toContainText('1 sent · 1 needs another look');
 await expect(page.locator('#tally')).toContainText(mode==='hidden'?'0 queued':'1 queued');
 expect(sends).toHaveLength(2);expect(await page.evaluate(()=>(globalThis as any).attemptedClose)).toBeUndefined();
});

test('installed-phone-style app opens Decide in place with the device token only in API headers',async({page})=>{
 await page.setViewportSize({width:390,height:844});
 const app=readFileSync(resolve(__dirname,'../../internal/api/static/index.html'),'utf8');
 const urls:string[]=[];
 await page.addInitScript(()=>localStorage.setItem('argus-token','device-secret'));
 await page.route('**/*',async route=>{
  const url=new URL(route.request().url());urls.push(url.href);
  if(url.pathname==='/')return route.fulfill({contentType:'text/html',body:app});
  if(url.pathname==='/decide')return route.fulfill({contentType:'text/html',body:shell});
  if(url.pathname.startsWith('/vendor/'))return route.fulfill({contentType:'text/javascript',body:''});
  if(!url.pathname.startsWith('/api/'))return route.fulfill({status:404});
  if(url.pathname.endsWith('/decision-session')){expect(route.request().postDataJSON()).toEqual({ticket:'phone-ticket'});return route.fulfill({json:{session:'phone-session',initial_role_id:null}});}
  if(url.pathname.endsWith('/decision-navigation')){expect(route.request().headers().authorization).toBe('Bearer phone-session');return route.fulfill({json:{roles:[]}});}
  expect(route.request().headers().authorization).toBe('Bearer device-secret');
  if(url.pathname.endsWith('/decision-counts'))return route.fulfill({json:{total:2}});
  if(url.pathname.endsWith('/decision-link'))return route.fulfill({json:{path:'/decide',ticket:'phone-ticket'}});
  return route.fulfill({json:url.pathname==='/api/status'?{status:'ok'}:[]});
 });
 await page.goto(origin);
 await expect(page.locator('#decide-count')).toHaveText('2');
 expect(await page.locator('#main-app > .tabs').evaluate(e=>e.scrollWidth<=e.clientWidth)).toBe(true);
 const pages=page.context().pages().length;
 await page.locator('#decide-entry-button').click();
 await expect(page.locator('#main h1')).toHaveText('No visible coordinators');
 expect(page.context().pages()).toHaveLength(pages);
 expect(urls.join(' ')).not.toContain('device-secret');
 expect(await page.evaluate(()=>JSON.stringify(sessionStorage))).not.toContain('device-secret');
 await expect(page).toHaveURL(origin+'/decide');
});

test('text controls and vertical gestures keep native behavior; background horizontal swipe changes tabs',async({page})=>{
 await page.setViewportSize({width:390,height:844});await rail(page);await page.goto(origin+'/decide#t=launch');
 await expect(page.locator('#decision-11')).toBeVisible();
 for(const markup of ['<input>','<textarea></textarea>','<select><option>One</option></select>','<div contenteditable="true"><span>Editable child</span></div>','<div role="textbox"><span>Textbox child</span></div>']){
  const prevented=await page.evaluate(markup=>{const parent=document.createElement('div');parent.innerHTML=markup;document.body.append(parent);const target=parent.querySelector('span')||parent.firstElementChild!;const event=new KeyboardEvent('keydown',{key:'ArrowRight',ctrlKey:true,bubbles:true,cancelable:true});target.dispatchEvent(event);parent.remove();return event.defaultPrevented;},markup);
  expect(prevented).toBe(false);
 }
 const gesture=async(dx:number,dy:number)=>page.locator('#main h1').evaluate((target,{dx,dy})=>{const start=new Event('touchstart',{bubbles:true});Object.defineProperty(start,'touches',{value:[{clientX:220,clientY:200}]});target.dispatchEvent(start);const end=new Event('touchend',{bubbles:true});Object.defineProperty(end,'changedTouches',{value:[{clientX:220+dx,clientY:200+dy}]});target.dispatchEvent(end);},{dx,dy});
 await gesture(-100,140);await expect(page.getByRole('tab',{name:'Decisions, 1 open'})).toHaveAttribute('aria-selected','true');
 await gesture(-100,5);await expect(page.getByRole('tab',{name:'FYIs, 0 open'})).toHaveAttribute('aria-selected','true');
});

test('accepted comment stays open and is cleared when another FYI action fails',async({page})=>{
 await rail(page);
 await page.route('**/roles/2/board/decide-items*',route=>route.fulfill({json:{items:[fyi,{...fyi,id:23}],coordinator:'coord',orchestrator:'Child'}}));
 await page.route('**/roles/2/board/fyi-actions',route=>{const {actions}=route.request().postDataJSON();return route.fulfill({json:{results:actions.map((a:any)=>({action_id:a.action_id,item_id:a.item_id,outcome:a.item_id===22?'commented':'invalid'}))}});});
 await page.goto(origin+'/decide#t=launch');await expect(page.locator('#decision-11')).toBeVisible();await page.locator('#main').focus();await page.keyboard.press('Control+ArrowDown');await page.keyboard.press('Control+ArrowRight');
 await page.locator('#note-22').fill('Accepted once');await page.locator('#decision-22').getByRole('button',{name:'Comment',exact:true}).click();
 await page.locator('#decision-23').getByRole('button',{name:'Dismiss',exact:true}).click();await page.getByRole('button',{name:'Submit & close',exact:true}).click();
 await expect(page.locator('#tally')).toContainText('1 queued');await expect(page.locator('#main')).not.toContainText('Accepted once');
 await expect(page.locator('#decision-22')).toContainText('Comment sent · still open');await expect(page.locator('#note-22')).toHaveValue('');
 await expect(page.locator('#decision-22 button[aria-pressed=true]')).toHaveCount(0);
});

test('a new expired ticket clears the old session and keeps the phone return path',async({page})=>{
 await rail(page);await page.addInitScript(()=>sessionStorage.setItem('board-decision-session',JSON.stringify({session:'old-session',initial_role_id:1})));
 const nav:string[]=[];page.on('request',r=>{if(r.url().endsWith('/decision-navigation'))nav.push(r.url());});
 await page.route('**/decision-session',route=>route.fulfill({status:401,json:{error:'expired'}}));
 await page.goto(origin+'/decide#t=expired');await expect(page.locator('#status')).toContainText('session expired');await expect(page.getByRole('link',{name:'Return to Argus'})).toHaveAttribute('href','/');
 expect(await page.evaluate(()=>sessionStorage.getItem('board-decision-session'))).toBeNull();expect(nav).toHaveLength(0);await expect(page).toHaveURL(origin+'/decide');
});

test('poll refresh requires acceptance and retains removed-board drafts as unavailable',async({page})=>{
 await page.clock.install();await rail(page);let removed=false;
 await page.route('**/decision-navigation',route=>route.fulfill({json:{roles:removed?rows.filter(r=>r.role_id!==2):rows}}));
 await page.goto(origin+'/decide#t=launch');await expect(page.locator('#decision-11')).toBeVisible();
 await page.locator('#main').focus();await page.keyboard.press('Control+ArrowDown');await page.keyboard.press('Control+ArrowRight');
 await page.locator('#note-22').fill('Keep across refresh');await page.getByRole('button',{name:'Comment',exact:true}).click();
 removed=true;await page.clock.fastForward(20000);await expect(page.locator('#updates')).toBeVisible();
 await expect(page.locator('#note-22')).toHaveValue('Keep across refresh');
 await page.locator('#refresh').click();await page.getByRole('button',{name:'Review queued',exact:true}).click();
 await expect(page.locator('summary')).toContainText('1 unsent note, its item is unavailable');await page.locator('summary').click();await expect(page.locator('.unsent-recovery')).toContainText('Keep across refresh');
 await expect(page.locator('#tally')).toContainText('0 queued');
});

test('FYI-only initial selection and explicit focus select FYIs; required comments block across tabs',async({page})=>{
 await rail(page);await page.route('**/decision-session',route=>route.fulfill({json:{session:'rail-session',initial_role_id:2}}));
 await page.goto(origin+'/decide?role=2&focus=22#t=launch');await expect(page.locator('#decision-22')).toBeVisible();await expect(page.locator('#decision-22')).toHaveClass(/focused/);
 await page.getByRole('button',{name:'Comment',exact:true}).click();await expect(page.locator('#submit')).toBeDisabled();
 await page.locator('#main').focus();await page.keyboard.press('Control+ArrowUp');await page.keyboard.press('Control+ArrowLeft');
 await expect(page.locator('#decision-11')).toBeVisible();await expect(page.locator('#submit')).toBeDisabled();
 await page.getByRole('button',{name:'Review queued',exact:true}).click();await expect(page.locator('#main')).toContainText('Required note missing');
});

test('deep-linked item marker stays inside its card and clear of the tab hint',async({page},testInfo)=>{
 await rail(page);
 await page.goto(origin+'/decide?role=1&focus=11#t=launch');const card=page.locator('#decision-11');await expect(card).toHaveClass(/focused/);
 const box=(await card.boundingBox())!,style=await card.evaluate(e=>{const c=getComputedStyle(e);return {outline:c.outlineStyle,offset:c.outlineOffset,shadow:c.boxShadow}});
 expect(style.outline==='none'||parseFloat(style.offset)<=0).toBe(true);expect(style.shadow).not.toBe('none');
 const help=page.locator('.tabs-help');if(await help.isVisible()){const h=(await help.boundingBox())!;expect(h.y+h.height).toBeLessThanOrEqual(box.y);}
 await page.screenshot({path:testInfo.outputPath('decide-deeplink-focus.png')});
});

test('visibility refusal cannot unlock a previously uncertain comment before reconciliation',async({page})=>{
 const sends=await rail(page,{failFYI:true});await page.goto(origin+'/decide#t=launch');await expect(page.locator('#decision-11')).toBeVisible();
 await page.locator('#main').focus();await page.keyboard.press('Control+ArrowDown');await page.keyboard.press('Control+ArrowRight');
 await page.locator('#note-22').fill('Immutable until confirmed');await page.getByRole('button',{name:'Comment',exact:true}).click();
 await page.locator('#submit').click();await expect(page.locator('#main h1')).toHaveText('Child');
 const original=sends[0].body.actions[0],retries:any[]=[];let hidden=true;
 await page.route('**/roles/2/board/fyi-actions',route=>{const {actions}=route.request().postDataJSON();retries.push(actions[0]);return hidden?route.fulfill({status:403,json:{error:'Coordinator hidden'}}):route.fulfill({json:{results:[{action_id:actions[0].action_id,item_id:22,outcome:'commented'}]}});});
 await page.locator('#submit').click();await expect(page.locator('.submit-note')).toContainText('0 sent · 1 needs another look');await page.locator('#review').click();await expect(page.locator('#main')).toContainText('Coordinator hidden');
 await expect(page.getByRole('button',{name:'Clear queued response',exact:true})).toBeDisabled();
 hidden=false;await page.locator('#submit-close').click();await expect(page.locator('h1')).toHaveText('Actions sent — you can close this tab');
 expect(retries).toEqual([original,original]);
});

async function mixedBoard(page:Page,{decisions}:{decisions:number}){
 const mixedFyi={...fyi,id:33,role_id:1};
 await page.route('**/*',async route=>{
  const path=new URL(route.request().url()).pathname;
  if(path==='/decide')return route.fulfill({contentType:'text/html',body:shell});
  if(path.endsWith('/decision-session'))return route.fulfill({json:{session:'mixed-session',initial_role_id:1}});
  if(path.endsWith('/decision-navigation'))return route.fulfill({json:{roles:[{role_id:1,parent_role_id:null,orchestrator:'Parent',coordinator:'coord',decisions,fyis:1}]}});
  if(path.endsWith('/decide-items'))return route.fulfill({json:{items:decisions?[decision,mixedFyi]:[mixedFyi],coordinator:'coord',orchestrator:'team'}});
  throw Error('Unexpected '+path);
 });
}

test('focused FYI still opens on Decisions when decisions are open, without highlight',async({page})=>{
 await mixedBoard(page,{decisions:1});await page.goto(origin+'/decide?role=1&focus=33#t=launch');
 await expect(page.locator('#decision-11')).toBeVisible();
 await expect(page.getByRole('tab',{name:/^Decisions/})).toHaveAttribute('aria-selected','true');
 await expect(page.locator('.focused')).toHaveCount(0);
});

test('focused FYI opens on FYIs and is highlighted when no decisions are open',async({page})=>{
 await mixedBoard(page,{decisions:0});await page.goto(origin+'/decide?role=1&focus=33#t=launch');
 await expect(page.locator('#decision-33')).toBeVisible();
 await expect(page.getByRole('tab',{name:/^FYIs/})).toHaveAttribute('aria-selected','true');
 await expect(page.locator('#decision-33')).toHaveClass(/focused/);
});

test('no focus with open decisions opens on Decisions',async({page})=>{
 await mixedBoard(page,{decisions:1});await page.goto(origin+'/decide?role=1#t=launch');
 await expect(page.locator('#decision-11')).toBeVisible();
 await expect(page.getByRole('tab',{name:/^Decisions/})).toHaveAttribute('aria-selected','true');
 await expect(page.locator('.focused')).toHaveCount(0);
});

test('partial submit accepts unchanged items across coordinators and retries only re-answered conflicts',async({page})=>{
 await rail(page);
 const boards=new Map([1,2].map(role=>[role,[{...fyi,id:role*100+1,role_id:role,subject:'Unchanged '+role},{...fyi,id:role*100+2,role_id:role,subject:'Changed '+role}]]));
 const sends:any[]=[];
 await page.route('**/decision-navigation',route=>route.fulfill({json:{roles:rows.slice(0,2).map(r=>({...r,decisions:0,fyis:2}))}}));
 await page.route('**/decide-items*',route=>{const role=Number(route.request().url().match(/roles\/(\d+)/)![1]);return route.fulfill({json:{items:boards.get(role)}});});
 await page.route('**/fyi-actions',route=>{
  const role=Number(route.request().url().match(/roles\/(\d+)/)![1]),{actions}=route.request().postDataJSON();sends.push({role,actions});
  return route.fulfill({json:{results:actions.map((a:any)=>{const item=boards.get(role)!.find(i=>i.id===a.item_id)!;return {action_id:a.action_id,item_id:a.item_id,outcome:a.expected_updated_at===item.updated_at?'commented':'conflict',item};})}});
 });
 await page.goto(origin+'/decide#t=launch');
 for(const role of [1,2]){
  if(role===2){await page.locator('#main').focus();await page.keyboard.press('Control+ArrowDown');}
  for(const item of boards.get(role)!){await page.locator('#note-'+item.id).fill('Preserved note '+item.id);await page.locator('#decision-'+item.id).getByRole('button',{name:'Comment',exact:true}).click();}
 }
 // The backing boards change after the drafts were loaded, before submit.
 for(const items of boards.values())items[1]={...items[1],updated_at:'changed-revision',body:'Updated context'};
 await page.locator('#submit').click();
 await expect(page.locator('.submit-note')).toContainText('2 sent · 2 needs another look');
 await expect(page.locator('#decision-202')).toContainText('Item changed.');
 await expect(page.locator('#main')).not.toContainText('Required note missing or too long');
 expect(sends.map(s=>s.actions.map((a:any)=>a.item_id))).toEqual([[101,102],[201,202]]);
 for(const role of [1,2]){
  if(role===1){await page.locator('#main').focus();await page.keyboard.press('Control+ArrowUp');}
  else {await page.locator('#main').focus();await page.keyboard.press('Control+ArrowDown');}
  const id=role*100+2;await expect(page.locator('#note-'+id)).toHaveValue('Preserved note '+id);
  await page.locator('#decision-'+id).getByRole('button',{name:'Comment',exact:true}).click();
 }
 await page.locator('#submit-close').click();await expect(page.locator('h1')).toHaveText('Actions sent — you can close this tab');
 expect(sends.slice(2).map(s=>s.actions.map((a:any)=>a.item_id))).toEqual([[102],[202]]);
 for(let n=0;n<2;n++){
  expect(sends[n+2].actions[0].action_id).not.toBe(sends[n].actions[1].action_id);
  expect(sends[n+2].actions[0].expected_updated_at).toBe('changed-revision');
 }
 await expect(page.locator('main')).toContainText('4 comments');
});

test('closed feedback explains why it was not saved while open sibling is accepted',async({page})=>{
 await rail(page);
 await page.route('**/roles/2/board/decide-items*',route=>route.fulfill({json:{items:[fyi,{...fyi,id:23}]}}));
 await page.route('**/roles/2/board/fyi-actions',route=>{const {actions}=route.request().postDataJSON();return route.fulfill({json:{results:actions.map((a:any)=>({action_id:a.action_id,item_id:a.item_id,outcome:a.item_id===22?'invalid':'commented',...(a.item_id===22?{item:{...fyi,resolved:true}}:{})}))}});});
 await page.goto(origin+'/decide?role=2#t=launch');await expect(page.locator('#decision-11')).toBeVisible();await page.locator('#main').focus();await page.keyboard.press('Control+ArrowDown');await page.keyboard.press('Control+ArrowRight');
 for(const id of [22,23]){await page.locator('#note-'+id).fill('Keep feedback '+id);await page.locator('#decision-'+id).getByRole('button',{name:'Comment',exact:true}).click();}
 await page.locator('#submit').click();await expect(page.locator('.submit-note')).toContainText('1 sent · 1 needs another look');
 await expect(page.locator('summary')).toContainText('1 unsent note');await page.locator('summary').click();
 await expect(page.locator('.unsent-recovery')).toContainText('Keep feedback 22');
 await expect(page.getByRole('button',{name:'Discard unsent note',exact:true})).toBeEnabled();
});

test('note warnings distinguish response and annotation validity from actual missing or oversized notes',async({page})=>{
 await rail(page);await page.goto(origin+'/decide#t=launch');await expect(page.locator('#decision-11')).toBeVisible();
 const warnings=await page.evaluate(()=>{
  const L=(globalThis as any).BoardDecisions;
  return [
   [{kind:'decision'},{choice:'',action:'',note:'Kept after conflict'}],
   [{kind:'fyi'},{action:'',note:''}],
   [{kind:'fyi'},{action:'comment',note:'Kept',stale:{annotations:[{}]}}],
   [{kind:'fyi'},{action:'comment',note:'',annotations:[{comment:'Range note'}]}],
   [{kind:'decision'},{choice:'other',note:'',annotations:[{}]}],
   [{kind:'fyi'},{action:'comment',note:' '}],
   [{kind:'todo'},{action:'done',note:'界'.repeat(1366)}],
  ].map(([item,draft])=>!!L.noteError(item,draft));
 });
 expect(warnings).toEqual([false,false,false,false,true,true,true]);
});
