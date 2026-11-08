import {test,expect} from '@playwright/test';
import {actorPage} from '../_lifecycle-fixture.ts';

test('Info consumes roster estimates, refreshes without reads, and clears for plain tasks',async({browser})=>{
  const cleanup=[];
  try {
    const page=await actorPage(browser,cleanup,'<div id="workspace"><div id="tools"><div class="tool-tabs"><button role="tab" id="tool-status" data-tool="status">Info</button></div><div id="tool-panel"></div></div></div>');
    const reads=await page.evaluate(async()=>{
      const {TaskTools}=await import('/tasks/tools.js');
      const reads=[];
      const tools=new TaskTools({json:async path=>{reads.push(path);return path.endsWith('/raw')?{model:'demo',account:'work'}:{links:[]};}},{});
      window.costTools=tools;
      tools.updateRoster({orchestrators:[{subtree_cost_usd:12,roles:[{task_id:'a',live:true,kind:'coordinator',cost_usd:2.5,tokens_input:1234,tokens_cache_read:345,tokens_cache_write_1h:10,tokens_cache_write_5m:20,tokens_output:50}]}]});
      tools.selectTask({id:'a'});await tools.drawStatus();
      return reads;
    });
    await expect(page.locator('.status-list')).toContainText('Accountwork');
    await expect(page.locator('.status-list')).toContainText('Estimated cost$2.50');
    await expect(page.locator('.status-list')).toContainText('Team total (this team)$12.00 (estimate, excludes nested teams)');
    await expect(page.locator('.status-list')).toContainText('Input1,234');
    await expect(page.locator('.status-list')).toContainText('Cached input345');
    await expect(page.locator('.status-list')).toContainText('Cache write (1h)10');
    await expect(page.locator('.status-list')).toContainText('Output50');
    await expect(page.locator('.status-list')).toContainText('Cache write (5m)20');
    expect(reads).toEqual(['/api/tasks/a/raw','/api/tasks/a/links']);
    await page.evaluate(async()=>{
      window.costTools.updateRoster({freelance:[{task_id:'a',live:true,kind:'freelance',cost_usd:4}]});await window.costTools.drawStatus();
    });
    await expect(page.locator('.status-list')).toContainText('Estimated cost$4.00');
    await expect(page.locator('.status-list')).not.toContainText('Team total');
    await expect(page.locator('.status-list')).not.toContainText('Cached input');
    await page.evaluate(async()=>{window.costTools.selectTask({id:'plain'});await window.costTools.drawStatus();});
    await expect(page.locator('.status-list')).not.toContainText('Estimated cost');
    await page.evaluate(()=>window.costTools.clear());
  } finally {for(const dispose of cleanup)await dispose();}
});

test('workspace routes its existing roster snapshot and refresh into Info only',async({page})=>{
  let cost=1.25, rosterReads=0;
  const task={id:'cost-task',name:'Cost task',project:'fixture',status:'complete',backend:'claude',archived:false};
  await page.route('**/api/**',route=>{
    const path=new URL(route.request().url()).pathname;
    if(path==='/api/events')return route.fulfill({contentType:'text/event-stream',body:': connected\n\n'});
    if(path.endsWith('/output'))return route.fulfill({body:'',headers:{'X-Output-Total':'0'}});
    let payload={};
    if(path==='/api/tasks')payload={tasks:[task]};
    else if(path==='/api/projects')payload={projects:[{name:'fixture'}]};
    else if(path==='/api/hera'){
      rosterReads++;
      payload={orchestrators:[{id:1,name:'fixture-team',roles:[{role_id:1,task_id:task.id,name:'builder',kind:'worker',live:true,status:'done',cost_usd:cost}]}],freelance:[]};
    } else if(path.endsWith('/raw'))payload=task;
    else if(path.endsWith('/links'))payload={links:[]};
    else if(path.endsWith('/size'))payload={cols:80,rows:24};
    else if(path==='/api/accounts')payload=[];
    else if(path==='/api/backends')payload={backends:[]};
    return route.fulfill({json:payload});
  });
  await page.goto(process.env.WORKSPACE_TEST_URL);
  await page.locator('#token').fill('fixture');
  await page.locator('#connect-button').click();
  await page.locator('#tool-status').click();
  await expect(page.locator('.status-list')).toContainText('Estimated cost$1.25');
  const initialReads=rosterReads;
  await page.locator('#tool-changes').click();
  await page.locator('#tool-status').click();
  await expect(page.locator('.status-list')).toContainText('Estimated cost$1.25');
  expect(rosterReads).toBe(initialReads);
  cost=3.75;
  await expect(page.locator('.status-list')).toContainText('Estimated cost$3.75',{timeout:12000});
  await expect(page.locator('#navigation')).not.toContainText('$3.75');
});
