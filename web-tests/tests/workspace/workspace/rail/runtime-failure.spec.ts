import {test,expect} from '@playwright/test';
import {actorPage} from '../../_lifecycle-fixture.ts';
const cleanup=[];test.afterEach(async()=>{for(const dispose of cleanup.splice(0))await dispose();});
test('completed live task remains busy and failed task clears on authoritative event',async({browser})=>{
 const page=await actorPage(browser,cleanup,'<div id="rows"></div>');
 await page.evaluate(async()=>{const {compactTaskRow}=await import('/workspace/rail/task-row.js');const {patchEvent}=await import('/workspace/events/workspace-events-policy.js');window.tasks=[{id:'live',name:'Busy review',status:'in_review',live:true},{id:'failed',name:'Standalone',status:'complete',failure:{reason:'validation failed',source:'task_result'}}];window.render=()=>document.querySelector('#rows').replaceChildren(...window.tasks.map(task=>compactTaskRow({task,select:()=>{}})));window.clearFailure=()=>{window.tasks=patchEvent(window.tasks,{type:'task.failure_changed',task_id:'failed',payload:{failure:null}}).tasks;window.render();};window.render();});
 await expect(page.locator('[data-task-id="live"] [data-status="working"]')).toHaveAccessibleName('Running');
 await expect(page.locator('[data-task-id="failed"] [data-status="failed"]')).toHaveAccessibleName('Failed');
 await page.evaluate(()=>window.clearFailure());await expect(page.locator('[data-task-id="failed"] [data-status="failed"]')).toHaveCount(0);
});
test('actual Info clears standalone failure reason while preserving independent Hera failure',async({browser})=>{
 const page=await browser.newPage({viewport:{width:1440,height:900}});cleanup.push(()=>page.close());
 const task={id:'a',name:'Failure example',project:'demo',status:'complete',live:false,failure:{reason:'validation failed',source:'task_result'}};
 await page.addInitScript(()=>{const original=window.fetch.bind(window);window.fetch=(url,options)=>String(url).startsWith('/api/events/stream')?Promise.resolve(new Response(new ReadableStream({start(controller){window.emitTaskEvent=event=>controller.enqueue(new TextEncoder().encode(`data: ${JSON.stringify(event)}\n\n`));}}),{headers:{'Content-Type':'text/event-stream'}})):original(url,options);});
 await page.route('**/api/**',route=>{
  const path=new URL(route.request().url()).pathname;
  if(path==='/api/tasks')return route.fulfill({json:{tasks:[task]}});
  if(path==='/api/tasks/a'||path.endsWith('/raw'))return route.fulfill({json:task});
  if(path==='/api/projects/full')return route.fulfill({json:{projects:[{name:'demo'}]}});
  if(path==='/api/hera')return route.fulfill({json:{orchestrators:[],freelance:[{role_id:1,kind:'freelance',live:true,task_id:'a',status:'failed'}]}});
  if(path.endsWith('/output'))return route.fulfill({body:'',headers:{'X-Output-Total':'0'}});
  return route.fulfill({json:{}});
 });
 await page.goto(process.env.WORKSPACE_TEST_URL);await page.locator('#token').fill('fixture-token');await page.locator('#connect-button').click();await page.locator('#tool-status').click();
 await expect(page.locator('#tool-panel .status-row').filter({has:page.getByText('Failure',{exact:true})}).locator('dd')).toHaveText('validation failed');
 task.failure=null;await page.evaluate(()=>window.emitTaskEvent({id:1,type:'task.failure_changed',task_id:'a',payload:{failure:null}}));
 await expect(page.locator('#tool-panel')).not.toContainText('validation failed');await expect(page.locator('#tool-panel dt').filter({hasText:/^Failure$/})).toHaveCount(0);
 await expect(page.locator('[data-task-id="a"] [data-status="failed"]')).toBeVisible();
});
