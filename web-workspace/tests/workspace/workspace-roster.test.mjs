import {test} from 'node:test';import assert from 'node:assert/strict';
import {WorkspaceRoster} from '../../workspace/workspace-roster.js';
test('a stopped roster watcher ignores late results and schedules no new reads',async t=>{
 t.mock.timers.enable({apis:['setTimeout']});const original=globalThis.document;globalThis.document={hidden:false,addEventListener(){}};t.after(()=>{globalThis.document=original;});let release,reads=0;const applied=[];
 const watcher=new WorkspaceRoster({json:()=>{reads++;return new Promise(resolve=>release=resolve);}},()=>true,result=>applied.push(result));t.after(()=>watcher.stop());watcher.start();t.mock.timers.tick(5000);assert.equal(reads,1);watcher.stop();release({orchestrators:[{id:1}]});await new Promise(setImmediate);t.mock.timers.tick(20000);assert.deepEqual(applied,[]);assert.equal(reads,1);
});
test('inactive or hidden presentation defers roster reads until explicitly visible',async t=>{
 t.mock.timers.enable({apis:['setTimeout']});const original=globalThis.document;globalThis.document={hidden:false,addEventListener(){}};t.after(()=>{globalThis.document=original;});let active=false,reads=0;const applied=[];
 const watcher=new WorkspaceRoster({json:async()=>{reads++;return{orchestrators:[]};}},()=>active,result=>applied.push(result));t.after(()=>watcher.stop());watcher.start();t.mock.timers.tick(5000);assert.equal(reads,0);active=true;document.hidden=true;await watcher.refresh();assert.equal(reads,0);document.hidden=false;await watcher.refresh();assert.equal(reads,1);assert.equal(applied.length,1);
});
