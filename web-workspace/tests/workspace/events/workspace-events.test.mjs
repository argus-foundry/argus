import {test} from 'node:test';import assert from 'node:assert/strict';import {patchEvent,SSEFrames} from '../../../workspace/events/workspace-events-policy.js';
import {WorkspaceEvents} from '../../../workspace/events/workspace-events.js';
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
