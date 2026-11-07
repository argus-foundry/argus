import {test,expect} from '@playwright/test';
const task=id=>({id,name:id,project:'fixture',status:'complete',backend:'fixture',pinned:false});
test('kanban groups persist disclosures and shared menus mutate orchestrators with rollback and focus',async({page})=>{
 const tasks=['plain','active','pinned','backlog','blocked','done','worker'].map(task);
 const orchestrators=['active','pinned','backlog','blocked','done'].map((id,i)=>({id:i+1,pinned:id==='pinned',kanban_status:id==='pinned'?'active':id,roles:[{role_id:i+1,kind:'coordinator',task_id:id,live:true}]}));
 orchestrators[0].roles.push({role_id:20,kind:'worker',task_id:'worker',live:true});
 const writes=[];let fail=false;
 await page.route('**/api/**',async route=>{
  const request=route.request(),path=new URL(request.url()).pathname;
  if(request.method()==='POST'&&path.startsWith('/api/hera/')){
   writes.push({path,body:request.postDataJSON(),auth:request.headers().authorization});
   if(fail)return route.fulfill({status:503,json:{}});
   const orch=orchestrators.find(o=>o.id===Number(path.split('/')[4]));
   const op=path.split('/')[5];if(op==='kanban')orch.kanban_status=request.postDataJSON().status;else orch.pinned=op==='pin';
   return route.fulfill({json:{}});
  }
  let json={};
  if(path==='/api/tasks')json={tasks};else if(path==='/api/hera')json={orchestrators};
  else if(path.startsWith('/api/projects'))json={projects:[{name:'fixture'}]};else if(path==='/api/backends')json={backends:[]};else if(path==='/api/accounts')json=[];
  else if(path.endsWith('/size'))json={cols:80,rows:24};else if(path.endsWith('/raw'))json=tasks.find(t=>path.includes('/'+t.id+'/'))||{};
  if(path.endsWith('/stream'))return route.fulfill({contentType:'text/event-stream',body:':fixture\n\n'});
  if(path.endsWith('/output'))return route.fulfill({body:'',headers:{'X-Output-Total':'0'}});
  return route.fulfill({json});
 });
 await page.goto(process.env.WORKSPACE_TEST_URL||'http://127.0.0.1:17750');
 await page.locator('#token').fill('fixture');await page.locator('#connect-button').click();
 const groups=page.locator('.kanban-group');await expect(groups).toHaveCount(2);
 await expect(groups.locator('> summary')).toHaveText(['Backlog (1)','Done (1)']);
 expect(await page.locator('.project-children > .task-family, .project-children > .task-item').locator('.task-row').evaluateAll(rows=>rows.map(r=>r.dataset.taskId))).toEqual(['pinned','active','worker','blocked','plain']);
 const done=page.locator('.kanban-group[data-kanban="done"]');await expect(done).not.toHaveAttribute('open');
 await done.locator(':scope > summary').click();await page.reload();await expect(done).toHaveAttribute('open','');
 const menu=()=>page.getByRole('button',{name:'Task actions for active',exact:true});
 await menu().focus();await page.keyboard.press('ArrowDown');
 await page.getByRole('menuitem',{name:'Move to Done',exact:true}).click();
 await expect(done.locator('.task-row[data-task-id="active"]')).toBeVisible();
 expect(writes.at(-1)).toEqual({path:'/api/hera/orchestrators/1/kanban',body:{status:'done'},auth:'Bearer fixture'});
 await expect(menu()).toBeFocused();
 await menu().click();await page.getByRole('menuitem',{name:'Pin',exact:true}).click();
 await expect(done.locator('.task-row').first()).toHaveAttribute('data-task-id','active');
 expect(writes.at(-1).path).toBe('/api/hera/orchestrators/1/pin');
 await menu().click();await page.getByRole('menuitem',{name:'Unpin',exact:true}).click();
 await expect(done.locator('.task-row[data-task-id="active"] .task-pin')).toHaveCount(0);
 expect(writes.at(-1).path).toBe('/api/hera/orchestrators/1/unpin');
 fail=true;await menu().click();await page.getByRole('menuitem',{name:'Move to Blocked',exact:true}).click();
 await expect(page.locator('#notice')).toContainText('not confirmed');
 await expect(done.locator('.task-row[data-task-id="active"]')).toBeVisible();
 await expect(menu()).toBeFocused();
 fail=false;await menu().click();await page.getByRole('menuitem',{name:'Move to In progress',exact:true}).click();
 await expect(page.locator('.project-children > .task-family .task-row[data-task-id="active"]')).toBeVisible();
 await done.locator(':scope > summary').click();await menu().click();await page.getByRole('menuitem',{name:'Move to Done',exact:true}).click();
 await expect(done.locator(':scope > summary')).toBeFocused();await expect(done.locator('.task-row[data-task-id="active"]')).not.toBeVisible();
 await page.keyboard.press('ArrowRight');await expect(done.locator('.task-row[data-task-id="active"]')).toBeVisible();
 await page.locator('#search').fill('plain');await expect(page.locator('.kanban-group')).toHaveCount(0);await expect(page.locator('.task-row')).toHaveCount(1);
});

test('search and Back/Forward reveal a Done descendant while clearing search restores disclosures',async({page})=>{
 const tasks=['plain','done-parent','done-child'].map(task);
 const orchestrators=[{id:1,kanban_status:'done',roles:[{role_id:1,kind:'coordinator',task_id:'done-parent',live:true},{role_id:2,kind:'worker',task_id:'done-child',live:true}]}];
 await page.route('**/api/**',route=>{
  const path=new URL(route.request().url()).pathname;
  let json={};
  if(path==='/api/tasks')json={tasks};else if(path==='/api/hera')json={orchestrators};
  else if(path.startsWith('/api/projects'))json={projects:[{name:'fixture'}]};else if(path==='/api/backends')json={backends:[]};else if(path==='/api/accounts')json=[];
  else if(path.endsWith('/size'))json={cols:80,rows:24};else if(path.endsWith('/raw'))json=tasks.find(t=>path.includes('/'+t.id+'/'))||{};
  if(path.endsWith('/stream'))return route.fulfill({contentType:'text/event-stream',body:':fixture\n\n'});
  if(path.endsWith('/output'))return route.fulfill({body:'',headers:{'X-Output-Total':'0'}});
  return route.fulfill({json});
 });
 await page.goto(process.env.WORKSPACE_TEST_URL||'http://127.0.0.1:17750');
 await page.locator('#token').fill('fixture');await page.locator('#connect-button').click();
 const done=page.locator('.kanban-group[data-kanban="done"]');
 const family=done.locator('.task-family');
 const parent=page.locator('.task-row[data-task-id="done-parent"]');
 const child=page.locator('.task-row[data-task-id="done-child"]');
 const plain=page.locator('.task-row[data-task-id="plain"]');
 await expect(done).not.toHaveAttribute('open');
 await done.locator(':scope > summary').click();
 const childX=(await child.boundingBox()).x,parentX=(await parent.boundingBox()).x;
 expect(childX-parentX).toBe(12);
 await family.locator(':scope > summary').focus();await page.keyboard.press('ArrowLeft');
 await done.locator(':scope > summary').click();
 await expect(child).not.toBeVisible();
 await page.locator('#search').fill('done');
 await expect(done).toHaveAttribute('open','');await expect(family).toHaveAttribute('open','');await expect(child).toBeVisible();
 expect((await child.boundingBox()).x).toBe(childX);
 await page.locator('#search').fill('');
 await expect(done).not.toHaveAttribute('open');await expect(family).not.toHaveAttribute('open');await expect(child).not.toBeVisible();
 await plain.click();
 await done.locator(':scope > summary').click();
 await family.locator(':scope > summary').focus();await page.keyboard.press('ArrowRight');
 await child.click();await expect(page.locator('#task-title')).toHaveText('done-child');
 await plain.click();await expect(page.locator('#task-title')).toHaveText('plain');
 const collapse=async()=>{
  await family.locator(':scope > summary').focus();await page.keyboard.press('ArrowLeft');
  await done.locator(':scope > summary').click();await expect(child).not.toBeVisible();
 };
 await collapse();
 await page.goBack();await expect(page.locator('#task-title')).toHaveText('done-child');
 await expect(done).toHaveAttribute('open','');await expect(family).toHaveAttribute('open','');await expect(child).toBeVisible();
 expect((await child.boundingBox()).x).toBe(childX);
 await page.goBack();await expect(page.locator('#task-title')).toHaveText('plain');
 await collapse();
 await page.goForward();await expect(page.locator('#task-title')).toHaveText('done-child');
 await expect(done).toHaveAttribute('open','');await expect(family).toHaveAttribute('open','');await expect(child).toBeVisible();
 expect((await child.boundingBox()).x).toBe(childX);
});

async function rollbackFixture(page,ids){
 const tasks=ids.map(task),orchestrators=ids.map((id,index)=>({id:index+1,kanban_status:'active',pinned:false,roles:[{role_id:index+1,kind:'coordinator',task_id:id,live:true}]}));
 const writes=[];let rejectWrite;
 const failure=new Promise(resolve=>rejectWrite=resolve);
 await page.route('**/api/**',async route=>{
  const request=route.request(),path=new URL(request.url()).pathname;
  if(request.method()==='POST'&&path.startsWith('/api/hera/')){
   writes.push({path,body:request.postDataJSON(),auth:request.headers().authorization});
   await failure;return route.fulfill({status:503,json:{}});
  }
  let json={};
  if(path==='/api/tasks')json={tasks};else if(path==='/api/hera')json={orchestrators};
  else if(path.startsWith('/api/projects'))json={projects:[{name:'fixture'}]};else if(path==='/api/backends')json={backends:[]};else if(path==='/api/accounts')json=[];
  else if(path.endsWith('/size'))json={cols:80,rows:24};else if(path.endsWith('/raw'))json=tasks.find(t=>path.includes('/'+t.id+'/'))||{};
  if(path.endsWith('/stream'))return route.fulfill({contentType:'text/event-stream',body:':fixture\n\n'});
  if(path.endsWith('/output'))return route.fulfill({body:'',headers:{'X-Output-Total':'0'}});
  return route.fulfill({json});
 });
 await page.goto(process.env.WORKSPACE_TEST_URL||'http://127.0.0.1:17750');
 await page.locator('#token').fill('fixture');await page.locator('#connect-button').click();
 await expect(page.locator('.task-row')).toHaveCount(ids.length);
 return {writes,rejectWrite};
}

test('failed pin restores rendered pin, ordering, menu action and focus',async({page})=>{
 const {writes,rejectWrite}=await rollbackFixture(page,['other','active']);
 const rows=page.locator('.project-children > .task-item .task-row');
 const menu=page.getByRole('button',{name:'Task actions for active',exact:true});
 const pin=page.locator('.task-row[data-task-id="active"] .task-pin');
 await expect(rows.first()).toHaveAttribute('data-task-id','other');await expect(pin).toHaveCount(0);
 await menu.click();await page.getByRole('menuitem',{name:'Pin',exact:true}).click();
 try{
  await expect(pin).toHaveCount(1);await expect(rows.first()).toHaveAttribute('data-task-id','active');await expect(menu).toBeFocused();
 }finally{rejectWrite();}
 await expect(page.locator('#notice')).toContainText('not confirmed');
 await expect(pin).toHaveCount(0);await expect(rows.first()).toHaveAttribute('data-task-id','other');await expect(menu).toBeFocused();
 await menu.click();await expect(page.getByRole('menuitem',{name:'Pin',exact:true})).toBeEnabled();
 await expect(page.getByRole('menuitem',{name:'Unpin',exact:true})).toHaveCount(0);
 expect(writes).toEqual([{path:'/api/hera/orchestrators/2/pin',body:{},auth:'Bearer fixture'}]);
});

test('failed move to empty collapsed Done returns focus to the source action',async({page})=>{
 const {writes,rejectWrite}=await rollbackFixture(page,['active']);
 const done=page.locator('.kanban-group[data-kanban="done"]');
 const active=page.locator('.kanban-group[data-kanban="active"]');
 const menu=page.getByRole('button',{name:'Task actions for active',exact:true});
 await expect(done).toHaveCount(0);
 await menu.click();await page.getByRole('menuitem',{name:'Move to Done',exact:true}).click();
 try{
  await expect(done).toHaveCount(1);await expect(done).not.toHaveAttribute('open');
  await expect(done.locator(':scope > summary')).toBeFocused();await expect(active).toHaveCount(0);
 }finally{rejectWrite();}
 await expect(page.locator('#notice')).toContainText('not confirmed');await expect(done).toHaveCount(0);
 await expect(page.locator('.task-row[data-task-id="active"]')).toBeVisible();await expect(menu).toBeFocused();
 expect(writes).toEqual([{path:'/api/hera/orchestrators/1/kanban',body:{status:'done'},auth:'Bearer fixture'}]);
});
