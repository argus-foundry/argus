import test from 'node:test';
import assert from 'node:assert/strict';
import {discardFile,discardRequest} from '../../tasks/changes/discard-file.js';
for(const untracked of [false,true])test(`discard captures a path and sends explicit deletion ${untracked}`,async()=>{
 const calls=[],api={request:async(path,options)=>{calls.push({path,...options});return Response.json({discarded:true});}};
 const signal=new AbortController().signal;
 await discardRequest(api,{id:'task space'},'src/file space.txt',untracked,signal);
 assert.equal(calls[0].path,'/api/tasks/task%20space/git/discard');assert.deepEqual(JSON.parse(calls[0].body),{path:'src/file space.txt',scope:'local',delete_untracked:untracked});assert.equal(calls[0].signal,signal);
});
for(const [code,status,pattern] of [
 ['rename_requires_explicit_paths',409,/rename.*both paths/i],['untracked_requires_delete',409,/untracked.*explicit/i],['conflict_state',409,/conflict/i],['no_changes',409,/no local changes/i],['task_archived',409,/archived/i],['invalid_path',400,/path.*invalid/i],['invalid_request',400,/request.*invalid/i],['invalid_scope',400,/scope/i],['task_not_found',404,/task.*unavailable/i],['worktree_not_found',404,/worktree.*unavailable/i],['git_failed',500,/not confirmed/i],
])test(`discard explains ${code} without retrying`,async()=>{
 let reads=0;await assert.rejects(()=>discardRequest({request:async()=>{reads++;return Response.json({code,error:code},{status});}},{id:'a'},'f',false),pattern);assert.equal(reads,1);
});
test('cancel sends no mutation and confirmation names captured scope and path',async()=>{
 const task={id:'a'},file={path:'draft space.txt',status:'??'};let config,calls=0;
 await discardFile({api:{request(){calls++;}},task,file,confirm:options=>{config=options;return Promise.resolve(false);}});
 assert.equal(calls,0);assert.match(config.message,/draft space.txt/);assert.match(config.message,/permanently delete/i);assert.equal(config.danger,true);
});
test('confirmation retains captured identity and cancellation fences a late response',async()=>{
 const abort=new AbortController(),calls=[],task={id:'original'},file={path:'original path',status:'M'};let config,release;
 await discardFile({api:{request:(path,options)=>{calls.push({path,...options});return new Promise(resolve=>{release=resolve;});}},task,file,signal:abort.signal,confirm:options=>{config=options;return Promise.resolve(false);}});
 task.id='other';file.path='other';const operation=config.onConfirm();abort.abort();release(Response.json({discarded:true}));await assert.rejects(()=>operation,{name:'AbortError'});
 assert.equal(calls[0].path,'/api/tasks/original/git/discard');assert.equal(JSON.parse(calls[0].body).path,'original path');assert.equal(calls.length,1);
});
