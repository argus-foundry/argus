import {test,expect} from '@playwright/test';

async function fixture(page,{comparison={requested_base:'parent',resolved_base:'parent',merge_base:'abc123',fallback_reason:null},status=' M src/file space.txt',branch='M\tsrc/file space.txt'}={}) {
  const task={id:'changes-task',name:'Changes contracts',project:'fixture',status:'complete',worktree_path:'/tmp/fixture worktree'};
  const state={comparison,status,branch,revision:'r1',stale:false,discardStatus:200,discardCode:'',revealStatus:200,revealCode:'',requests:[] as {path:string,body:unknown}[],statusReads:0};
  await page.addInitScript(()=>{
    const original=window.fetch;
    window.fetch=(path,options)=>String(path).startsWith('/api/events/stream')?Promise.resolve(new Response(new ReadableStream({start(controller){window['changesEvents']=controller;}}),{headers:{'Content-Type':'text/event-stream'}})):original(path,options);
    window['invalidateChanges']=()=>window['changesEvents'].enqueue(new TextEncoder().encode('data: '+JSON.stringify({type:'worktree.changed',task_id:'changes-task',payload:{reason:'filesystem'}})+'\n\n'));
  });
  await page.route('**/api/**',async route=>{
    const path=new URL(route.request().url()).pathname;let json:any={};let responseStatus=200;
    if(route.request().method()==='POST'){
      state.requests.push({path,body:route.request().postDataJSON()});
      if(path.endsWith('/git/discard')){responseStatus=state.discardStatus;json=responseStatus===200?{discarded:true,scope:'local',path:'src/file space.txt'}:{code:state.discardCode,error:state.discardCode};}
      if(path.endsWith('/reveal')){responseStatus=state.revealStatus;json=responseStatus===200?{status:'opened',host:'daemon'}:{error:state.revealCode};}
    }else if(path==='/api/tasks')json={tasks:[task]};
    else if(path==='/api/projects/full'||path==='/api/projects')json={projects:[{name:'fixture'}]};
    else if(path.endsWith('/raw'))json=task;
    else if(path.endsWith('/git/status')){state.statusReads++;json={Status:state.status,BranchFiles:state.branch,comparison:state.comparison,git_revision:state.revision,stale:state.stale};}
    else if(path.endsWith('/git/diff'))json={Diff:'diff --git a/f b/f\n@@ -1 +1 @@\n-old\n+new',comparison:state.comparison};
    else if(path.endsWith('/size'))json={cols:80,rows:24};
    if(path.endsWith('/output'))return route.fulfill({body:'',headers:{'X-Output-Total':'0'}});
    await route.fulfill({status:responseStatus,json});
  });
  await page.goto(process.env.WORKSPACE_TEST_URL!);
  await page.locator('#token').fill('fixture');await page.locator('#connect-button').click();
  await expect(page.locator('.file-row')).toHaveCount(1);return state;
}

test('stacked status and file diff share returned comparison and explain fallback base',async({page})=>{
  const state=await fixture(page);
  await expect(page.locator('.changes-comparison')).toContainText('Requested parent');
  await expect(page.locator('.changes-comparison')).toContainText('merge-base abc123 of parent');
  await page.locator('.file-row').click();await expect(page.locator('.review-caption')).toContainText('merge-base abc123 of parent');
  state.comparison={requested_base:'missing',resolved_base:'origin/master',merge_base:'def456',fallback_reason:'requested base missing'};
  await page.evaluate(()=>window['invalidateChanges']());
  await expect(page.locator('.changes-comparison')).toContainText('Effective base: origin/master');
  await page.locator('.file-row').click();await expect(page.locator('.review-caption')).toContainText('Effective base: origin/master');
  await page.getByRole('button',{name:'Split',exact:true}).click();await expect(page.locator('.review-caption')).toContainText('Requested missing');
  await expect(page.locator('.file-row')).toHaveCount(1);
});

for(const untracked of [false,true])test(`confirmed ${untracked?'untracked deletion':'tracked discard'} captures exact path and refreshes through events`,async({page})=>{
  const state=await fixture(page,{status:(untracked?'?? ':' M ')+'src/file space.txt'});
  const action=untracked?'Delete untracked file':'Discard local changes';
  await page.getByRole('button',{name:'File actions for src/file space.txt'}).click();await page.getByRole('menuitem',{name:action,exact:true}).click();
  const dialog=page.locator('#confirm-dialog');await expect(dialog).toContainText('src/file space.txt');await expect(dialog.getByRole('button',{name:'Cancel',exact:true})).toBeFocused();
  await dialog.getByRole('button',{name:'Cancel',exact:true}).click();expect(state.requests).toEqual([]);
  await page.getByRole('button',{name:'File actions for src/file space.txt'}).click();await page.getByRole('menuitem',{name:action,exact:true}).click();
  await dialog.getByRole('button',{name:untracked?'Delete file':'Discard changes',exact:true}).click();await expect(dialog).toHaveCount(0);
  expect(state.requests).toEqual([{path:'/api/tasks/changes-task/git/discard',body:{path:'src/file space.txt',scope:'local',delete_untracked:untracked}}]);
  await expect(page.locator('.file-row')).toHaveCount(1);
  state.status='';state.branch='';state.revision='r2';await page.evaluate(()=>window['invalidateChanges']());await expect(page.getByText('No changes yet',{exact:true})).toBeVisible();
});
for(const [code,responseStatus,message] of [
 ['rename_requires_explicit_paths',409,'both paths'],['untracked_requires_delete',409,'explicit deletion'],['conflict_state',409,'conflict'],['no_changes',409,'no local changes'],['task_archived',409,'archived'],['invalid_path',400,'path is invalid'],['task_not_found',404,'task is unavailable'],['worktree_not_found',404,'worktree is unavailable'],['git_failed',500,'not confirmed'],
] as const)test(`discard retains list and explains ${code}`,async({page})=>{
  const state=await fixture(page);state.discardStatus=responseStatus;state.discardCode=code;
  await page.getByRole('button',{name:'File actions for src/file space.txt'}).click();await page.getByRole('menuitem',{name:'Discard local changes',exact:true}).click();
  await page.locator('#confirm-dialog').getByRole('button',{name:'Discard changes',exact:true}).click();
  await expect(page.locator('#confirm-dialog [role=alert]')).toContainText(message);expect(state.requests.length).toBe(1);await expect(page.locator('.file-row')).toHaveCount(1);
});
test('branch-only files offer no local discard and a worktree event refreshes an open diff',async({page})=>{
  await fixture(page,{status:''});await page.locator('.file-row').click();await expect(page.locator('.diff-table')).toBeVisible();
  await page.getByRole('button',{name:'File actions for src/file space.txt'}).click();await expect(page.getByRole('menuitem',{name:'Discard local changes',exact:true})).toHaveCount(0);
  let diffReads=0;await page.route('**/git/diff?*',route=>{diffReads++;return route.fulfill({json:{Diff:'@@ -1 +1 @@\n-old\n+fresh event contents'}});});
  await page.evaluate(()=>window['invalidateChanges']());await expect(page.locator('.diff-table')).toContainText('fresh event contents');expect(diffReads).toBeGreaterThan(0);
});

for(const target of ['worktree','file'])test(`Finder ${target} action explains daemon host and sends exact JSON`,async({page})=>{
  const state=await fixture(page);
  if(target==='worktree')await page.getByRole('button',{name:'Task actions for Changes contracts',exact:true}).click();else await page.getByRole('button',{name:'File actions for src/file space.txt'}).click();
  await page.getByRole('menuitem',{name:target==='worktree'?'Open in Finder':'Reveal in Finder',exact:true}).click();
  const dialog=page.locator('#confirm-dialog');await expect(dialog).toContainText('different computer');expect(state.requests).toEqual([]);
  await dialog.getByRole('button',{name:'Open Finder',exact:true}).click();await expect(dialog).toHaveCount(0);
  expect(state.requests).toEqual([{path:'/api/tasks/changes-task/reveal',body:target==='worktree'?{}:{path:'src/file space.txt'}}]);await expect(page.locator('#notice')).toHaveText('Finder opened on the daemon host.');
});
for(const [responseStatus,code,message] of [
 [403,'forbidden','master token'],[501,'unsupported_host','does not support macOS'],[404,'task_not_found','task or its worktree is unavailable'],[404,'path_not_found','no longer exists'],[400,'invalid_path','invalid'],[500,'reveal_failed','could not open Finder'],
] as const)test(`Finder explains ${responseStatus} ${code} and preserves file state`,async({page})=>{
  const state=await fixture(page);state.revealStatus=responseStatus;state.revealCode=code;
  await page.getByRole('button',{name:'File actions for src/file space.txt'}).click();await page.getByRole('menuitem',{name:'Reveal in Finder',exact:true}).click();
  const dialog=page.locator('#confirm-dialog');await dialog.getByRole('button',{name:'Open Finder',exact:true}).click();await expect(dialog.getByRole('alert')).toContainText(message);expect(state.requests.length).toBe(1);
  await dialog.getByRole('button',{name:'Cancel',exact:true}).click();await expect(page.locator('.file-row')).toHaveCount(1);
  if(responseStatus===403){
    await page.getByRole('button',{name:'File actions for src/file space.txt'}).click();await expect(page.getByRole('menuitem',{name:'Reveal in Finder',exact:true})).toBeDisabled();await page.keyboard.press('Escape');
    await page.getByRole('button',{name:'Task actions for Changes contracts',exact:true}).click();await expect(page.getByRole('menuitem',{name:'Open in Finder',exact:true})).toBeDisabled();
  }
});
test('Finder cancellation sends no launch request',async({page})=>{
 const state=await fixture(page);await page.getByRole('button',{name:'File actions for src/file space.txt'}).click();await page.getByRole('menuitem',{name:'Reveal in Finder',exact:true}).click();await page.keyboard.press('Escape');await expect(page.locator('#confirm-dialog')).toHaveCount(0);expect(state.requests).toEqual([]);
});

test('expanded untracked directory refreshes its children after a discard event',async({page})=>{
  const state=await fixture(page,{status:'?? draft/',branch:''});let files=[{Path:'draft/file space.txt',Status:'A',IsDir:false}];
  await page.route('**/files?*',route=>route.fulfill({json:{Files:files}}));
  await page.locator('.file-row').click();await expect(page.locator('.directory-files .file-row')).toHaveCount(1);
  await page.getByRole('button',{name:'File actions for draft/file space.txt',exact:true}).click();await page.getByRole('menuitem',{name:'Delete untracked file',exact:true}).click();
  await page.locator('#confirm-dialog').getByRole('button',{name:'Delete file',exact:true}).click();expect(state.requests[0].body).toEqual({path:'draft/file space.txt',scope:'local',delete_untracked:true});
  files=[{Path:'draft/new.txt',Status:'A',IsDir:false}];state.revision='r2';await page.evaluate(()=>window['invalidateChanges']());
  await expect(page.locator('.directory-files')).toContainText('new.txt');await expect(page.locator('.directory-files')).not.toContainText('file space.txt');
});
test('reconnect resnapshots Changes after a stream gap',async({page})=>{
 const state=await fixture(page);state.branch='A\treconnected.txt';state.status='';state.revision='r2';
 await page.evaluate(()=>window['changesEvents'].close());await page.getByRole('button',{name:'Retry connection',exact:true}).click();
 await expect(page.locator('.file-row')).toHaveCount(1);await expect(page.locator('.file-row')).toContainText('reconnected.txt');
});
test('stale snapshots retain files and converge through bounded retry',async({page})=>{
 const state=await fixture(page);state.stale=true;await page.evaluate(()=>window['invalidateChanges']());await expect(page.getByText('Git snapshot is stale. Checking for current changes…',{exact:true})).toBeVisible();
 state.stale=false;state.revision='r2';state.branch='A\tfresh.txt';state.status='';await expect(page.locator('.file-row')).toContainText('fresh.txt');await expect(page.getByText('Git snapshot is stale. Checking for current changes…',{exact:true})).toHaveCount(0);
});
