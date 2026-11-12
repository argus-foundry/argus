import test from 'node:test';
import assert from 'node:assert/strict';
import {ToolRefresh} from '../../tasks/tools-live.js';
function fixture(){
 const calls=[],applied=[],errors=[],state={visible:true},intervals=[],timeouts=[];
 const timers={setInterval(fn){intervals.push(fn);return fn;},clearInterval(){},setTimeout(fn){timeouts.push(fn);return fn;},clearTimeout(fn){const i=timeouts.indexOf(fn);if(i>=0)timeouts.splice(i,1);}};
 const refresh=new ToolRefresh({visible:()=>state.visible,read:(task,tool,signal)=>new Promise((resolve,reject)=>calls.push({task,tool,signal,resolve,reject})),apply:value=>applied.push(value),failed:error=>errors.push(error),timers});
 return{refresh,calls,applied,errors,state,intervals,timeouts};
}
const settle=async()=>{for(let i=0;i<12;i++)await Promise.resolve();};
const event=revision=>({task_id:'a',type:'worktree.changed',payload:revision?{revision}:{reason:'filesystem'}});
test('worktree events refresh only matching Changes; Changes has no discovery interval',async()=>{
 const f=fixture();f.refresh.select('a','changes');f.calls[0].resolve({git_revision:'r1'});await settle();assert.equal(f.intervals.length,0);
 f.refresh.event({...event('r2'),task_id:'b'});f.refresh.event(event('r1'));assert.equal(f.calls.length,1);
 f.refresh.event(event('r2'));f.refresh.event(event('r2'));assert.equal(f.calls.length,2);f.calls[1].resolve({git_revision:'r2'});await settle();assert.equal(f.calls.length,2);
 f.refresh.event(event());assert.equal(f.calls.length,3);f.refresh.stop();
});
test('events fence earlier in-flight snapshots and coalesce a fresh read',async()=>{
 const f=fixture();f.refresh.select('a','changes');f.refresh.event(event('r2'));f.refresh.event(event('r3'));
 f.calls[0].resolve({git_revision:'r1'});await settle();assert.deepEqual(f.applied,[]);assert.equal(f.calls.length,2);
 f.calls[1].resolve({git_revision:'r3'});await settle();assert.deepEqual(f.applied,[{git_revision:'r3'}]);
 f.refresh.select('b','changes');f.refresh.select('c','changes');assert.equal(f.calls[2].signal.aborted,true);f.calls[2].resolve({old:true});f.calls[3].resolve({current:true});await settle();assert.equal(f.applied.at(-1).current,true);f.refresh.stop();
});
test('hidden events defer until visible; resync forces a fresh read independent of revision',async()=>{
 const f=fixture();f.state.visible=false;f.refresh.select('a','changes');f.refresh.event(event('r2'));assert.equal(f.calls.length,0);
 f.state.visible=true;f.refresh.trigger('visible');f.calls[0].resolve({git_revision:'r2'});await settle();f.refresh.event({type:'resync'});assert.equal(f.calls.length,2);f.refresh.stop();
});
test('stale snapshots remain labeled and retry with a finite backoff; explicit retry recovers',async()=>{
 const f=fixture();f.refresh.select('a','changes');
 for(let i=0;i<4;i++){f.calls.at(-1).resolve({git_revision:'r1',stale:true});await settle();if(i<3)f.timeouts.shift()();}
 assert.equal(f.calls.length,4);assert.equal(f.timeouts.length,0);assert.equal(f.errors.length,1);assert.equal(f.applied.at(-1).stale,true);
 f.refresh.trigger('retry');f.calls.at(-1).resolve({git_revision:'r2',stale:false});await settle();assert.equal(f.applied.at(-1).git_revision,'r2');f.refresh.stop();
});
test('Artifacts retains its interval and write signals, failures permit manual retry',async()=>{
 const f=fixture();f.refresh.select('a','artifacts');f.calls[0].resolve({});await settle();assert.equal(f.intervals.length,1);
 f.refresh.event(event('r2'));assert.equal(f.calls.length,1);f.intervals[0]();f.calls[1].reject(new Error('offline'));await settle();assert.equal(f.errors.length,1);
 f.refresh.trigger('retry');f.calls[2].resolve({});await settle();f.refresh.event({task_id:'a',type:'session.idle'});assert.equal(f.calls.length,4);f.refresh.stop();
});
test('session write signals and resync remain supported; status and stopped owners do not read',async()=>{
 const f=fixture();f.refresh.select('a','changes');f.calls[0].resolve({});await settle();
 for(const type of ['session.idle','session.needs_input','session.exited','task.status_changed','resync']){const count=f.calls.length;f.refresh.event({type,task_id:'a'});assert.equal(f.calls.length,count+1);f.calls.at(-1).resolve({});await settle();}
 f.refresh.select('a','status');const count=f.calls.length;f.refresh.event(event());f.refresh.trigger('focus');assert.equal(f.calls.length,count);f.refresh.stop();f.refresh.trigger('focus');assert.equal(f.calls.length,count);
});
