import test from 'node:test';
import assert from 'node:assert/strict';
import {Api} from '../../core/api.js';
import {TerminalInput} from '../../terminal/terminal-input.js';

test('coded input rejection fences queued bytes and never retries rejected input', async t => {
 const original=globalThis.fetch;let posts=0,fences=0;let task={id:'a',live:true,status:'in_progress'};
 t.after(()=>{globalThis.fetch=original;});
 globalThis.fetch=async path=>path.endsWith('/input')?(posts++,new Response(JSON.stringify({code:'task_archived',error:'secret'}),{status:409})):new Response('{"backends":[]}');
 const api=new Api(()=>task);api.inputRejected=()=>{fences++;task={...task,archive_pending:true};};
 const abort=new AbortController();t.after(()=>abort.abort());
 const input=new TerminalInput(api,task,abort.signal,()=>true);
 await assert.rejects(input.raw('first'),error=>error.code==='task_archived'&&!error.message.includes('secret'));
 await assert.rejects(input.raw('queued'));
 assert.equal(posts,1);assert.equal(fences,1);
 task={...task,archive_pending:false,archived:true};await assert.rejects(input.raw('still archived'));assert.equal(posts,1);
 task={...task,archived:false};globalThis.fetch=async()=>{posts++;return new Response('{}');};
 await input.raw('deliberate new input');assert.equal(posts,2);
});
test('only known guard codes on matching task operations become public error fields',async t=>{
 const original=globalThis.fetch;t.after(()=>{globalThis.fetch=original;});
 for(const [path,code,expected] of [['/api/tasks/a/resume','hera_closeout','hera_closeout'],['/api/tasks/a/restart','hera_closeout','hera_closeout'],['/api/tasks/a/resume','task_running',undefined],['/api/tasks/a/input','private-secret',undefined],['/api/accounts','hera_closeout',undefined]]){
  globalThis.fetch=async()=>new Response(JSON.stringify({code,can_force_revive:true,error:'private-secret'}),{status:409});
  await assert.rejects(new Api().request(path,{method:'POST'}),error=>{assert.equal(error.code,expected);assert.equal(error.canForceRevive,expected==='hera_closeout'?true:undefined);assert.doesNotMatch(error.message,/private-secret/);return true;});
 }
});

test('authoritative archive fence survives pre-rejection snapshots and still-archived reads',async()=>{
 const {WorkspaceEvents}=await import('../../workspace/events/workspace-events.js');
 const {patchEvent}=await import('../../workspace/events/workspace-events-policy.js');
 let tasks=[{id:'a',status:'in_progress',live:true,archived:false}];let scheduled=0;
 const events=Object.assign(Object.create(WorkspaceEvents.prototype),{archivePending:new Map(),archiveRevision:0,snapshotArchives:new Map(),pending:[],scheduleSnapshot(){scheduled++;},problem(){},callbacks:{event:event=>{tasks=patchEvent(tasks,event).tasks;}}});
 events.beginSnapshot();events.rejectInput('a');assert.equal(tasks[0].archive_pending,true);
 tasks=events.snapshot([{id:'a',archived:false}],tasks);assert.equal(tasks[0].archive_pending,true,'pre-rejection snapshot cannot release the fence');
 events.beginSnapshot();tasks=events.snapshot([{id:'a',archived:true}],tasks);assert.equal(tasks[0].archived,true);assert.equal(tasks[0].archive_pending,false);assert.ok(scheduled>0);
 events.beginSnapshot();tasks=events.snapshot([{id:'a',archived:false}],tasks);assert.equal(tasks[0].archived,false);assert.equal(tasks[0].archive_pending,false);
});
