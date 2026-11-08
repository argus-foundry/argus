import test from 'node:test';
import assert from 'node:assert/strict';
import {ToolRefresh} from '../../tasks/tools-live.js';
function fixture(){
 const calls=[],applied=[],errors=[],state={visible:true};let tick;
 const timers={setInterval(fn){tick=fn;return 1;},clearInterval(){tick=null;}};
 const refresh=new ToolRefresh({visible:()=>state.visible,read:(task,tool,signal)=>new Promise((resolve,reject)=>calls.push({task,tool,signal,resolve,reject})),apply:value=>applied.push(value),failed:error=>errors.push(error),timers});
 return{refresh,calls,applied,errors,state,poll:()=>tick?.()};
}
const settle=async()=>{for(let i=0;i<12;i++)await Promise.resolve();};
test('events only refresh the visible selected task; fallback covers missing events',async()=>{
 const f=fixture();f.refresh.select('a','changes');assert.equal(f.calls.length,1);f.calls[0].resolve({});await settle();
 f.refresh.event({task_id:'b',type:'session.idle'});assert.equal(f.calls.length,1);
 f.refresh.event({task_id:'a',type:'session.idle'});assert.equal(f.calls.length,2);f.calls[1].resolve({});await settle();
 f.poll();assert.equal(f.calls.length,3);f.calls[2].resolve({});await settle();f.state.visible=false;
 f.poll();f.refresh.event({task_id:'a',type:'session.exited'});assert.equal(f.calls.length,3);
 f.state.visible=true;f.refresh.trigger('focus');assert.equal(f.calls.length,4);f.refresh.stop();
});
test('single flight coalesces triggers and task switches abort and fence stale results',async()=>{
 const f=fixture();f.refresh.select('a','changes');f.poll();f.poll();assert.equal(f.calls.length,1);
 f.calls[0].resolve({first:true});await settle();assert.equal(f.calls.length,2);
 f.refresh.select('b','changes');assert.equal(f.calls[1].signal.aborted,true);assert.equal(f.calls.length,3);
 f.calls[1].resolve({stale:true});f.calls[2].resolve({current:true});await settle();assert.deepEqual(f.applied,[{first:true},{current:true}]);f.refresh.stop();
});
test('hidden selection defers reads; Artifacts shares triggers and failures permit retry',async()=>{
 const f=fixture();f.state.visible=false;f.refresh.select('a','artifacts');assert.equal(f.calls.length,0);
 f.state.visible=true;f.refresh.trigger('visible');assert.equal(f.calls[0].tool,'artifacts');f.calls[0].reject(new Error('offline'));await settle();assert.equal(f.errors.length,1);
 f.refresh.trigger('retry');assert.equal(f.calls.length,2);f.refresh.stop();
});

test('all write signals and resync refresh; status tool and stopped controller never poll',async()=>{
 const f=fixture();f.refresh.select('a','changes');f.calls[0].resolve({});await settle();
 for(const type of ['session.needs_input','session.exited','task.status_changed','resync']){
  const before=f.calls.length;f.refresh.event({task_id:'a',type});assert.equal(f.calls.length,before+1);f.calls.at(-1).resolve({});await settle();
 }
 f.refresh.select('a','status');const before=f.calls.length;f.poll();f.refresh.event({task_id:'a',type:'session.idle'});assert.equal(f.calls.length,before);f.refresh.stop();f.refresh.trigger('focus');assert.equal(f.calls.length,before);
});
