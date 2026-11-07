export async function reviewFixture(page,{count=140,hold=false,sse=false,fail=false,nested=false}={}){
 let tasks=Array.from({length:count},(_,i)=>({id:String(i),name:nested&&i===0?'Match root':nested&&i===2?'Match leaf':`Task ${i}`,project:'review',branch:`branch-${i}`,worktree_path:`/tmp/${i}`,archived:true,status:'complete'}));
 const calls=[],reads=[],pending=[],writes=[];
 await page.addInitScript(()=>{const fetch=window.fetch.bind(window);window.fetch=(url,options)=>String(url).startsWith('/api/events/stream')?Promise.resolve(new Response(new ReadableStream({start(controller){window.emitReviewEvent=event=>controller.enqueue(new TextEncoder().encode(`data: ${JSON.stringify(event)}\n\n`));}}),{headers:{'Content-Type':'text/event-stream'}})):fetch(url,options);});
 await page.route('**/api/**',async route=>{
  const path=new URL(route.request().url()).pathname,method=route.request().method(),id=path.split('/')[3],task=tasks.find(task=>task.id===id);
  if(method==='GET')reads.push(path);
  if(method==='POST'&&/\/(unarchive|archive)$/.test(path)){writes.push([method,path]);tasks=tasks.map(task=>task.id===id?{...task,archived:!path.endsWith('/unarchive')}:task);return route.fulfill({json:{}});}
  if(method==='DELETE'){
   calls.push(id);if(hold)await new Promise(resolve=>pending.push(resolve));
   if(fail)return route.fulfill({status:500,json:{}});
   tasks=tasks.filter(task=>task.id!==id);if(sse)await page.evaluate(id=>window.emitReviewEvent({type:'task.deleted',task_id:id}),id);
   return route.fulfill({json:{}});
  }
  const json=path==='/api/tasks'?{tasks}:path==='/api/projects/full'?{projects:[]}:path==='/api/hera'?{orchestrators:[{id:1,name:'Review team',roles:tasks.filter(task=>!nested||task.id!=='2').map((task,index)=>({task_id:task.id,role_id:Number(task.id)+1,name:'Agent '+index,kind:index?'worker':'coordinator',live:true}))},...(nested?[{id:2,name:'Nested team',bridge_parent_orch_id:1,bridge_parent_role_id:2,roles:[{task_id:'1',role_id:100,kind:'coordinator',live:true},{task_id:'2',role_id:101,kind:'worker',live:true}]}]:[])]}:path==='/api/backends'?{backends:[]}:path.endsWith('/raw')||/^\/api\/tasks\/\d+$/.test(path)?task||{}:path.endsWith('/links')?{links:[]}:{};
  return route.fulfill({json});
 });
 await page.goto('http://127.0.0.1:17750');await page.locator('#token').fill('fixture');await page.locator('#connect-button').click();await page.locator('#archive-filter').click();await page.locator('.task-row').first().waitFor();
 return {calls,reads,pending,writes,remove(id){tasks=tasks.filter(task=>task.id!==id);},restore(id){tasks=tasks.map(task=>task.id===id?{...task,archived:false}:task);}};
}
export async function bulkReview(page){await page.getByRole('button',{name:'Select tasks',exact:true}).click();await page.getByRole('checkbox',{name:'Select all matching tasks',exact:true}).check();await page.getByRole('button',{name:'Delete selected tasks',exact:true}).click();}
