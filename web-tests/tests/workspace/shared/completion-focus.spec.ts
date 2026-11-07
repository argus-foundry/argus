import {test,expect} from '@playwright/test';
for(const action of ['archive','delete'])for(const collapsed of [false,true])test(`bulk ${action} recovery next project collapsed=${collapsed}`,async({page})=>{
 let tasks=['a','b','c'].map((id,index)=>({id,name:`Task ${id}`,project:['alpha','beta','gamma'][index],archived:action==='delete',status:'complete'}));
 await page.route('**/api/**',async route=>{
  const path=new URL(route.request().url()).pathname;
  if(action==='archive'&&path==='/api/tasks/a/archive'&&route.request().method()==='POST'){tasks=tasks.map(task=>task.id==='a'?{...task,archived:true}:task);return route.fulfill({json:{}});}
  if(action==='delete'&&path==='/api/tasks/a'&&route.request().method()==='DELETE'){tasks=tasks.filter(task=>task.id!=='a');return route.fulfill({json:{}});}
  const json=path==='/api/tasks'?{tasks}:path==='/api/projects/full'?{projects:[]}:path==='/api/hera'?{}:{};
  return route.fulfill({json});
 });
 await page.goto('/');await page.locator('#token').fill('fixture');await page.locator('#connect-button').click();if(action==='delete')await page.locator('#archive-filter').click();await page.locator('.task-row[data-task-id="c"]').waitFor();
 if(collapsed)await page.locator('.project-group[data-project="beta"] .project-header').click();
 await page.getByRole('button',{name:'Select tasks',exact:true}).click();await page.getByRole('checkbox',{name:'Select Task a',exact:true}).check();
 await page.getByRole('button',{name:action==='delete'?'Delete selected tasks':'Archive selected tasks',exact:true}).click();
 await page.locator('#bulk-dialog').getByRole('button',{name:action==='delete'?'Delete 1 tasks':'Archive 1 tasks',exact:true}).click();await expect(page.locator('#bulk-dialog')).toHaveCount(0);await expect(page.locator('.task-row[data-task-id="a"]')).toHaveCount(0);
 const state=await page.evaluate(()=>({active:document.activeElement.tagName,activeId:document.activeElement.id,activeTask:document.activeElement.dataset.taskId,hiddenRects:document.querySelector('.task-row[data-task-id="b"]')?.getClientRects().length}));
 expect(state.active,JSON.stringify(state)).not.toBe('BODY');
 await expect(page.locator(`.task-row[data-task-id="${collapsed?'c':'b'}"]`)).toBeFocused({timeout:1000});
});
test('settled Tools load cannot restore again after later outside departure',async({page})=>{
 await page.goto('/');
 const state=await page.evaluate(async()=>{
  document.body.replaceChildren();const panel=document.createElement('section');panel.id='tool-panel';const tab=document.createElement('button');tab.id='tool-artifacts';tab.textContent='Artifacts';
  const origin=document.createElement('button');origin.textContent='Report';panel.append(origin);document.body.append(tab,panel);origin.focus();
  const {TaskTools}=await import('/tasks/tools.js');
  const view=Object.assign(Object.create(TaskTools.prototype),{task:{id:'a'},tool:'artifacts',generation:0,abort:new AbortController(),live:{stop(){},select(){}},visible:()=>true,invalidateIdentity(){},review:{}});
  await view.load();view.apply({artifacts:[{filename:'report.txt',name:'Report',type:'text',size:1}]});
  const first=document.activeElement.textContent;if(!document.activeElement.matches('.artifact-row'))throw new Error('Initial Tools recovery did not complete');
  const outside=document.createElement('button');outside.textContent='Temporary navigation action';document.body.append(outside);outside.focus();outside.remove();
  view.apply({artifacts:[]});
  return {first,bodyOwnsFocus:document.activeElement===document.body,active:document.activeElement.tagName,activeId:document.activeElement.id,activeTask:document.activeElement.dataset.taskId};
 });
 expect(state.bodyOwnsFocus,JSON.stringify(state)).toBe(true);
});
test('real Changes shortcut load cannot reclaim focus on later background refresh',async({page})=>{
 let file='first.js',reads=0;
 await page.route('**/api/**',async route=>{
  const path=new URL(route.request().url()).pathname;
  let json={};
  if(path==='/api/tasks')json={tasks:[{id:'a',name:'Task a',project:'demo',status:'complete'}]};
  if(path==='/api/projects/full')json={projects:[]};
  if(path==='/api/tasks/a/git/status'){reads++;json={BranchFiles:`M\t${file}`};}
  return route.fulfill({json});
 });
 await page.goto('/');await page.locator('#token').fill('fixture');await page.locator('#connect-button').click();
 const initial=page.locator('.file-row[data-file-path="first.js"]');await initial.waitFor();await initial.focus();
 const before=reads;await page.keyboard.press('Alt+3');await expect.poll(()=>reads).toBeGreaterThan(before);await expect(initial).toBeFocused();
 await page.locator('#search').focus();await page.locator('#search').evaluate(element=>element.blur());await expect(page.locator('body')).toBeFocused();
 file='later.js';await page.evaluate(()=>window.dispatchEvent(new Event('focus')));
 await page.locator('.file-row[data-file-path="later.js"]').waitFor();
 const active=await page.evaluate(()=>({tag:document.activeElement.tagName,className:document.activeElement.className,id:document.activeElement.id}));expect(active.tag,JSON.stringify(active)).toBe('BODY');
 await expect(page.locator('body')).toBeFocused({timeout:1000});
});

for(const hidden of [false,true])test(`last-task removal recovers the hidden staged-copy control navigation=${hidden}`,async({page})=>{
 let tasks=[{id:'a',name:'Daily',project:'demo',status:'complete'}];
 await page.route('**/api/**',async route=>{const path=new URL(route.request().url()).pathname;await route.fulfill({json:path==='/api/tasks'?{tasks}:path.endsWith('/clipboard')?{text:'Staged text'}:path==='/api/projects/full'?{projects:[]}:{}});});
 await page.goto('/');await page.locator('#token').fill('fixture');await page.locator('#connect-button').click();const copy=page.getByRole('button',{name:'Copy staged text',exact:true});await copy.waitFor();if(hidden)await page.locator('#toggle-nav').click();await copy.focus();tasks=[];
 await page.evaluate(()=>document.dispatchEvent(new Event('visibilitychange')));await expect(copy).toBeHidden();await expect(page.locator(hidden?'#toggle-nav':'#active-filter')).toBeFocused();
});

test('last-task removal recovers a focused header PR link',async({page})=>{
 let tasks=[{id:'a',name:'Daily',project:'demo',branch:'feature',status:'complete'}];
 await page.route('**/api/**',async route=>{const path=new URL(route.request().url()).pathname;await route.fulfill({json:path==='/api/tasks'?{tasks}:path.endsWith('/meta')?{entries:[{namespace:'pr',key:'url',value:'https://github.com/example/repo/pull/1'}]}:path==='/api/projects/full'?{projects:[]}:{}});});
 await page.goto('/');await page.locator('#token').fill('fixture');await page.locator('#connect-button').click();const pr=page.locator('.task-header-links a');await pr.waitFor();await pr.focus();tasks=[];await page.evaluate(()=>document.dispatchEvent(new Event('visibilitychange')));await expect(pr).toHaveCount(0);await expect(page.locator('#tab-agent')).toBeFocused();
});
