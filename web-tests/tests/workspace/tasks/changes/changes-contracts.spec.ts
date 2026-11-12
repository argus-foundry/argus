import {test,expect} from '@playwright/test';

async function fixture(page,{comparison={requested_base:'parent',resolved_base:'parent',merge_base:'abc123',fallback_reason:null},status=' M src/file space.txt',branch='M\tsrc/file space.txt'}={}) {
  const task={id:'changes-task',name:'Changes contracts',project:'fixture',status:'complete',worktree_path:'/tmp/fixture worktree'};
  const state={comparison,status,branch,revision:'r1',discardStatus:200,discardCode:'',revealStatus:200,revealCode:'',requests:[] as {path:string,body:unknown}[],statusReads:0};
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
    else if(path.endsWith('/git/status')){state.statusReads++;json={Status:state.status,BranchFiles:state.branch,comparison:state.comparison,git_revision:state.revision,stale:false};}
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
