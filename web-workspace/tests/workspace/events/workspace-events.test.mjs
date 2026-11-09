import {test} from 'node:test';import assert from 'node:assert/strict';import {patchEvent,SSEFrames} from '../../../workspace/events/workspace-events-policy.js';
import {WorkspaceEvents} from '../../../workspace/events/workspace-events.js';
import {ComposerContext} from '../../../composer/composer-context.js';
test('named event patches task state and unknown events request resnapshot',()=>{
 const tasks=[{id:'a',status:'in_progress',live:true}];assert.equal(patchEvent(tasks,{type:'task.status_changed',task_id:'a',payload:{to:'in_review'}}).tasks[0].status,'in_review');
 assert.equal(patchEvent(tasks,{type:'session.needs_input',task_id:'a',payload:{needs_input:true}}).tasks[0].needs_input,true);
 assert.equal(patchEvent(tasks,{type:'future.event'}).resnapshot,true);assert.equal(patchEvent(tasks,{type:'resync'}).resnapshot,true);
});
test('SSE frames survive chunk boundaries, CRLF and comments',()=>{
 const parser=new SSEFrames(),result=[];for(const text of [':keep\r\n\r\nevent: task.ren','amed\r\ndata: {"id":2,','"type":"task.renamed"}\r\n\r\n'])result.push(...parser.feed(text));assert.equal(result.length,1);assert.equal(result[0].id,2);
});
test('archive envelopes request authoritative reconciliation without overriding restored state',()=>{
 const tasks=[{id:'a',archived:false,live:true,status:'in_progress'}];
 const result=patchEvent(tasks,{type:'task.archived',task_id:'a'});
 assert.equal(result.tasks[0].archived,false);
 assert.equal(result.resnapshot,true);
});

test('archive marks pending immediately and still requests reconciliation',()=>{
 const tasks=[{id:'a',archived:false},{id:'b',archived:false}];
 const result=patchEvent(tasks,{type:'task.archived',task_id:'a'});
 assert.deepEqual(result.tasks,[{id:'a',archived:false,archive_pending:true},{id:'b',archived:false}]);
 assert.equal(result.resnapshot,true);
 assert.equal(tasks[0].archived,false);
});

for(const retry of ['scheduled','manual'])test(`${retry} SSE recovery lets a successful snapshot clear a later warning`,async t=>{
 t.mock.timers.enable({apis:['setTimeout']});
 const original=globalThis.document;
 globalThis.document={addEventListener(){},createElement:()=>({contains(){return false;},setAttribute(){},addEventListener(){},append(){}}),querySelector:()=>({after(){}}),body:{classList:{toggle(){}}}};
 let snapshots=0;
 const events=new WorkspaceEvents({request:async()=>new Response(new ReadableStream(),{headers:{'content-type':'text/event-stream'}})},{refresh:async()=>{snapshots++;events.healthy();}});
 t.after(()=>{events.stop();globalThis.document=original;});events.running=true;
 events.failed(new Error('SSE unavailable'));
 events.healthy();assert.equal(events.banner.hidden,false,'A pending retry must keep the warning visible');
 if(retry==='scheduled'){t.mock.timers.tick(8000);await new Promise(setImmediate);}else await events.reconnect();
 assert.equal(snapshots,1);assert.equal(events.banner.hidden,true);
 events.problem('Snapshot failed');assert.equal(events.banner.hidden,false);
 await events.callbacks.refresh();assert.equal(events.banner.hidden,true,'A successful snapshot must clear the warning after SSE recovery');
 t.mock.timers.tick(8000);await new Promise(setImmediate);assert.equal(snapshots,2,'Consumed or cancelled retries must not run again');
});

test('context invalidations bypass baseline buffering without forcing task resnapshots',async t=>{
 const original=globalThis.document;
 globalThis.document={addEventListener(){},createElement:()=>({contains(){return false;},setAttribute(){},addEventListener(){},append(){}}),querySelector:()=>({after(){}}),body:{classList:{toggle(){}}}};
 const delivered=[],context={id:1,type:'task.context_changed',task_id:'a',payload:{session_id:'s2'}};
 let stream;const events=new WorkspaceEvents({request:async()=>new Response(new ReadableStream({start(controller){stream=controller;}}),{headers:{'content-type':'text/event-stream'}})},{context:event=>delivered.push(event),refresh(){}});
 t.after(()=>{events.stop();globalThis.document=original;});await events.start();
 stream.enqueue(new TextEncoder().encode(`event: task.context_changed\ndata: ${JSON.stringify(context)}\n\n`));
 await new Promise(setImmediate);
 assert.deepEqual(delivered,[context]);assert.deepEqual(events.pending,[]);
 const started={id:2,type:'session.started',task_id:'a'};
 stream.enqueue(new TextEncoder().encode(`data: ${JSON.stringify(started)}\n\n`));await new Promise(setImmediate);
 assert.deepEqual(delivered,[context,started]);assert.deepEqual(events.pending,[started]);
 assert.equal(patchEvent([{id:'a'}],context).resnapshot,false);
});

test('session exit SSE fences selected context immediately while retaining the buffered task update',async t=>{
 const original=globalThis.document;
 globalThis.document={hidden:false,addEventListener(){},createElement:()=>({contains(){return false;},setAttribute(){},addEventListener(){},append(){}}),querySelector:()=>({after(){}}),body:{classList:{toggle(){}}}};
 const previous={task_id:'a',session_id:'s1',live:true,context:{used_tokens:42,categories:{messages:42}}},releases=[],ordinary=[];
 const context=Object.create(ComposerContext.prototype);
 Object.assign(context,{task:{id:'a'},data:{measurement:previous},render(){},api:{request:()=>new Promise(resolve=>releases.push(resolve)),json:async()=>({})}});
 let stream;const events=new WorkspaceEvents({request:async()=>new Response(new ReadableStream({start(controller){stream=controller;}}),{headers:{'content-type':'text/event-stream'}})},{context:event=>context.event(event),event:event=>ordinary.push(event),refresh(){}});
 t.after(()=>{context.cancelReads();events.stop();releases.forEach(release=>release(Response.json(previous)));globalThis.document=original;});
 const old=context.refreshMeasurement(),controller=context.controller;await events.start();
 const exited={id:1,type:'session.exited',task_id:'a'};
 stream.enqueue(new TextEncoder().encode(`event: session.exited\ndata: ${JSON.stringify(exited)}\n\n`));await new Promise(setImmediate);
 assert.equal(controller.signal.aborted,true);assert.equal(context.data.measurement,null);assert.equal(releases.length,2);
 assert.deepEqual(events.pending,[exited]);assert.deepEqual(ordinary,[]);
 releases[1](Response.json({...previous,live:false,context:{used_tokens:77,categories:{messages:77}}}));await context.contextRead;
 releases[0](Response.json(previous));await old;
 assert.equal(context.data.measurement.live,false);assert.equal(context.data.measurement.context.categories.messages,77);
});

test('suggestion invalidation reaches the composer while task snapshots buffer events',async()=>{
 const delivered=[],event={id:7,type:'task.suggestion_changed',task_id:'a',payload:{available:false}};
 const signal=new AbortController();
 const events=Object.assign(Object.create(WorkspaceEvents.prototype),{running:true,generation:1,cursor:0,buffering:true,pending:[],failed:error=>{throw error;},callbacks:{suggestion:value=>{delivered.push(value);signal.abort();}}});
 const response=new Response(new ReadableStream({start(controller){controller.enqueue(new TextEncoder().encode(`data: ${JSON.stringify(event)}\n\n`));controller.close();}}));
 await events.consume(response,1,signal.signal);
 assert.deepEqual(delivered,[event]);
 assert.equal(patchEvent([{id:'a'}],event).resnapshot,false);
});


test('task.updated wire events request an authoritative task snapshot',()=>{
 const tasks=[{id:'a',pinned:false,archived:true}];
 const [event]=new SSEFrames().feed('event: task.updated\ndata: {"type":"task.updated","task_id":"a","payload":{"changed_fields":["archived","pinned"]}}\n\n');
 assert.equal(event.type,'task.updated');
 const result=patchEvent(tasks,event);
 assert.equal(result.resnapshot,true);
 assert.deepEqual(result.tasks,tasks);
});

test('legacy poll stops after task.updated and rearms on reconnect',async t=>{
 t.mock.timers.enable({apis:['setInterval']});let reads=0,stream;
 const original=globalThis.document;globalThis.document={hidden:false,addEventListener(){},createElement:()=>({contains(){return false;},setAttribute(){},addEventListener(){},append(){}}),querySelector:()=>({after(){}}),body:{classList:{toggle(){}}}};
 const events=new WorkspaceEvents({request:async()=>new Response(new ReadableStream({start(controller){stream=controller;}}),{headers:{'content-type':'text/event-stream'}})},{refresh:async()=>reads++});
 t.after(()=>{events.stop();globalThis.document=original;});await events.start();t.mock.timers.tick(30000);assert.equal(reads,1);
 stream.enqueue(new TextEncoder().encode('data: {"id":1,"type":"task.updated","task_id":"a"}\n\n'));await new Promise(setImmediate);
 t.mock.timers.tick(60000);assert.equal(reads,1);await events.reconnect();assert.equal(reads,2);t.mock.timers.tick(30000);assert.equal(reads,3);
});
