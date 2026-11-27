import {test,expect} from '@playwright/test';
import {readFileSync} from 'node:fs';
import {resolve} from 'node:path';

const shell=readFileSync(resolve(__dirname,'../../internal/api/static/decide.html'),'utf8');

test('any kind can be deferred, shown unread and restored without submitting a queued response',async({page})=>{
 const stamp='2026-11-27T00:00:00Z',writes:any[]=[];
 let items=['decision','fyi','action','todo'].map((kind,n)=>({id:n+1,role_id:1,kind,subject:`Item ${kind}`,body:'Review later',read:false,blocking:kind==='action',updated_at:stamp,deferred_at:null as string|null}));
 await page.route('**/*',async route=>{
  const request=route.request(),path=new URL(request.url()).pathname;
  if(path==='/decide')return route.fulfill({contentType:'text/html',body:shell});
  if(path.endsWith('/decision-session'))return route.fulfill({json:{session:'test',initial_role_id:1}});
  if(path.endsWith('/decision-navigation'))return route.fulfill({json:{roles:[{role_id:1,orchestrator:'Test',coordinator:'coord',...Object.fromEntries(['decisions','fyis','actions','todos'].map((key,n)=>[key,items.filter(i=>i.kind===['decision','fyi','action','todo'][n]&&!i.deferred_at).length]))}]}});
  if(path.endsWith('/decide-items'))return route.fulfill({json:{items}});
  if(path.endsWith('/defer')){
   expect(request.method()).toBe('PUT');const body=request.postDataJSON(),id=Number(path.split('/').at(-2));writes.push(body);expect(body.expected_updated_at).toBe(stamp);
   items=items.map(i=>i.id===id?{...i,deferred_at:body.deferred?stamp:null}:i);
   return route.fulfill({json:{item:items.find(i=>i.id===id),unread_count:0}});
  }
  return route.fulfill({status:404});
 });
 await page.goto('http://localhost/decide?role=1#t=test');
 for(const [n,kind]of ['Decisions','FYIs','Actions','Todos'].entries()){
  await page.getByRole('tab',{name:new RegExp(`^${kind},`)}).click();
  const card=page.locator(`#decision-${n+1}`);await expect(card).toBeVisible();
  if(n===2){await card.getByRole('button',{name:'Done',exact:true}).click();await expect(page.locator('#submit')).toBeEnabled();}
  await card.getByRole('button',{name:'Defer',exact:true}).click();
  await expect(card).toHaveCount(0);await expect(page.getByRole('button',{name:'Show deferred (1)',exact:true})).toBeVisible();await expect(page.locator('#submit')).toBeDisabled();
  await page.getByRole('button',{name:'Show deferred (1)',exact:true}).click();await expect(card).toContainText('Deferred');
  await card.getByRole('button',{name:'Restore',exact:true}).click();await expect(card).toBeVisible();
  expect(items[n].read).toBe(false);expect(items[n].updated_at).toBe(stamp);
  await page.getByRole('button',{name:'Hide deferred (0)',exact:true}).click();
 }
 expect(writes).toHaveLength(8);expect(writes.map(b=>b.deferred)).toEqual([true,false,true,false,true,false,true,false]);
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
});
