import {test} from 'node:test';
import assert from 'node:assert/strict';
import {roleStatus,taskRoles} from '../../../workspace/rail/task-grouping.js';
import {taskParents,taskForest} from '../../../workspace/rail/task-hierarchy.js';
const tasks=[{id:'c',name:'Coord',project:'p'},{id:'w',name:'Worker',project:'p',needs_input:true},{id:'n',name:'Nested',project:'p'},{id:'plain',name:'Plain',project:'p'}];
const roster={orchestrators:[{id:1,name:'Root',kanban_status:'active',roles:[{role_id:1,kind:'coordinator',live:true,task_id:'c',status:'working'},{role_id:2,kind:'worker',live:true,task_id:'w',status:'blocked'}]},{id:2,name:'Sub',kanban_status:'backlog',bridge_parent_orch_id:1,bridge_parent_role_id:2,roles:[{role_id:3,kind:'coordinator',live:true,task_id:'w'},{role_id:4,kind:'worker',live:true,task_id:'n',ready_to_close:true}]}]};
test('project forest nests bridge tasks once and keeps plain tasks reachable',()=>{
 const roots=taskForest(tasks,taskParents(tasks,roster));const root=roots[0];assert.equal(root.task.id,'c');assert.equal(root.children[0].task.id,'w');assert.equal(root.children[0].children[0].task.id,'n');assert.equal(root.needsInput,true);assert.equal(roots[1].task.id,'plain');
});
test('filtering out coordinator promotes matching worker without losing section',()=>{
 const roots=taskForest(tasks.filter(task=>task.id==='n'),taskParents(tasks,roster));assert.equal(roots[0].task.id,'n');
});
test('role tags classify ready and blocked and never label ordinary tasks',()=>{
 const roles=taskRoles(roster);assert.equal(roleStatus(roles.get('w')),'blocked');assert.equal(roleStatus(roles.get('n')),'ready');assert.equal(roleStatus(roles.get('plain')),'');
});
test('blocked role state stays visible even when an old ready marker remains',()=>{assert.equal(roleStatus({live:true,status:'blocked',ready_to_close:true}),'blocked');});
test('missing role status is not classified as idle',()=>{assert.equal(roleStatus({live:true}),'');});
test('a working role outranks a stale ready marker while idle or done roles still show ready',()=>{
 assert.equal(roleStatus({live:true,status:'working',ready_to_close:true},{live:true,idle:false}),'working');
 assert.equal(roleStatus({live:true,status:'failed',ready_to_close:true}),'failed');
 assert.equal(roleStatus({live:true,status:'done',ready_to_close:true}),'ready');
 assert.equal(roleStatus({live:true,status:'idle',ready_to_close:true}),'ready');
});
test('a working binding is not displaced by a stale ready binding for the same task',()=>{
 const live=(id,status,ready)=>({role_id:id,live:true,task_id:'t',status,ready_to_close:ready});
 const task={id:'t',live:true,idle:false};
 const pick=roles=>roleStatus(taskRoles({orchestrators:[{id:1,roles}]},[task]).get('t'),task);
 assert.equal(pick([live(1,'working',true),live(2,'done',true)]),'working');
 assert.equal(pick([live(1,'done',true),live(2,'working',true)]),'working');
 assert.equal(pick([live(1,'working',true),live(2,'blocked',true)]),'blocked');
 assert.equal(pick([live(1,'idle',false),live(2,'done',true)]),'ready');
});
for (const [statuses,expected] of [
 [['failed','working'],'failed'],
 [['failed','done'],'failed'],
 [['blocked','failed'],'blocked'],
]) {
 for (const order of [statuses,[...statuses].reverse()]) {
  test(`${expected} wins competing live bindings ordered ${order.join(', ')}`,()=>{
   const roles=order.map((status,index)=>({role_id:index+1,live:true,task_id:'t',status,ready_to_close:true}));
   const selected=taskRoles({orchestrators:roles.map(role=>({id:role.role_id,roles:[role]}))}).get('t');
   assert.equal(roleStatus(selected),expected);
  });
 }
}
test('a self-reported working status is shown only while the bound session is running',()=>{
 const role={live:true,status:'working'};
 assert.equal(roleStatus(role,{live:true,idle:false}),'working');
 assert.equal(roleStatus(role,{live:true,idle:true}),'idle');
 assert.equal(roleStatus(role,{live:false}),'stopped');
 assert.equal(roleStatus({...role,ready_to_close:true},{live:true,idle:true}),'ready');
 assert.equal(roleStatus({live:true,status:'blocked'},{live:true,idle:true}),'blocked');
});
test('a stale working binding does not outrank a ready binding for an idle task',()=>{
 const roles=[{role_id:1,live:true,task_id:'t',status:'working'},{role_id:2,live:true,task_id:'t',status:'done',ready_to_close:true}];
 const tasks=[{id:'t',live:true,idle:true}];
 for (const order of [roles,[...roles].reverse()]) assert.equal(taskRoles({orchestrators:[{id:1,roles:order}]},tasks).get('t').role_id,2);
 assert.equal(taskRoles({orchestrators:[{id:1,roles}]},[{id:'t',live:true,idle:false}]).get('t').role_id,1);
});

test('missing bound task cannot support a working tag',()=>{
 const role={live:true,status:'working'};
 assert.equal(roleStatus(role),'stopped');
 assert.equal(roleStatus({...role,ready_to_close:true}),'ready');
});
