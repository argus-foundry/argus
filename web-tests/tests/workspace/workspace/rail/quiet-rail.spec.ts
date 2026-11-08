import {test,expect} from '@playwright/test';

async function fixture(page,{storage=true,onlyDone=false,failedBacklog=false}={}){
 if(!storage)await page.addInitScript(()=>{Storage.prototype.getItem=()=>{throw new Error('Blocked');};Storage.prototype.setItem=()=>{throw new Error('Blocked');};});
 const ids=failedBacklog?['current','backlog','child']:onlyDone?['done','child']:['current','pinned','blocked','backlog','done','child','plain','nested'];
 const tasks=ids.map(id=>({id,name:id,project:'fixture',status:'complete',backend:'fixture',needs_input:!failedBacklog&&id==='child',pinned:false}));
 if(onlyDone)tasks.unshift({id:'elsewhere',name:'elsewhere',project:'other',status:'complete',backend:'fixture',needs_input:false,pinned:false});
 const orchestrators=['current','pinned','blocked','backlog','done'].filter(id=>ids.includes(id)).map((id,index)=>({id:index+1,kanban_status:id==='current'||id==='pinned'?'active':id,pinned:id==='pinned',roles:[{role_id:index+1,kind:'coordinator',task_id:id,live:true,status:'idle'}]}));
 orchestrators.find(orch=>orch.kanban_status===(failedBacklog?'backlog':'done')).roles.push({role_id:20,kind:'worker',task_id:'child',live:true,status:failedBacklog?'failed':'blocked'});
 if(!onlyDone&&!failedBacklog)orchestrators.push({id:30,kanban_status:'done',bridge_parent_orch_id:orchestrators[0].id,bridge_parent_role_id:1,roles:[{role_id:30,kind:'coordinator',task_id:'nested',live:true,status:'working'}]});
 await page.route('**/api/**',route=>{
  const path=new URL(route.request().url()).pathname;
  let json={};
  if(path==='/api/tasks')json={tasks};else if(path==='/api/hera')json={orchestrators};
  else if(path.startsWith('/api/projects'))json={projects:[{name:'fixture'}]};else if(path==='/api/backends')json={backends:[]};else if(path==='/api/accounts')json=[];
  else if(path.endsWith('/size'))json={cols:80,rows:24};else if(path.endsWith('/raw'))json=tasks.find(task=>path.includes('/'+task.id+'/'))||{};
  if(path.endsWith('/stream'))return route.fulfill({contentType:'text/event-stream',body:':fixture\n\n'});
  if(path.endsWith('/output'))return route.fulfill({body:'',headers:{'X-Output-Total':'0'}});
  return route.fulfill({json});
 });
 await page.goto(process.env.WORKSPACE_TEST_URL||'http://127.0.0.1:17750');
 await page.locator('#token').fill('fixture');await page.locator('#connect-button').click();
 await expect(page.locator(`.task-row[data-task-id="${failedBacklog?'backlog':'done'}"]`)).toHaveCount(1);
}

test('quiet 220px rail keeps current and blocked visible with deferred attention and stable geometry',async({page})=>{
 await page.setViewportSize({width:1200,height:850});await page.emulateMedia({reducedMotion:'reduce'});
 await page.addInitScript(()=>{if(!localStorage.getItem('argus-workspace:kanban-groups'))localStorage.setItem('argus-workspace:kanban-groups',JSON.stringify({'["fixture","active"]':true,'["fixture","blocked"]':true}));});
 await fixture(page);
 await page.locator('#nav-resize').focus();await page.keyboard.press('Home');
 expect((await page.locator('#navigation').boundingBox()).width).toBe(220);
 const children=page.locator('.project-children').first();
 await expect(children.locator(':scope > .kanban-group > summary')).toHaveText(['Backlog (1)','Done (1), 1 needs input']);
 await expect(page.locator('.kanban-group[data-kanban="active"],.kanban-group[data-kanban="blocked"]')).toHaveCount(0);
 for(const id of ['current','pinned','blocked','plain','nested'])await expect(page.locator(`.task-row[data-task-id="${id}"]`)).toBeVisible();
 await expect(page.locator('.task-triage')).toHaveText('Blocked');
 const done=page.locator('.kanban-group[data-kanban="done"]');
 await expect(done).not.toHaveAttribute('open');await expect(done.locator('summary').first()).toHaveAccessibleName('Done (1), 1 needs input task groups in fixture');
 await expect(page.locator('.project-header')).toHaveClass(/needs-attention/);
 await expect(page.locator('.task-row[data-task-id="current"] .task-role-tag')).toHaveAttribute('aria-label','Agent idle');
 await expect(page.locator('#active-filter')).toContainText('Active');
 const root=await page.locator('.task-row[data-task-id="current"]').boundingBox();
 const plain=await page.locator('.task-row[data-task-id="plain"]').boundingBox();
 const nested=await page.locator('.task-row[data-task-id="nested"]').boundingBox();
 expect(plain.x).toBe(root.x);expect(nested.x-root.x).toBe(12);
 const caret=await done.locator('.kanban-chevron').boundingBox();
 const familyCaret=await page.locator('.project-children > .task-family .task-caret').first().boundingBox();expect(caret.x).toBe(familyCaret.x);
 const bounds=await children.boundingBox();expect(await children.evaluate(el=>el.scrollWidth)).toBeLessThanOrEqual(Math.ceil(bounds.width));
 await page.locator('#navigation').screenshot({path:'.context/quiet-rail-220px.png'});
 await page.getByRole('button',{name:'Select tasks',exact:true}).click();await expect(page.locator('.task-triage')).toHaveText('Blocked');
 const blockedRow=await page.locator('.task-row[data-task-id="blocked"]').boundingBox();
 const blockedCheck=await page.locator('.task-select[data-task-id="blocked"]').boundingBox();
 expect(blockedCheck.y+blockedCheck.height/2).toBe(blockedRow.y+blockedRow.height/2);
 await page.getByRole('button',{name:'Cancel task selection',exact:true}).click();
 const backlog=page.locator('.kanban-group[data-kanban="backlog"]');await expect(backlog).not.toHaveAttribute('open');
 await backlog.locator('summary').click();await page.reload();await expect(backlog).toHaveAttribute('open','');await expect(done).not.toHaveAttribute('open');
 await page.locator('#search').fill('child');
 const child=page.locator('.task-row[data-task-id="child"]');await expect(child).toBeVisible();await expect(child).toHaveAccessibleName(/Done/);
 await page.locator('#search').fill('');await expect(done).not.toHaveAttribute('open');
});

test('all-done project remains reachable when browser storage is denied',async({page})=>{
 await fixture(page,{storage:false,onlyDone:true});
 const done=page.locator('.kanban-group[data-kanban="done"]');await expect(done).not.toHaveAttribute('open');
 await done.locator('summary').first().focus();await page.keyboard.press('ArrowRight');await expect(page.locator('.task-row[data-task-id="child"]')).toBeVisible();
 await page.keyboard.press('ArrowLeft');await expect(done).not.toHaveAttribute('open');
 await page.locator('#search').fill('done');await expect(done).toHaveAttribute('open','');
 await page.locator('#search').fill('');await expect(done).not.toHaveAttribute('open');
});

test('failed-only collapsed Backlog preserves project attention without needs-input labels',async({page})=>{
 await fixture(page,{failedBacklog:true});
 const project=page.locator('.project-group[data-project="fixture"]'),header=project.locator(':scope > .project-header');
 const backlog=project.locator('.kanban-group[data-kanban="backlog"]');
 await expect(backlog).not.toHaveAttribute('open');await expect(backlog.locator(':scope > summary')).toHaveText('Backlog (1), 1 needs attention');
 await expect(project).toHaveClass(/needs-attention/);await expect(header).toHaveClass(/needs-attention/);
 await expect(project).not.toContainText('needs input');
 await header.locator('.project-caret').click();await expect(project).not.toHaveAttribute('open');await expect(header).toHaveClass(/needs-attention/);
 await header.locator('.project-caret').click();await backlog.locator(':scope > summary').click();
 await expect(page.locator('.task-row[data-task-id="child"] .task-status-icon')).toHaveAttribute('aria-label','Failed');
});
