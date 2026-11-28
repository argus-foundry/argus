import {test,expect,Page} from '@playwright/test';
import {readFileSync} from 'node:fs';
import {resolve} from 'node:path';

const shell=readFileSync(resolve(__dirname,'../../internal/api/static/decide.html'),'utf8');
const command='echo  "keep **literal**"\nOpen the Settings panel';

async function setupWork(page:Page){
 const items=[
  {id:11,role_id:1,kind:'action',blocking:true,command,subject:'Unlock the host',body:'Human-only step',updated_at:'2026-11-20T00:00:00Z'},
  {id:12,role_id:1,kind:'todo',trigger:'after release',trigger_pending:true,subject:'Team follow-up',body:'Aaron does nothing now',updated_at:'2026-11-20T00:00:00Z'}
 ];
 const sends:any[]=[];let uncertain=false,fail=false;
 await page.addInitScript(()=>{Object.defineProperty(navigator,'clipboard',{value:{writeText:async(value:string)=>{(globalThis as any).copiedStep=value;}}});window.close=()=>{};});
 await page.route('**/*',async route=>{
  const path=new URL(route.request().url()).pathname;
  if(path==='/decide')return route.fulfill({contentType:'text/html',body:shell});
  if(path.endsWith('decision-session'))return route.fulfill({json:{session:'test-session',initial_role_id:1}});
  if(path.endsWith('decision-navigation'))return route.fulfill({json:{roles:[{role_id:1,orchestrator:'Test',coordinator:'coord',decisions:0,fyis:0,actions:items.filter(i=>i.kind==='action').length,todos:items.filter(i=>i.kind==='todo').length}]}});
  if(path.endsWith('decide-items'))return route.fulfill({json:{items}});
  if(path.endsWith('/fyi-actions')){
   const actions=route.request().postDataJSON().actions;sends.push(...actions);
   if(fail){uncertain=true;return route.abort();}
   return route.fulfill({json:{results:actions.map((a:any)=>{
    const at=items.findIndex(i=>i.id===a.item_id);if(at>=0)items.splice(at,1);
    return {item_id:a.item_id,action_id:a.action_id,outcome:'resolved'};
   })}});
  }
  return route.fulfill({status:404});
 });
 await page.goto('http://localhost/decide?role=1&focus=11#t=launch');
 await expect(page.locator('#decision-11')).toBeVisible();
 return {sends,setFailure:(value:boolean)=>{fail=value;},uncertain:()=>uncertain};
}

test('action exact copy and todo dropped share the response queue on desktop and phone',async({page})=>{
 const {sends}=await setupWork(page);
 await expect(page.getByRole('tab',{name:'Actions, 1 open',exact:true})).toHaveAttribute('aria-selected','true');
 await expect(page.locator('#decision-11')).toContainText('Blocking');
 await expect(page.locator('#decision-11 pre')).toHaveText(command);
 await expect(page.locator('#decision-11 .annotation-trigger')).toHaveCount(0);
 await page.getByRole('button',{name:'Copy step for Action #11',exact:true}).click();
 expect(await page.evaluate(()=>(globalThis as any).copiedStep)).toBe(command);
 await page.getByRole('button',{name:'Done',exact:true}).click();
 await page.getByRole('tab',{name:'Todos, 1 open',exact:true}).click();
 await expect(page.locator('#decision-12')).toContainText('Waiting for: after release');
 await page.locator('#note-12').fill('No longer needed');
 await page.getByRole('button',{name:'Dropped',exact:true}).click();
 await page.locator('#review').click();
 await expect(page.locator('#main')).toContainText('Done');
 await expect(page.locator('#main')).toContainText('Dropped');
 await page.locator('#submit-close').click();
 await expect(page.locator('h1')).toHaveText('Actions sent — you can close this tab');
 expect(sends.map(a=>({action:a.action,note:a.note}))).toEqual([{action:'done',note:''},{action:'dropped',note:'No longer needed'}]);
 expect(sends.every(a=>a.action_id&&a.expected_updated_at&&!a.annotations)).toBe(true);
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
});

test('uncertain action completion stays frozen over reload and replays identical payload',async({page})=>{
 const state=await setupWork(page);state.setFailure(true);
 await page.getByRole('button',{name:'Done',exact:true}).click();
 await page.locator('#submit').click();
 await expect(page.locator('.submit-note')).toContainText('0 sent · 1 needs another look');
 expect(state.uncertain()).toBe(true);
 await page.reload();
 await expect(page.locator('#note-11')).toBeDisabled();
 await expect(page.getByRole('button',{name:'Done',exact:true})).toBeDisabled();
 state.setFailure(false);await page.locator('#submit-close').click();
 await expect(page.locator('h1')).toHaveText('Actions sent — you can close this tab');
 expect(state.sends).toHaveLength(2);expect(state.sends[1]).toEqual(state.sends[0]);
});
