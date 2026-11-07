import {test,expect} from '@playwright/test';
test('proposal neighbor retains Approve with uneven path counts',async({page})=>{
 await page.goto('/');
 await page.evaluate(async()=>{
  document.body.replaceChildren();
  const host=document.createElement('section');host.className='settings-section';document.body.append(host);
  const {SandboxProposals}=await import('/settings/sandbox/settings-proposals.js');
  let mutated=false;
  const rows=[{id:'a',task_id:'a',entries:[{path:'/tmp/a'},{path:'/tmp/b'},{path:'/tmp/c'}]},{id:'b',task_id:'b',entries:[{path:'/tmp/d'}]}];
  window.view=new SandboxProposals({host,api:{json:async(path,config={})=>{if(config.method){mutated=true;return {};}return {proposals:mutated?rows.slice(1):rows};}},busy(){},status(){}});
 });
 const approve=page.getByRole('button',{name:'Approve paths',exact:true}).first();await approve.focus();await page.keyboard.press('Enter');
 const confirm=page.getByRole('button',{name:'Confirm',exact:true});await confirm.focus();await page.keyboard.press('Enter');
 await expect(page.locator('dialog')).toHaveCount(0);await expect(page.locator('.settings-proposal')).toHaveCount(1);
 await expect(page.getByRole('button',{name:'Reject proposal',exact:true})).not.toBeFocused();
 await expect(page.getByRole('button',{name:'Approve paths',exact:true})).toBeFocused({timeout:1000});
});
test('departed focus does not return after chosen outside control disappears',async({page})=>{
 await page.goto('/');
 const result=await page.evaluate(async()=>{
  document.body.replaceChildren();
  const root=document.createElement('section'),origin=document.createElement('button'),outside=document.createElement('button');
  origin.textContent='Save project';origin.dataset.focusKey='save';outside.textContent='Temporary other control';root.append(origin);document.body.append(root,outside);
  const {captureFocus}=await import('/shared/focus-recovery.js');origin.focus();const restore=captureFocus(root);
  origin.disabled=true;outside.focus();outside.remove();origin.remove();
  const replacement=document.createElement('button');replacement.textContent='Save project';replacement.dataset.focusKey='save';root.append(replacement);restore();
  return {bodyOwnsFocus:document.activeElement===document.body,active:document.activeElement.textContent};
 });
 expect(result.bodyOwnsFocus,JSON.stringify(result)).toBe(true);
});
test('connected nonselector origin retains focus priority',async({page})=>{
 await page.goto('/');
 const result=await page.evaluate(async()=>{
  document.body.replaceChildren();const root=document.createElement('section'),origin=document.createElement('p'),back=document.createElement('button');
  origin.tabIndex=-1;origin.textContent='Save failed';back.textContent='Back';root.append(origin,back);document.body.append(root);
  const {captureFocus}=await import('/shared/focus-recovery.js');origin.focus();const restore=captureFocus(root,()=>back,{neighbors:false});
  origin.blur();restore();return document.activeElement===origin;
 });
 expect(result).toBe(true);
});
test('focus recovery skips controls inside closed disclosures',async({page})=>{
 await page.goto('/');
 const result=await page.evaluate(async()=>{
  document.body.replaceChildren();const root=document.createElement('section');
  const origin=document.createElement('button');origin.textContent='Archive';root.append(origin);
  const details=document.createElement('details');const summary=document.createElement('summary');summary.textContent='Collapsed';
  const hidden=document.createElement('button');hidden.textContent='Archive';details.append(summary,hidden);root.append(details);
  const next=document.createElement('button');next.textContent='Archive';root.append(next);document.body.append(root);
  const {captureFocus}=await import('/shared/focus-recovery.js');origin.focus();const restore=captureFocus(root,undefined,{selector:'button'});origin.remove();restore();
  return {nextOwnsFocus:document.activeElement===next,hiddenRects:hidden.getClientRects().length,active:document.activeElement.outerHTML};
 });
 expect(result.nextOwnsFocus,JSON.stringify(result)).toBe(true);
});
for(const collapsed of [false,true])test(`real rail archive focus next project collapsed=${collapsed}`,async({page})=>{
 let tasks=['a','b','c'].map((id,index)=>({id,name:`Task ${id}`,project:['alpha','beta','gamma'][index],archived:false,status:'complete'}));
 await page.route('**/api/**',async route=>{
  const path=new URL(route.request().url()).pathname;
  if(path==='/api/tasks/a/archive'&&route.request().method()==='POST'){tasks=tasks.map(task=>task.id==='a'?{...task,archived:true}:task);return route.fulfill({json:{}});}
  const json=path==='/api/tasks'?{tasks}:path==='/api/projects/full'?{projects:[]}:path==='/api/hera'?{}:{};
  return route.fulfill({json});
 });
 await page.goto('/');await page.locator('#token').fill('fixture');await page.locator('#connect-button').click();
 await page.locator('.task-row[data-task-id="c"]').waitFor();
 if(collapsed){await page.locator('.project-group[data-project="beta"] .project-header').click();await expect(page.locator('.project-group[data-project="beta"]')).not.toHaveAttribute('open');}
 const origin=page.locator('.task-archive[data-task-id="a"]');await origin.focus();await page.keyboard.press('Enter');
 const confirm=page.locator('#archive-dialog').getByRole('button',{name:'Archive task',exact:true});await confirm.focus();await page.keyboard.press('Enter');
 await expect(page.locator('#archive-dialog')).toHaveCount(0);await expect(page.locator('.task-row[data-task-id="a"]')).toHaveCount(0);
 const state=await page.evaluate(()=>({activeTag:document.activeElement.tagName,activeTask:document.activeElement.dataset.taskId||null,activeAction:document.activeElement.className,hidden:[...document.querySelectorAll('.project-group[data-project="beta"] .task-archive')].map(item=>item.getClientRects().length)}));
 expect(state.activeTag,JSON.stringify(state)).not.toBe('BODY');
 await expect(page.locator(`.task-archive[data-task-id="${collapsed?'c':'b'}"]`)).toBeFocused({timeout:1000});
});

test('focus recovery continues when a rendered candidate refuses native focus',async({page})=>{
 await page.goto('/');
 const focused=await page.evaluate(async()=>{
  document.body.replaceChildren();const root=document.createElement('section'),origin=document.createElement('button');
  origin.dataset.focusKey='archive';origin.textContent='Archive';root.append(origin);document.body.append(root);
  const {captureFocus}=await import('/shared/focus-recovery.js');origin.focus();const restore=captureFocus(root);
  const blocked=origin.cloneNode(true);blocked.style.visibility='hidden';const next=document.createElement('button');next.textContent='Archive';root.replaceChildren(blocked,next);
  restore();return document.activeElement===next;
 });
 expect(focused).toBe(true);
});
