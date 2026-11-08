import {test,expect} from '@playwright/test';
import {resetServer} from '../../_helpers';
const errors=new WeakMap();
test.beforeAll(async()=>{await resetServer();});
test.beforeEach(async({page})=>{const list=[];errors.set(page,list);page.on('pageerror',error=>list.push(error.message));});
test.afterEach(async({page})=>expect(errors.get(page)).toEqual([]));
async function fixture(page,{archived=true,hold=false,failAll=false}={}){
 let tasks=Array.from({length:140},(_,i)=>({id:String(i),name:`Task ${String(i).padStart(3,'0')}`,project:i<80?'alpha':'beta',archived,status:'complete',live:i===0,pr_state:i===0?'awaiting-review':'',branch:`branch-${i}`,worktree_path:`/tmp/task-${i}`}));
 const calls=[],reads=[],pending=[];let active=0,max=0;
 await page.route('**/api/**',route=>{reads.push(new URL(route.request().url()).pathname);return route.fulfill({status:404,json:{}});});
 await page.route('**/api/projects/full',r=>r.fulfill({json:{projects:[]}}));
 await page.route('**/api/hera',r=>r.fulfill({json:{orchestrators:[{id:1,name:'workspace-team',roles:Array.from({length:70},(_,i)=>({task_id:String(i),role_id:i+1,name:i?'builder-'+i:'coord',kind:i?'worker':'coordinator',live:true}))}]}}));
 await page.route('**/api/tasks?archived=all',r=>r.fulfill({json:{tasks}}));
 await page.route('**/api/tasks/0/links',r=>r.fulfill({json:{links:[{url:'https://example.com/pr/42',isPR:true}]}}));
 await page.route('**/api/tasks/0/meta?namespace=pr',r=>r.fulfill({json:{entries:[]}}));
 await page.route(/\/api\/tasks\/\d+(?:\/archive|\/unarchive)?$/,async r=>{
  const path=new URL(r.request().url()).pathname,id=path.split('/')[3];if(r.request().method()==='GET')return r.fulfill({json:tasks.find(t=>t.id===id)});
  calls.push(id);active++;max=Math.max(max,active);if(failAll){active--;return r.fulfill({status:500,json:{}});}
  if(hold)await new Promise(resolve=>pending.push(resolve));
  tasks=r.request().method()==='DELETE'?tasks.filter(t=>t.id!==id):tasks.map(t=>t.id===id?{...t,archived:!path.endsWith('/unarchive')}:t);active--;return r.fulfill({json:{}});
 });
 await page.goto('http://127.0.0.1:17750');await page.locator('#token').fill('fixture');await page.locator('#connect-button').click();if(archived)await page.locator('#archive-filter').click();await page.locator('.task-row').first().waitFor();
 return {calls,reads,pending,get max(){return max;},get tasks(){return tasks;},arrive(task){tasks.push(task);}};
}
test('140-task tri-state project/coordinator/select-all cascades include collapsed rows and retain search selection',async({page})=>{
 await fixture(page);await page.locator('summary.task-disclosure[data-parent-task-id="0"] > .task-caret').click();await page.getByRole('button',{name:'Select tasks',exact:true}).click();
 const root=page.getByRole('checkbox',{name:'Select Task 000',exact:true}),all=page.getByRole('checkbox',{name:'Select all matching tasks',exact:true});
 await root.check();await expect(page.locator('#task-count')).toHaveText('70 selected');await expect(all).toHaveAttribute('aria-checked','mixed');
 await page.locator('summary.task-disclosure[data-parent-task-id="0"] > .task-caret').click();await page.getByRole('checkbox',{name:'Select Task 001',exact:true}).uncheck();await expect(root).toHaveAttribute('aria-checked','mixed');await expect(page.getByRole('checkbox',{name:'Select project alpha',exact:true})).toHaveAttribute('aria-checked','mixed');
 await page.getByRole('checkbox',{name:'Select project alpha',exact:true}).check();await expect(page.locator('#task-count')).toHaveText('80 selected');
 await page.locator('#search').fill('Task 139');await expect(page.locator('#task-count')).toHaveText('80 selected, 80 not shown');await all.check();await expect(page.locator('#task-count')).toHaveText('81 selected, 80 not shown');
 await page.locator('#search').fill('');await all.check();await expect(page.locator('#task-count')).toHaveText('140 selected');await root.uncheck();await expect(page.locator('#task-count')).toHaveText('70 selected');
 await page.keyboard.press('Escape');await expect(page.locator('.task-select')).toHaveCount(0);
});
test('140-task cached review stays fixed height with grouped attention, safe cached PR link and red header',async({page})=>{
 const state=await fixture(page);await page.locator('.task-row[data-task-id="0"]').click();await expect(page.getByRole('link',{name:'Open pull request',exact:true})).toBeVisible();
 await page.getByRole('button',{name:'Select tasks',exact:true}).click();await page.getByRole('checkbox',{name:'Select all matching tasks',exact:true}).check();
 const action=page.getByRole('button',{name:'Delete selected tasks',exact:true});await expect(action).toHaveClass(/danger/);const before=state.reads.length;await action.click();
 const dialog=page.locator('#bulk-dialog');await expect(dialog.getByRole('button',{name:'Cancel',exact:true})).toBeFocused();await expect(dialog).toContainText('Delete 140 tasks permanently');await expect(dialog.getByRole('heading',{name:'Needs attention (70)',exact:true})).toBeVisible();await expect(dialog).toContainText('workspace-team');await expect(dialog.getByRole('link',{name:'Pull request',exact:true})).toHaveAttribute('href','https://example.com/pr/42');
 await expect(dialog.getByText('Everything else (70)',{exact:true})).toBeVisible();const height=(await dialog.boundingBox()).height;await dialog.getByText('Show list',{exact:true}).click();expect((await dialog.boundingBox()).height).toBe(height);
 expect(state.reads.slice(before).filter(path=>/\/tasks\/\d+\/(git\/status|meta|links)/.test(path))).toEqual([]);
});
for(const archived of [true,false])test(`140-task ${archived?'delete':'archive'} shares progress/Stop and caps concurrency at four`,async({page})=>{
 const state=await fixture(page,{archived,hold:true});await page.getByRole('button',{name:'Select tasks',exact:true}).click();await page.getByRole('checkbox',{name:'Select all matching tasks',exact:true}).check();const action=page.getByRole('button',{name:archived?'Delete selected tasks':'Archive selected tasks',exact:true});if(archived)await expect(action).toHaveClass(/danger/);else await expect(action).not.toHaveClass(/danger/);await action.click();
 const dialog=page.locator('#bulk-dialog');await dialog.getByRole('button',{name:archived?'Delete 140 tasks':'Archive 140 tasks',exact:true}).click();await expect.poll(()=>state.calls.length).toBe(4);expect(state.max).toBe(4);
 await expect(dialog.locator('[role=status]')).toContainText(`0 of 140 ${archived?'deleted':'archived'}`);await dialog.getByRole('button',{name:'Stop',exact:true}).click();for(const release of state.pending.splice(0))release();
 await expect(dialog).toHaveCount(0);expect(state.calls).toHaveLength(4);await expect(page.locator('#task-count')).toHaveText('136 selected');await expect(page.locator('#bulk-alert')).toContainText('136 not started');
});

test('new arrivals remain unchecked and make a fully selected project mixed',async({page})=>{
 const state=await fixture(page);await page.getByRole('button',{name:'Select tasks',exact:true}).click();await page.getByRole('checkbox',{name:'Select all matching tasks',exact:true}).check();
 state.arrive({id:'140',name:'New arrival',project:'alpha',archived:true,status:'complete'});await page.evaluate(()=>document.dispatchEvent(new Event('visibilitychange')));
 await expect(page.locator('.task-row[data-task-id="140"]')).toBeVisible();await expect(page.locator('#task-count')).toHaveText('140 selected');await expect(page.getByRole('checkbox',{name:'Select all matching tasks',exact:true})).toHaveAttribute('aria-checked','mixed');await expect(page.getByRole('checkbox',{name:'Select project alpha',exact:true})).toHaveAttribute('aria-checked','mixed');await expect(page.getByRole('checkbox',{name:'Select New arrival',exact:true})).not.toBeChecked();
});

test('project checkboxes share the row trailing column and completing 140 deletes exits with notice',async({page})=>{
 const state=await fixture(page);await page.getByRole('button',{name:'Select tasks',exact:true}).click();
 const project=await page.getByRole('checkbox',{name:'Select project alpha',exact:true}).boundingBox(),row=await page.getByRole('checkbox',{name:'Select Task 000',exact:true}).boundingBox();expect(Math.abs(project.x+project.width-row.x-row.width)).toBeLessThanOrEqual(1);
 await page.getByRole('checkbox',{name:'Select all matching tasks',exact:true}).check();await page.getByRole('button',{name:'Delete selected tasks',exact:true}).click();await page.locator('#bulk-dialog').getByRole('button',{name:'Delete 140 tasks',exact:true}).click();
 await expect(page.locator('#bulk-dialog')).toHaveCount(0);expect(new Set(state.calls).size).toBe(140);expect(state.calls).toHaveLength(140);await expect(page.getByRole('button',{name:'Select tasks',exact:true})).toBeVisible();await expect(page.locator('#notice')).toHaveText('140 tasks updated.');await expect(page.locator('#archive-filter')).toBeFocused();
});

test('140 ordinary failures stay checked with named reasons in a bounded alert',async({page})=>{
 const state=await fixture(page,{failAll:true});await page.getByRole('button',{name:'Select tasks',exact:true}).click();await page.getByRole('checkbox',{name:'Select all matching tasks',exact:true}).check();await page.getByRole('button',{name:'Delete selected tasks',exact:true}).click();await page.locator('#bulk-dialog').getByRole('button',{name:'Delete 140 tasks',exact:true}).click();
 await expect(page.locator('#bulk-dialog')).toHaveCount(0);await expect(page.locator('#task-count')).toHaveText('140 selected');await expect(page.locator('#bulk-alert')).toContainText('140 failed');await expect(page.locator('#bulk-alert')).toContainText('Task 000');await expect(page.locator('#bulk-alert')).toContainText('Task 139');await expect(page.locator('#bulk-alert')).toContainText('500');expect((await page.locator('#bulk-alert').boundingBox()).height).toBeLessThanOrEqual(140);expect((await page.locator('#task-groups').boundingBox()).height).toBeGreaterThan(0);expect(state.calls).toHaveLength(140);expect(new Set(state.calls).size).toBe(140);
});
