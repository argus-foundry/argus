import {test} from 'node:test';
import assert from 'node:assert/strict';
import {KanbanDisclosure} from '../../../workspace/rail/kanban-group.js';
test('Done defaults closed, preferences survive reconstruction and projects are independent',t=>{
 const values=new Map();const original=globalThis.localStorage;
 globalThis.localStorage={getItem:key=>values.get(key),setItem:(key,value)=>values.set(key,value)};t.after(()=>globalThis.localStorage=original);
 const disclosure=new KanbanDisclosure();assert.equal(disclosure.collapsed('p','done'),true);assert.equal(disclosure.collapsed('p','active'),false);
 disclosure.set('p','done',false);disclosure.set('p','blocked',true);
 const restored=new KanbanDisclosure();assert.equal(restored.collapsed('p','done'),false);assert.equal(restored.collapsed('p','blocked'),false);assert.equal(restored.collapsed('other','done'),true);
});
test('blocked or malformed storage preserves defaults and usable disclosure',t=>{
 const original=globalThis.localStorage;t.after(()=>globalThis.localStorage=original);
 for(const value of ['[]','false','null','"bad"','not JSON']){
  globalThis.localStorage={getItem:()=>value,setItem:()=>{throw new Error('Blocked');}};
  const disclosure=new KanbanDisclosure();assert.equal(disclosure.collapsed('p','done'),true);disclosure.set('p','done',false);assert.equal(disclosure.collapsed('p','done'),false);
 }
});

test('deferred footers default closed and blocked cannot be hidden by saved disclosure',t=>{
 const original=globalThis.localStorage;t.after(()=>globalThis.localStorage=original);
 globalThis.localStorage={getItem:()=>JSON.stringify({'["p","blocked"]':true}),setItem:()=>{throw new Error('Blocked');}};
 const disclosure=new KanbanDisclosure();
 assert.equal(disclosure.collapsed('p','backlog'),true);
 assert.equal(disclosure.collapsed('p','blocked'),false);
 disclosure.set('p','backlog',false);assert.equal(disclosure.collapsed('p','backlog'),false);
});

test('invalid stored disclosure values keep default footers closed',t=>{
 const original=globalThis.localStorage;t.after(()=>globalThis.localStorage=original);
 for(const value of ['false',0,[],{}]){
  globalThis.localStorage={getItem:()=>JSON.stringify({'["p","backlog"]':value})};
  assert.equal(new KanbanDisclosure().collapsed('p','backlog'),true);
 }
});
