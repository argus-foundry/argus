import {reviewFixture as fixture,bulkReview as bulk} from './_task-action-review-fixture.ts';
import {test,expect} from '@playwright/test';
import {resetServer} from '../../_helpers';
const failures=new WeakMap();
test.beforeAll(async()=>resetServer());
test.beforeEach(async({page})=>{const errors=[];failures.set(page,errors);page.on('pageerror',error=>errors.push(error.message));});
test.afterEach(async({page})=>expect(failures.get(page)).toEqual([]));
for(const sse of [false,true])test(`140 deletes do not open pending targets or read their viewers, SSE ${sse}`,async({page})=>{
 const state=await fixture(page,{sse});await page.locator('.task-row[data-task-id="0"]').click();await expect(page.locator('#task-title')).toHaveText('Task 0');await bulk(page);const before=state.reads.length;
 await page.locator('#bulk-dialog').getByRole('button',{name:'Delete 140 tasks',exact:true}).click();await expect(page.locator('#bulk-dialog')).toHaveCount(0);expect(state.calls).toHaveLength(140);
 expect(state.reads.slice(before).filter(path=>/^\/api\/tasks\/(?!0\/)[^/]+\/(links|meta|clipboard|output|raw|bridge)/.test(path))).toEqual([]);
});
test('restoring a captured task invalidates the pending delete review',async({page})=>{
 const state=await fixture(page,{count:5});await bulk(page);state.restore('4');await page.evaluate(()=>document.dispatchEvent(new Event('visibilitychange')));await expect(page.locator('.task-row[data-task-id="4"]')).toHaveCount(0);
 await expect(page.locator('#bulk-dialog')).toHaveCount(0);await expect(page.locator('#bulk-alert')).toContainText('scope');expect(state.calls).toEqual([]);
});
test('a restored queued task never receives DELETE after the first four finish',async({page})=>{
 const state=await fixture(page,{count:5,hold:true});await bulk(page);await page.locator('#bulk-dialog').getByRole('button',{name:'Delete 5 tasks',exact:true}).click();await expect.poll(()=>state.calls.length).toBe(4);
 state.restore('4');await page.evaluate(()=>document.dispatchEvent(new Event('visibilitychange')));await expect(page.locator('.task-row[data-task-id="4"]')).toHaveCount(0);for(const release of state.pending.splice(0))release();
 await expect(page.locator('#bulk-dialog')).toHaveCount(0);expect(state.calls).toEqual(['0','1','2','3']);await expect(page.locator('#bulk-alert')).toContainText('scope');
});
test('progress focuses Stop and a failed single delete focuses its error summary',async({page})=>{
 const state=await fixture(page,{count:1,hold:true,fail:true});await page.locator('.task-more').click();await page.getByRole('menuitem',{name:'Delete',exact:true}).click();const dialog=page.locator('#delete-dialog');await dialog.getByRole('button',{name:'Delete',exact:true}).focus();await page.keyboard.press('Enter');
 await expect(dialog.getByRole('button',{name:'Stop',exact:true})).toBeFocused();for(const release of state.pending.splice(0))release();await expect(dialog.locator('[role=alert]')).toBeFocused();
});
test('row own pressed state is independent of a mixed coordinator checkbox',async({page})=>{
 await fixture(page,{count:5});await page.locator('.task-row[data-task-id="0"]').click();await page.getByRole('button',{name:'Select tasks',exact:true}).click();const row=page.locator('.task-row[data-task-id="0"]');await row.click();
 await expect(row).toHaveAttribute('aria-pressed','true');await expect(row).not.toHaveAttribute('aria-current');await expect(page.getByRole('checkbox',{name:'Select Task 0',exact:true})).toHaveAttribute('aria-checked','mixed');await row.press('Space');await expect(row).toHaveAttribute('aria-pressed','false');
});
for(const cancel of ['Cancel','Escape'])test(`settings delete ${cancel} restores its surviving origin without mutation`,async({page})=>{
 const state=await fixture(page,{count:1});await page.locator('.task-more').click();await page.getByRole('menuitem',{name:'Task settings',exact:true}).click();const origin=page.locator('#task-settings-panel').getByRole('button',{name:'Delete task',exact:true});await origin.focus();await page.keyboard.press('Enter');
 if(cancel==='Escape')await page.keyboard.press('Escape');else await page.locator('#task-action-dialog').getByRole('button',{name:'Cancel',exact:true}).click();
 await expect(origin).toBeFocused();expect(state.calls).toEqual([]);
});
