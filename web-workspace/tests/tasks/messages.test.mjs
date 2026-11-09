import {test} from 'node:test';import assert from 'node:assert/strict';
import {AgentMessages} from '../../tasks/messages/agent-messages.js';
import {CoordinatorInbox,coordinatorRoles,replyText} from '../../tasks/messages/coordinator-inbox.js';
test('history reads GET only, prepends older pages and deduplicates source plus id',async()=>{
 const calls=[],pages=[{messages:[{source:'hera',id:2}],next_before_id:2},{messages:[{source:'hera',id:1},{source:'hera',id:2}],next_before_id:null}];
 const view=new AgentMessages({json:async(path,options)=>{calls.push([path,options.method]);return pages.shift();}},null);view.render=()=>{};
 view.select({id:'a'});await view.refresh();await view.older();assert.deepEqual(view.messages.map(m=>m.id),[1,2]);assert.equal(calls.every(([,method])=>!method),true);assert.match(calls[1][0],/before_id=2/);
});
test('coordinator scope uses bindings and reply preserves the existing draft',()=>{
 const roster={orchestrators:[{roles:[{role_id:1,kind:'worker',live:true,task_id:'a'},{role_id:2,kind:'coordinator',live:true,task_id:'a'}]}]};
 assert.deepEqual(coordinatorRoles(roster,'a').map(r=>r.role_id),[2]);assert.equal(replyText('existing',{id:3,subject:'Decision'}),'existing\n\nReply to Inbox #3: Decision\n');
});
test('read-state and dismiss use viewed revision and publish authoritative response',async()=>{
 const calls=[],changed=[],item={id:1,role_id:2,subject:'Question',updated_at:'2026-11-08T00:00:00Z',read:false};
 const view=new CoordinatorInbox({json:async(path,options)=>{calls.push([path,JSON.parse(options.body),options.method]);return {item:{...item,read:true},unread_count:0};}},null,{changed:(...args)=>changed.push(args)});view.render=()=>{};
 view.select({id:'a'},{role_id:2});view.items=[item];await view.mutate(item,'read-state',{read:true});
 assert.deepEqual(calls[0],['/api/hera/roles/2/board/1/read-state',{read:true,expected_updated_at:item.updated_at},'PUT']);assert.equal(view.items[0].read,true);assert.deepEqual(changed[0],[2,0]);
 await view.mutate(item,'dismiss',{});assert.match(calls[1][0],/dismiss$/);assert.equal(calls[1][2],'POST');
});
test('conflicts refetch without automatically reading the changed item',async()=>{
 const calls=[],item={id:1,updated_at:'new',read:false};
 const view=new CoordinatorInbox({json:async(path,options)=>{calls.push([path,options?.method]);if(options?.method)throw Object.assign(new Error('conflict'),{status:409});return {items:[item],unread_count:1};}},null);view.render=()=>{};view.select({id:'a'},{role_id:2});
 await view.mutate({id:1,updated_at:'old'},'read-state',{read:true});assert.equal(calls.length,2);assert.equal(view.items[0].read,false);assert.match(view.error,/changed/);
});
test('history and board ignore late responses after scope teardown',async()=>{
 for(const Class of [AgentMessages,CoordinatorInbox]){
 let release;const view=new Class({json:()=>new Promise(resolve=>release=resolve)},null);view.render=()=>{};view.select({id:'a'},{role_id:2});const read=view.refresh();view.destroy();release({messages:[{id:1}],items:[{id:1}]});await read;
 assert.equal(view.task,null);
 }
});

test('board events coalesce during reads and mutation replies cannot cross task scopes',async()=>{
 const releases=[];const view=new CoordinatorInbox({json:()=>new Promise(resolve=>releases.push(resolve))},null);view.render=()=>{};view.select({id:'a'},{role_id:2});
 const initial=view.refresh();view.event({type:'hera.board_changed',payload:{role_id:2}});view.event({type:'hera.board_changed',payload:{role_id:2}});
 releases[0]({items:[],unread_count:0});await initial;await new Promise(setImmediate);assert.equal(releases.length,2);
 releases[1]({items:[{id:1,read:false}],unread_count:1});await view.flight;
 const write=view.mutate({id:1,updated_at:'old'},'read-state',{read:true});view.select({id:'b'},{role_id:3});releases[2]({item:{id:1,read:true},unread_count:0});await write;assert.deepEqual(view.items,[]);
});

for(const kind of ['history','board'])test(`${kind} stale response cannot replace the next task data`,async()=>{
 const releases=[],Class=kind==='history'?AgentMessages:CoordinatorInbox;
 const view=new Class({json:()=>new Promise(resolve=>releases.push(resolve))},null);view.render=()=>{};
 view.select({id:'a'},{role_id:1});const old=view.refresh();view.select({id:'b'},{role_id:2});const next=view.refresh();
 releases[1]({messages:[{source:'hera',id:20}],items:[{id:20}],next_before_id:20});await next;
 releases[0]({messages:[{source:'hera',id:10}],items:[{id:10}],next_before_id:10});await old;
 assert.deepEqual((kind==='history'?view.messages:view.items).map(row=>row.id),[20]);if(kind==='history')assert.equal(view.cursor,20);
});
test('history prunes expansion identities to retained messages',async()=>{
 let id=1;const view=new AgentMessages({json:async()=>({messages:[{source:'hera',id:id++}],next_before_id:null})},null);view.render=()=>{};view.select({id:'a'});
 await view.refresh();view.expanded.add('hera:1');await view.refresh();assert.equal(view.expanded.size,0);
});
