import {test} from 'node:test';
import assert from 'node:assert/strict';
import {runTaskBatch} from '../../tasks/task-batch-policy.js';
for(const action of ['archive','restore','delete'])test(`${action} uses explicit captured endpoints with at most four requests`,async()=>{
 const calls=[];let running=0,max=0;
 const result=await runTaskBatch(Array.from({length:140},(_,i)=>({id:String(i),archived:i%2===0})),action,async(path,options)=>{
  running++;max=Math.max(max,running);await new Promise(resolve=>setTimeout(resolve,1));calls.push([path,options.method]);running--;
 });
 assert.equal(max,4);assert.equal(result.succeeded.length,140);assert.equal(calls.length,140);
 for(let i=0;i<140;i++)assert.ok(calls.some(([path,method])=>path===`/api/tasks/${i}${action==='delete'?'':action==='restore'?'/unarchive':'/archive'}`&&method===(action==='delete'?'DELETE':'POST')));
});
test('Stop prevents new dispatch and allows four in-flight requests to finish',async()=>{
 const control={stopped:false},releases=[],calls=[],progress=[];
 const pending=runTaskBatch(Array.from({length:140},(_,i)=>({id:String(i)})),'delete',path=>{calls.push(path);return new Promise(resolve=>releases.push(resolve));},state=>progress.push(state.processed),undefined,()=>{},control);
 assert.equal(calls.length,4);control.stopped=true;for(const release of releases)release();const result=await pending;
 assert.equal(calls.length,4);assert.equal(result.succeeded.length,4);assert.equal(result.notStarted.length,136);assert.deepEqual(progress,[1,2,3,4]);
});
for(const status of [401,403,0])test(`auth/network ${status} stops new dispatch without retrying in-flight IDs`,async()=>{
 const calls=[];const result=await runTaskBatch(Array.from({length:140},(_,i)=>({id:String(i)})),'delete',async path=>{calls.push(path);throw Object.assign(new Error('lost'),{status});});
 assert.equal(calls.length,4);assert.equal(new Set(calls).size,4);assert.equal(result.failed.length,4);assert.equal(result.notStarted.length,136);
});
test('ordinary failures do not prevent other IDs completing and retain reasons',async()=>{
 const result=await runTaskBatch([{id:'a'},{id:'b'}],'delete',async path=>{if(path.endsWith('/a'))throw Object.assign(new Error('server failure'),{status:500});});
 assert.deepEqual(result.succeeded,['b']);assert.equal(result.failed[0].id,'a');assert.equal(result.failed[0].error.message,'server failure');assert.deepEqual(result.notStarted,[]);
});
test('queued tasks that leave the observed scope are skipped before dispatch',async()=>{
 const pending=[],calls=[],allowed=new Set(['0','1','2','3','4']);
 const running=runTaskBatch(Array.from({length:5},(_,i)=>({id:String(i)})),'delete',path=>{calls.push(path);return new Promise(resolve=>pending.push(resolve));},()=>{},undefined,()=>{}, {stopped:false},task=>allowed.has(task.id));
 assert.equal(calls.length,4);allowed.delete('4');for(const release of pending)release();const result=await running;
 assert.equal(calls.length,4);assert.deepEqual(result.skipped.map(task=>task.id),['4']);assert.deepEqual(result.succeeded,['0','1','2','3']);
});
