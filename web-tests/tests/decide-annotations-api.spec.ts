import {test,expect} from '@playwright/test';
const headers={Authorization:'Bearer test-token'};

test('real mixed submit accepts unchanged FYIs across two coordinators and retries changed FYIs',async({page,request})=>{
 const control='http://127.0.0.1:'+(Number(process.env.ARGUS_DECIDE_TEST_PORT||17749)+10);
 expect((await request.post(control+'/test/reset')).ok()).toBe(true);
 await page.addInitScript(()=>{window.close=()=>{};});
 const nav=await (await request.get('/api/hera/board/decision-navigation',{headers})).json();
 expect(nav.roles).toHaveLength(2);
 const boards=await Promise.all(nav.roles.map(async(row:any)=>({role:row.role_id,...await (await request.get('/api/hera/roles/'+row.role_id+'/board/decide-items',{headers})).json()})));
 const link=await (await request.post('/api/hera/board/decision-link',{headers,data:{}})).json();
 await page.goto('/decide#t='+link.ticket);
 const posts:any[]=[];page.on('request',r=>{if(r.method()==='POST'&&r.url().endsWith('/fyi-actions'))posts.push(r.postDataJSON().actions);});
 for(let n=0;n<boards.length;n++){
  if(n){await page.locator('#main').focus();await page.keyboard.press('Control+ArrowDown');}
  await page.getByRole('tab',{name:/^FYIs/}).click();
  for(const item of boards[n].items.filter((i:any)=>i.kind==='fyi')){
   await page.locator('#note-'+item.id).fill('Feedback '+item.id);
   await page.locator('#decision-'+item.id).getByRole('button',{name:'Comment',exact:true}).click();
  }
 }
 const changed=boards.map(b=>b.items.find((i:any)=>i.subject==='Changed FYI'));
 for(let n=0;n<boards.length;n++){
  const item=changed[n],response=await request.post(control+'/test/board-revise',{data:{role_id:boards[n].role,item_id:item.id,kind:item.kind,subject:item.subject,body:'New context during review'}});
  expect(response.ok()).toBe(true);expect((await response.json()).updated_at).not.toBe(item.updated_at);
 }
 await page.locator('#submit').click();await expect(page.locator('.submit-note')).toContainText('2 sent · 2 needs another look');
 await expect(page.locator('#main')).not.toContainText('Required note missing or too long');
 await expect(page.locator('#decision-'+changed[1].id)).toContainText('Item changed.');
 for(let n=0;n<boards.length;n++){
  if(!n){await page.locator('#main').focus();await page.keyboard.press('Control+ArrowUp');}
  else {await page.locator('#main').focus();await page.keyboard.press('Control+ArrowDown');}
  const item=changed[n];await expect(page.locator('#note-'+item.id)).toHaveValue('Feedback '+item.id);
  await page.locator('#decision-'+item.id).getByRole('button',{name:'Comment',exact:true}).click();
 }
 await page.locator('#submit-close').click();await expect(page.locator('h1')).toHaveText('Actions sent — you can close this tab');
 expect(posts).toHaveLength(4);
 expect(posts.slice(2).flat().map(a=>a.item_id)).toEqual(changed.map(i=>i.id));
 for(let n=0;n<boards.length;n++){
  const original=posts[n].find((a:any)=>a.item_id===changed[n].id),retry=posts[n+2][0];
  expect(retry.action_id).not.toBe(original.action_id);expect(retry.expected_updated_at).not.toBe(original.expected_updated_at);
 }
 await expect(page.locator('main')).toContainText('4 comments');
 for(let n=0;n<boards.length;n++){
  const base='/api/hera/roles/'+boards[n].role+'/board/fyi-actions/';
  for(const action of [posts[n].find((a:any)=>a.item_id!==changed[n].id),posts[n+2][0]]){
   const response=await request.get(base+action.action_id,{headers});expect(response.ok()).toBe(true);
   const receipt=await response.json();expect(receipt.outcome).toBe('commented');expect(receipt.note).toBe('Feedback '+action.item_id);
  }
 }
});

test('real isolated API validates browser anchors, retains full receipts and leaves feedback-only decisions open',async({page,request})=>{
 const reset=await request.post('http://127.0.0.1:'+(Number(process.env.ARGUS_DECIDE_TEST_PORT||17749)+10)+'/test/reset');expect(reset.ok()).toBe(true);
 await page.addInitScript(()=>{window.close=()=>{};});
 const nav=await (await request.get('/api/hera/board/decision-navigation',{headers})).json();const role=nav.roles[0].role_id,base='/api/hera/roles/'+role+'/board';
 const board=await (await request.get(base+'/decide-items',{headers})).json(),item=board.items.find((i:any)=>i.subject==='Annotation fixture');expect(item).toBeTruthy();
 const link=await (await request.post(base+'/decision-link',{headers,data:{focus_item_id:item.id}})).json();await page.goto('/decide#t='+link.ticket);const card=page.locator('#decision-'+item.id);await expect(card).toBeVisible();
 await card.locator('[data-field="body"]').focus();await page.keyboard.press('Enter');await page.locator('#selection-toolbar').getByRole('button',{name:'Delete',exact:true}).click();const firstResponse=page.waitForResponse(r=>r.url().endsWith('/answers')&&r.request().method()==='POST');await page.locator('#submit').click();const response=await firstResponse;expect(response.status()).toBe(200);const result=(await response.json()).results[0];expect(result.outcome).toBe('commented');await expect(card).toBeVisible();await expect(page.locator('.submit-note')).toContainText('Answers sent to Argus');await expect(page.locator('.receipt')).toHaveCount(0);
 const receipt=await (await request.get(base+'/fyi-actions/'+result.action_id,{headers})).json();expect(receipt.annotations[0].quote).toBe('Hello bold 😀\nFirst bullet\nNested detail');expect(receipt.annotations[0].type).toBe('DELETION');expect(receipt.annotations[0].start_offset).toBe(0);expect(receipt.item.resolved).toBe(false);
 const replay=await request.post(base+'/answers',{headers,data:response.request().postDataJSON()});expect((await replay.json()).results[0].outcome).toBe('commented');
 await card.locator('input[value="a"]').check();await page.locator('#submit-close').click();await expect(page.locator('h1')).toHaveText('Actions sent — you can close this tab');const remaining=await (await request.get(base+'/decide-items',{headers})).json();expect(remaining.items.some((i:any)=>i.id===item.id)).toBe(false);
});
