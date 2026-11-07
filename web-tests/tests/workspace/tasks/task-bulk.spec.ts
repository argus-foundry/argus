import {test,expect} from '@playwright/test';
import {resetServer} from '../../_helpers';
test.beforeAll(async()=>{await resetServer();});
const pageErrors=new WeakMap();
test.beforeEach(async({page})=>{const errors=[];pageErrors.set(page,errors);page.on('pageerror',error=>errors.push(error.message));});
test.afterEach(async({page})=>{expect(pageErrors.get(page)).toEqual([]);});
async function fixture(page,{archived=false,fail='',status=500,delay=0}={}){
 let tasks=['a','b','c'].map(id=>({id,name:`Task ${id}`,project:'repo',archived,live:id==='a',status:'in_progress',worktree_path:`/tmp/${id}`,branch:id,pr_state:id==='a'?'awaiting-review':''}));
 const calls=[];
 await page.route('**/api/**',r=>r.fulfill({status:404,json:{}}));
 await page.route('**/api/projects/full',r=>r.fulfill({json:{projects:[]}}));
 await page.route('**/api/hera',r=>r.fulfill({json:{orchestrators:[{id:1,roles:[{task_id:'a',role_id:1,kind:'coordinator',name:'coord',live:true},{task_id:'b',role_id:2,kind:'worker',name:'worker',live:true}]}]}}));
 await page.route('**/api/tasks?archived=all',r=>r.fulfill({json:{tasks}}));
 await page.route(/\/api\/tasks\/[^/]+(?:\/archive)?$/,async r=>{
  const path=new URL(r.request().url()).pathname,id=path.split('/')[3];
  if(!['POST','DELETE'].includes(r.request().method()))return r.fulfill({json:tasks.find(t=>t.id===id)||{}});
  calls.push([id,r.request().method(),path]);
  if(delay)await new Promise(resolve=>setTimeout(resolve,delay*(id==='b'?4:1)));
  if(id===fail){if(!status)return r.abort('connectionfailed');return r.fulfill({status,json:{}});}
  tasks=r.request().method()==='DELETE'?tasks.filter(t=>t.id!==id):tasks.map(t=>t.id===id?{...t,archived:true,pinned:false}:t);
  return r.fulfill({json:{}});
 });
 await page.goto('http://127.0.0.1:17750');await page.locator('#token').fill('fixture-token');await page.locator('#connect-button').click();
 if(archived)await page.locator('#archive-filter').click();await page.locator('.task-row[data-task-id="a"]').waitFor();return calls;
}
async function select(page,...ids){
 await page.getByRole('button',{name:'Select tasks',exact:true}).click();
 for(const id of ids)await page.locator(`.task-row[data-task-id="${id}"]`).click();
}
test('Select uses existing action space, keeps status and viewer, Space toggles and Escape exits',async({page})=>{
 await fixture(page);await page.locator('#draft').fill('keep draft');
 const title=await page.locator('#task-title').textContent();
 const before=await page.locator('.task-row[data-task-id="a"] .task-status-icon').boundingBox();
 await select(page,'a');await expect(page.locator('#task-count')).toHaveText('1 selected');
 await expect(page.locator('.task-more')).toHaveCount(0);await expect(page.locator('.task-archive')).toHaveCount(0);
 expect((await page.locator('.task-row[data-task-id="a"] .task-status-icon').boundingBox())?.x).toBe(before?.x);
 await page.locator('.task-row[data-task-id="b"]').click();await expect(page.locator('#task-count')).toHaveText('2 selected');
 await expect(page.locator('#task-title')).toHaveText(title);await expect(page.locator('#draft')).toHaveValue('keep draft');
 const checkbox=page.getByRole('checkbox',{name:'Select Task b',exact:true});await checkbox.focus();await page.keyboard.press('Space');await expect(checkbox).not.toBeChecked();
 await page.keyboard.press('ArrowDown');await expect(page.getByRole('checkbox',{name:'Select Task c',exact:true})).toBeFocused();
 await page.keyboard.press('Escape');await expect(page.getByRole('button',{name:'Select tasks',exact:true})).toBeFocused();await expect(page.locator('.task-more')).toHaveCount(3);
});
test('disclosures retain selection and search announces hidden selected IDs',async({page})=>{
 await fixture(page);await select(page,'b');await expect(page.getByRole('checkbox',{name:'Select Task a',exact:true})).not.toBeChecked();
 await page.locator('summary.task-disclosure[data-parent-task-id="a"] > .task-caret').click();await expect(page.locator('#task-count')).toHaveText('1 selected');
 await page.locator('summary.task-disclosure[data-parent-task-id="a"] > .task-caret').click();await expect(page.getByRole('checkbox',{name:'Select Task b',exact:true})).toBeChecked();await expect(page.locator('.task-row[data-task-id="b"]')).toHaveClass(/batch-selected/);
 await page.locator('#search').fill('Task c');await expect(page.locator('#task-count')).toHaveText('1 selected, 1 not shown');await expect(page.getByRole('button',{name:'Cancel task selection',exact:true})).toBeVisible();
});
test('Active batch archives explicitly, reports progress and retains open task draft',async({page})=>{
 const calls=await fixture(page,{delay:250});await page.locator('#draft').fill('archive draft');await select(page,'a','b');
 await page.getByRole('button',{name:'Archive selected tasks',exact:true}).click();const dialog=page.locator('#bulk-dialog');
 await expect(dialog).toContainText('Task a');await expect(dialog).toContainText('Task b');await expect(dialog).toContainText('team attachments');await expect(dialog).not.toContainText(/Hera|orchestrator|binding/);await expect(dialog.getByRole('button',{name:'Cancel',exact:true})).toBeFocused();
 await dialog.getByRole('button',{name:'Archive 2 tasks',exact:true}).click();await expect(dialog.locator('[role=status]')).toContainText('1 of 2');await expect(dialog).toHaveCount(0);
 expect(calls.map(c=>c.slice(0,2))).toEqual([['a','POST'],['b','POST']]);expect(calls.every(c=>c[2].endsWith('/archive'))).toBe(true);
 await expect(page.locator('#task-title')).toHaveText('Task a');await expect(page.locator('#draft')).toHaveValue('archive draft');await expect(page.locator('#send')).toBeDisabled();
});
test('Archived bulk Delete includes live/Hera tasks and confirms every identity',async({page})=>{
 const calls=await fixture(page,{archived:true});await select(page,'a','b');await page.getByRole('button',{name:'Delete selected tasks',exact:true}).click();
 const dialog=page.locator('#bulk-dialog');await expect(dialog).toHaveClass(/danger-dialog/);await expect(dialog).toContainText('Live session');await expect(dialog).toContainText('coord');await expect(dialog).toContainText('worker');
 await expect(dialog.locator('input')).toHaveCount(0);await dialog.getByRole('button',{name:'Delete 2 tasks',exact:true}).click();await expect(dialog).toHaveCount(0);
 expect(calls.map(c=>c.slice(0,2))).toEqual([['a','DELETE'],['b','DELETE']]);await expect(page.locator('.task-row[data-task-id="c"]')).toBeFocused();
});
test('partial failure keeps only failed tasks selected with an alert',async({page})=>{
 const calls=await fixture(page,{archived:true,fail:'a'});await select(page,'a','b');await page.getByRole('button',{name:'Delete selected tasks',exact:true}).click();
 await page.locator('#bulk-dialog').getByRole('button',{name:'Delete 2 tasks',exact:true}).click();await expect(page.locator('#bulk-dialog')).toHaveCount(0);
 await expect(page.locator('#bulk-alert')).toContainText('1 failed');await expect(page.getByRole('checkbox',{name:'Select Task a',exact:true})).toBeChecked();await expect(page.locator('.task-row[data-task-id="b"]')).toHaveCount(0);expect(calls).toHaveLength(2);
});
for(const status of [401,0])test(`connection/auth loss ${status} retains failure while in-flight successes finish`,async({page})=>{
 const calls=await fixture(page,{archived:true,fail:'a',status});await select(page,'a','b','c');await page.getByRole('button',{name:'Delete selected tasks',exact:true}).click();
 await page.locator('#bulk-dialog').getByRole('button',{name:'Delete 3 tasks',exact:true}).click();await expect(page.locator('#bulk-dialog')).toHaveCount(0);
 await expect(page.locator('#bulk-alert')).toContainText('1 failed');await expect(page.locator('#bulk-alert')).toContainText('0 not started');expect(calls).toHaveLength(3);await expect(page.locator('#task-count')).toHaveText('1 selected');
});

test('batch review cancels without mutations, returns action focus, then filter clears selection',async({page})=>{
 const calls=await fixture(page);await select(page,'a');await page.getByRole('button',{name:'Archive selected tasks',exact:true}).click();
 await page.locator('#bulk-dialog').getByRole('button',{name:'Cancel',exact:true}).click();expect(calls).toEqual([]);await expect(page.getByRole('button',{name:'Archive selected tasks',exact:true})).toBeFocused();
 await page.locator('#archive-filter').click();await expect(page.getByRole('button',{name:'Select tasks',exact:true})).toBeVisible();await expect(page.locator('.task-select')).toHaveCount(0);
});
test('bulk deletion of every archived row focuses Archived filter',async({page})=>{
 const calls=await fixture(page,{archived:true});await select(page,'a','b','c');await page.getByRole('button',{name:'Delete selected tasks',exact:true}).click();await page.locator('#bulk-dialog').getByRole('button',{name:'Delete 3 tasks',exact:true}).click();
 await expect(page.locator('#bulk-dialog')).toHaveCount(0);await expect(page.locator('.task-row')).toHaveCount(0);await expect(page.locator('#archive-filter')).toBeFocused();expect(calls).toHaveLength(3);
});
for(const width of [220,380])test(`Select mode fits ${width}px rail with native controls`,async({page})=>{
 await fixture(page);await page.evaluate(width=>document.documentElement.style.setProperty('--nav-width',width+'px'),width);await select(page,'a');
 const summary=await page.locator('.nav-summary').boundingBox(),cancel=await page.getByRole('button',{name:'Cancel task selection',exact:true}).boundingBox();expect(cancel.x+cancel.width).toBeLessThanOrEqual(summary.x+summary.width+1);
 await expect(page.getByRole('checkbox',{name:'Select Task a',exact:true})).toHaveAttribute('aria-checked','mixed');
});

test('entering Select with all groups collapsed keeps focus on Cancel',async({page})=>{
 await fixture(page);await page.locator('.project-header .project-caret').click();await page.getByRole('button',{name:'Select tasks',exact:true}).click();
 await expect(page.getByRole('button',{name:'Cancel task selection',exact:true})).toBeFocused();await page.keyboard.press('Escape');await expect(page.getByRole('button',{name:'Select tasks',exact:true})).toBeFocused();
});
