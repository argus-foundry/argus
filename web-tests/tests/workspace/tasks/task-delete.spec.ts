import {test,expect} from '@playwright/test';
import {resetServer} from '../../_helpers';
test.beforeAll(async()=>{await resetServer();});
const pageErrors=new WeakMap();
test.beforeEach(async({page})=>{const errors=[];pageErrors.set(page,errors);page.on('pageerror',error=>errors.push(error.message));});
test.afterEach(async({page})=>{expect(pageErrors.get(page)).toEqual([]);});
async function fixture(page,archived=false){
 let tasks=[{id:'a',name:'Alpha',project:'repo',branch:'feature',worktree_path:'/tmp/task',archived,live:true,pr_state:'awaiting-review',status:'in_progress'},{id:'b',name:'Beta',project:'repo',archived,status:'complete'}];
 const deletes=[];
 await page.route('**/api/**',r=>r.fulfill({status:404,json:{}}));
 await page.route('**/api/projects/full',r=>r.fulfill({json:{projects:[]}}));
 await page.route('**/api/hera',r=>r.fulfill({json:{freelance:[{task_id:'a',role_id:1,name:'worker',kind:'freelance',live:true}]}}));
 await page.route('**/api/tasks?archived=all',r=>r.fulfill({json:{tasks}}));
 await page.route('**/api/tasks/a/git/status',r=>r.fulfill({json:{Status:' M file'}}));
 await page.route('**/api/tasks/a/links',r=>r.fulfill({json:{links:[{url:'https://example.com/pr/1',isPR:true}]}}));
 await page.route('**/api/tasks/a',async r=>{if(r.request().method()==='DELETE'){deletes.push('a');tasks=tasks.filter(t=>t.id!=='a');return r.fulfill({json:{}});}return r.fulfill({json:tasks[0]});});
 await page.goto('http://127.0.0.1:17750');await page.locator('#token').fill('fixture-token');await page.locator('#connect-button').click();
 if(archived)await page.locator('#archive-filter').click();
 await page.locator('.task-row[data-task-id="a"]').waitFor();return deletes;
}
for(const archived of [false,true])test(`rail Delete review in ${archived?'Archived':'Active'} captures task, cancels and deletes with focus`,async({page})=>{
 const deletes=await fixture(page,archived);
 await page.locator('.task-more[data-task-id="a"]').click();await page.getByRole('menuitem',{name:'Delete',exact:true}).click();
 const dialog=page.locator('#delete-dialog');await expect(dialog.getByRole('button',{name:'Cancel',exact:true})).toBeFocused();
 for(const text of ['Alpha','repo','feature','/tmp/task','Live session','worker','1 reported changed file','awaiting-review','force'])await expect(dialog).toContainText(text);
 await expect(dialog).toContainText('Team agents worker attached to a team');await expect(dialog).not.toContainText(/Hera|binding|orchestrator|Freelance/);
 await expect(dialog.getByRole('link',{name:'Pull request'})).toHaveAttribute('href','https://example.com/pr/1');
 await dialog.getByRole('button',{name:'Cancel',exact:true}).click();expect(deletes).toEqual([]);
 await page.locator('.task-more[data-task-id="a"]').click();await page.getByRole('menuitem',{name:'Delete',exact:true}).click();await dialog.getByRole('button',{name:'Delete',exact:true}).click();
 await expect(page.locator('.task-row[data-task-id="a"]')).toHaveCount(0);await expect(page.locator('.task-row[data-task-id="b"]')).toBeFocused();expect(deletes).toEqual(['a']);
});
test('delete failure alerts and retains row',async({page})=>{
 await fixture(page);await page.route('**/api/tasks/a',r=>r.fulfill({status:500,json:{}}));
 await page.locator('.task-more[data-task-id="a"]').click();await page.getByRole('menuitem',{name:'Delete',exact:true}).click();await page.locator('#delete-dialog').getByRole('button',{name:'Delete',exact:true}).click();
 await expect(page.locator('#delete-dialog [role=alert]')).toBeVisible();await expect(page.locator('.task-row[data-task-id="a"]')).toHaveCount(1);
});

test('deleting the open task selects its neighbor and retains another task draft',async({page})=>{
 await fixture(page);
 await page.locator('.task-row[data-task-id="b"]').click();
 await page.locator('#draft').fill('retained draft');
 await page.locator('.task-row[data-task-id="a"]').click();
 await page.locator('.task-more[data-task-id="a"]').click();await page.getByRole('menuitem',{name:'Delete',exact:true}).click();await page.locator('#delete-dialog').getByRole('button',{name:'Delete',exact:true}).click();
 await expect(page.locator('#task-title')).toHaveText('Beta');await expect(page.locator('#draft')).toHaveValue('retained draft');
});
