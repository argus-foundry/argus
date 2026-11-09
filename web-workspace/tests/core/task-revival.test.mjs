import test from 'node:test';
import assert from 'node:assert/strict';
import {reviveTask} from '../../tasks/task-revival.js';
for(const operation of ['resume','restart'])for(const accepted of [false,true])test(`${operation} close-out asks deliberately and submits at most once`,async()=>{
 const calls=[];let confirmations=0;
 const api={json:async(path,options)=>{calls.push({path,body:options.body});if(calls.length===1)throw Object.assign(new Error('closed'),{status:409,code:'hera_closeout',canForceRevive:true});return {status:'ok'};}};
 const result=reviveTask(api,{id:'a',name:'A'},operation,null,new AbortController().signal,async()=>{confirmations++;return accepted;});
 if(accepted){assert.deepEqual(await result,{status:'ok'});assert.deepEqual(JSON.parse(calls[1].body),{force_revive:true});}else await assert.rejects(result,/canceled/);
 assert.equal(calls.length,accepted?2:1);assert.equal(confirmations,1);
});
for(const code of ['task_running','agent_restarting','other'])test(`${code} never offers force`,async()=>{
 let confirm=0,calls=0;await assert.rejects(reviveTask({json:async()=>{calls++;throw Object.assign(new Error('conflict'),{status:409,code,canForceRevive:true});}},{id:'a'},'resume',null,null,async()=>{confirm++;return true;}));assert.equal(calls,1);assert.equal(confirm,0);
});
test('failed forced revival never loops or submits input',async()=>{
 let calls=0;await assert.rejects(reviveTask({json:async()=>{calls++;throw Object.assign(new Error('failure'),{status:409,code:'hera_closeout',canForceRevive:true});}},{id:'a'},'resume',null,null,async()=>true),/failure/);assert.equal(calls,2);
});
