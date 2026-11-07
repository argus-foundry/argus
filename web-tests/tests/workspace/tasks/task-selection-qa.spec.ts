import {test,expect} from '@playwright/test';
import {resetServer} from '../../_helpers';
import {reviewFixture} from './_task-action-review-fixture.ts';
const failures=new WeakMap();
test.beforeAll(async()=>resetServer());
test.beforeEach(async({page})=>{const errors=[];failures.set(page,errors);page.on('pageerror',error=>errors.push(error.message));});
test.afterEach(async({page})=>expect(failures.get(page)).toEqual([]));

test('QA cascade search selects only matches and preserves hidden selections when clearing a parent',async({page})=>{
 await reviewFixture(page,{count:2});await page.getByRole('button',{name:'Select tasks',exact:true}).click();await page.locator('#search').fill('Task 0');const root=page.getByRole('checkbox',{name:'Select Task 0',exact:true});
 await root.check();await expect(page.locator('#task-count')).toHaveText('1 selected');await root.uncheck();await expect(page.locator('#task-count')).toHaveText('0 selected');
 await page.locator('#search').fill('');await page.locator('.task-row[data-task-id="1"]').click();await page.locator('#search').fill('Task 0');await root.check();await expect(page.locator('#task-count')).toHaveText('2 selected, 1 not shown');await root.uncheck();await expect(page.locator('#task-count')).toHaveText('1 selected, 1 not shown');
});
test('QA cascade search traverses a nonmatching intermediate without selecting it',async({page})=>{
 await reviewFixture(page,{count:3,nested:true});await page.getByRole('button',{name:'Select tasks',exact:true}).click();await page.locator('#search').fill('Match');await page.getByRole('checkbox',{name:'Select Match root',exact:true}).check();
 await expect(page.locator('#task-count')).toHaveText('2 selected');await expect(page.getByRole('checkbox',{name:'Select Match leaf',exact:true})).toBeChecked();await page.getByRole('checkbox',{name:'Select Match root',exact:true}).uncheck();await expect(page.locator('#task-count')).toHaveText('0 selected');
});
test('QA single archive and restore retain the neutral shared identity and attention body',async({page})=>{
 const state=await reviewFixture(page,{count:2});state.restore('0');await page.evaluate(()=>document.dispatchEvent(new Event('visibilitychange')));await expect(page.locator('#active-count')).toHaveText('1');await page.locator('#active-filter').click();
 for(const action of ['Archive','Restore']){
  if(action==='Restore')await page.locator('#archive-filter').click();await page.getByRole('button',{name:`${action} Task 0`,exact:true}).click();const dialog=page.locator('#archive-dialog');
  await expect(dialog).not.toHaveClass(/danger-dialog/);await expect(dialog.getByRole('heading',{name:'Needs attention (1)',exact:true})).toBeVisible();await expect(dialog.getByRole('heading',{name:'Everything else (0)',exact:true})).toBeVisible();
  for(const text of ['Task 0','Task ID 0','Project review','Branch branch-0','Worktree /tmp/0'])await expect(dialog.locator('.task-confirm-body')).toContainText(text);
  await expect(dialog.getByRole('button',{name:'Cancel',exact:true})).toBeFocused();await dialog.getByRole('button',{name:`${action} task`,exact:true}).click();await expect(dialog).toHaveCount(0);
 }
 expect(state.writes).toEqual([['POST','/api/tasks/0/archive'],['POST','/api/tasks/0/unarchive']]);
});
for(const change of ['remove','restore'])test(`QA snapshot render prunes a hidden selected task after ${change}`,async({page})=>{
 const state=await reviewFixture(page,{count:3});await page.getByRole('button',{name:'Select tasks',exact:true}).click();await page.locator('.task-row[data-task-id="1"]').click();await page.locator('.task-row[data-task-id="2"]').click();await page.locator('#search').fill('Task 0');await expect(page.locator('#task-count')).toHaveText('2 selected, 2 not shown');
 state[change]('1');await page.evaluate(()=>document.dispatchEvent(new Event('visibilitychange')));await expect(page.locator('#archive-count')).toHaveText('2');await expect(page.locator('#task-count')).toHaveText('1 selected, 1 not shown');await expect(page.getByRole('button',{name:'Delete selected tasks',exact:true})).toBeEnabled();
 state[change]('2');await page.evaluate(()=>document.dispatchEvent(new Event('visibilitychange')));await expect(page.locator('#archive-count')).toHaveText('1');await expect(page.locator('#task-count')).toHaveText('0 selected');await expect(page.getByRole('button',{name:'Delete selected tasks',exact:true})).toBeDisabled();
});
