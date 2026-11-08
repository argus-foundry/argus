import test from 'node:test';
import assert from 'node:assert/strict';
import {ComposerBridge} from '../../composer/composer-bridge.js';

test('context and suggestion share one in-flight bridge read, without caching settled snapshots',async()=>{
 const held=[],calls=[];
 const bridge=new ComposerBridge({json:(...args)=>{calls.push(args);return new Promise(resolve=>held.push(resolve));}});
 const context=bridge.json('/api/tasks/a/bridge'),hint=bridge.json('/api/tasks/a/bridge');assert.equal(calls.length,1);
 held[0]({suggestion:null});assert.deepEqual(await context,await hint);
 const next=bridge.json('/api/tasks/a/bridge');assert.equal(calls.length,2);held[1]({suggestion:{text:'new'}});assert.equal((await next).suggestion.text,'new');
});

test('selection aborts shared reads and an old completion cannot clear the new task read',async()=>{
 const held=[],calls=[];const bridge=new ComposerBridge({json:(...args)=>{calls.push(args);return new Promise(resolve=>held.push(resolve));}});
 const old=bridge.json('/api/tasks/a/bridge');bridge.invalidate();assert.equal(calls[0][1].signal.aborted,true);
 const current=bridge.json('/api/tasks/b/bridge');held[0]({task_id:'a'});await old;
 assert.equal(bridge.json('/api/tasks/b/bridge'),current);assert.equal(calls.length,2);
 held[1]({task_id:'b'});assert.equal((await current).task_id,'b');
 const other=bridge.json('/api/accounts',{signal:calls[0][1].signal});assert.equal(calls[2][0],'/api/accounts');held[2]([]);await other;
});

test('context category requests retain the raw API transport and subscriber options',async()=>{
 const calls=[],response=Response.json({task_id:'a',live:true,session_id:'s',context:null});
 const api=new ComposerBridge({request:(...args)=>{calls.push(args);return Promise.resolve(response);}});
 const options={signal:new AbortController().signal,allowErrorResponse:true};
 assert.equal(await api.request('/api/tasks/a/context',options),response);
 assert.deepEqual(calls,[['/api/tasks/a/context',options]]);
});
