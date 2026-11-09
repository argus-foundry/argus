import {test,expect} from '../../../web-tests/node_modules/@playwright/test/index.mjs';
test('targeted pin preserves unrelated edits and events reconcile without touching the draft',async({page})=>{
 await page.route('**/adoption-fixture',route=>route.fulfill({contentType:'text/html',body:`
 <header class="workspace-header"></header><div id="notice"></div><textarea id="draft">keep draft</textarea><button id="pin">Pin</button><p id="rail"></p>
 <script type="module">
 import {RailActions} from '/workspace/rail/rail-actions.js';
 import {WorkspaceEvents} from '/workspace/events/workspace-events.js';
 import {patchEvent} from '/workspace/events/workspace-events-policy.js';
 let task={id:'a',name:'Concurrent name',pinned:false,archived:true};window.calls=[];
 const api={json:async(path,options)=>{calls.push({path,method:options.method,body:JSON.parse(options.body)});return {task_id:'a',pinned:true,archived:false};},request:async()=>new Response(new ReadableStream({start(c){window.emit=e=>c.enqueue(new TextEncoder().encode('data: '+JSON.stringify(e)+'\\n\\n'));}}),{headers:{'content-type':'text/event-stream'}})};
 const draw=()=>document.querySelector('#rail').textContent=JSON.stringify(task);
 const actions=new RailActions(api,async()=>draw());document.querySelector('#pin').onclick=()=>actions.run(task,'pin');
 const events=new WorkspaceEvents(api,{refresh:async()=>{window.refreshes=(window.refreshes||0)+1;task={...task,name:'Event name'};task=events.snapshot([task])[0];draw();},event:e=>{if(patchEvent([task],e).resnapshot)events.scheduleSnapshot();}});
 await events.start();events.snapshot([task]);draw();window.ready=true;
 </script>`}));
 await page.goto('/adoption-fixture');await page.waitForFunction(()=>window.ready);
 await page.getByRole('button',{name:'Pin',exact:true}).click();
 await expect(page.locator('#rail')).toContainText('"archived":false');
 expect(await page.evaluate(()=>window.calls)).toEqual([{path:'/api/tasks/a/pin',method:'POST',body:{pinned:true}}]);
 await page.evaluate(()=>{emit({id:1,type:'task.updated',task_id:'a',payload:{changed_fields:['name']}});emit({id:2,type:'task.updated',task_id:'a',payload:{changed_fields:['pinned']}});});
 await expect(page.locator('#rail')).toContainText('Event name');expect(await page.evaluate(()=>window.refreshes)).toBe(1);
 await expect(page.locator('#draft')).toHaveValue('keep draft');
});

for(const recovery of ['reconnect','fallback poll'])test(`${recovery} converges missed pin changes from DTO snapshots`,async({page})=>{
 await page.clock.install();
 await page.route('**/pin-snapshot-fixture',route=>route.fulfill({contentType:'text/html',body:`
 <header class="workspace-header"></header><p id="rail"></p>
 <script type="module">
 import {RailActions} from '/workspace/rail/rail-actions.js';
 import {WorkspaceEvents} from '/workspace/events/workspace-events.js';
 window.dto={id:'a',pinned:true};window.reads=[];
 const api={json:async path=>{reads.push(path);return{tasks:[{...dto}]};},request:async()=>new Response(new ReadableStream(),{headers:{'content-type':'text/event-stream'}})};
 const actions=new RailActions(api,()=>{});
 const refresh=async()=>{events.beginSnapshot();const fence=actions.beginSnapshot();const result=await api.json('/api/tasks?archived=all');const tasks=events.snapshot(result.tasks);actions.reconcile(tasks,fence);document.querySelector('#rail').textContent=String(actions.pinned.has('a'));};
 const events=new WorkspaceEvents(api,{refresh});window.events=events;await events.start();await refresh();window.ready=true;
 </script>`}));
 await page.goto('/pin-snapshot-fixture');await page.waitForFunction(()=>window.ready);await expect(page.locator('#rail')).toHaveText('true');
 await page.evaluate(()=>{dto={id:'a',pinned:false};});
 if(recovery==='reconnect')await page.evaluate(()=>events.reconnect());else await page.clock.runFor(30000);
 await expect(page.locator('#rail')).toHaveText('false');
 await page.evaluate(()=>{dto={id:'a',pinned:true};});
 if(recovery==='reconnect')await page.evaluate(()=>events.reconnect());else await page.clock.runFor(30000);
 await expect(page.locator('#rail')).toHaveText('true');
 await page.evaluate(()=>{dto={id:'a'};});
 if(recovery==='reconnect')await page.evaluate(()=>events.reconnect());else await page.clock.runFor(30000);
 await expect(page.locator('#rail')).toHaveText('true');
 expect(await page.evaluate(()=>reads)).toEqual(Array(4).fill('/api/tasks?archived=all'));
});
