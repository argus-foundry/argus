import {test,expect} from '@playwright/test';
const headers={Authorization:'Bearer test-token'};
test('real isolated API validates browser anchors, retains full receipts and leaves feedback-only decisions open',async({page,request})=>{
 const reset=await request.post('http://127.0.0.1:'+(Number(process.env.ARGUS_DECIDE_TEST_PORT||17749)+10)+'/test/reset');expect(reset.ok()).toBe(true);
 await page.addInitScript(()=>{window.close=()=>{};});
 const nav=await (await request.get('/api/hera/board/decision-navigation',{headers})).json();const role=nav.roles[0].role_id,base='/api/hera/roles/'+role+'/board';
 const board=await (await request.get(base+'/decide-items',{headers})).json(),item=board.items.find((i:any)=>i.subject==='Annotation fixture');expect(item).toBeTruthy();
 const link=await (await request.post(base+'/decision-link',{headers,data:{focus_item_id:item.id}})).json();await page.goto('/decide#t='+link.ticket);const card=page.locator('#decision-'+item.id);await expect(card).toBeVisible();
 await card.locator('[data-field="body"]').focus();await page.keyboard.press('Enter');await page.locator('#selection-toolbar').getByRole('button',{name:'Delete',exact:true}).click();const firstResponse=page.waitForResponse(r=>r.url().endsWith('/answers')&&r.request().method()==='POST');await page.locator('#submit').click();const response=await firstResponse;expect(response.status()).toBe(200);const result=(await response.json()).results[0];expect(result.outcome).toBe('commented');await expect(card).toBeVisible();await expect(page.locator('#status')).toContainText('Actions sent');await expect(page.locator('.receipt')).toHaveCount(0);
 const receipt=await (await request.get(base+'/fyi-actions/'+result.action_id,{headers})).json();expect(receipt.annotations[0].quote).toBe('Hello bold 😀\nFirst bullet\nNested detail');expect(receipt.annotations[0].type).toBe('DELETION');expect(receipt.annotations[0].start_offset).toBe(0);expect(receipt.item.resolved).toBe(false);
 const replay=await request.post(base+'/answers',{headers,data:response.request().postDataJSON()});expect((await replay.json()).results[0].outcome).toBe('commented');
 await card.locator('input[value="a"]').check();await page.locator('#submit-close').click();await expect(page.locator('h1')).toHaveText('Actions sent — you can close this tab');const remaining=await (await request.get(base+'/decide-items',{headers})).json();expect(remaining.items.some((i:any)=>i.id===item.id)).toBe(false);
});
