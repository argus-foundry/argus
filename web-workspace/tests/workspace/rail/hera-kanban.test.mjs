import {test} from 'node:test';
import assert from 'node:assert/strict';
import {coordinators,kanbanGroups,kanbanLabel,HeraKanban} from '../../../workspace/rail/hera-kanban.js';
const orch=(id,task,status,pinned=false,extra={})=>({id,kanban_status:status,pinned,roles:[{kind:'coordinator',live:true,task_id:task}],...extra});
const roster={orchestrators:[orch(1,'a','active'),orch(2,'b','done'),orch(3,'c','active',true),orch(4,'d','blocked'),orch(5,'e','backlog'),orch(6,'nested','done',false,{bridge_parent_orch_id:1})]};
const entries=['plain','a','b','c','d','e'].map(id=>({task:{id},children:[]}));
test('groups root coordinator trees in fixed order and pins within their group',()=>{
 const result=kanbanGroups(entries,coordinators(roster));
 assert.deepEqual(result.plain.map(e=>e.task.id),['plain']);
 assert.deepEqual(result.groups.map(g=>g.status),['active','backlog','blocked','done']);
 assert.deepEqual(result.groups[0].entries.map(e=>e.task.id),['c','a']);
 assert.equal(coordinators(roster).get('nested').root,false);
});
test('missing state defaults active; empty groups and promoted workers have no fake grouping',()=>{
 const result=kanbanGroups(entries.slice(0,2),coordinators({orchestrators:[orch(1,'a')]}));
 assert.deepEqual(result.groups.map(g=>g.status),['active']);assert.equal(result.plain.length,1);
});
test('optimistic failure rolls back, deduplicates and reports feedback',async()=>{
 let reject,requests=0;const messages=[];
 const action=new HeraKanban({json:()=>{requests++;return new Promise((_,no)=>reject=no);}},()=>{},async()=>{},message=>messages.push(message));
 const coordinator=coordinators(roster).get('a');
 const run=action.run(coordinator,'kanban','done');
 assert.equal(action.value(coordinator).kanban_status,'done');
 await action.run(coordinator,'kanban','blocked');assert.equal(requests,1);
 reject(new Error('Denied'));await run;
 assert.equal(action.value(coordinator).kanban_status,'active');assert.match(messages[0],/Denied/);
});
test('pin uses orchestrator endpoint and successful refresh replaces optimistic override',async()=>{
 const requests=[];const action=new HeraKanban({json:async(...args)=>requests.push(args)},()=>{},async()=>{},()=>{});
 const coordinator=coordinators(roster).get('a');await action.run(coordinator,'pin');
 assert.equal(requests[0][0],'/api/hera/orchestrators/1/pin');assert.equal(requests[0][1].method,'POST');
 assert.equal(action.overrides.size,0);
});

test('kanban sends status body and clear fences a late rejected write',async()=>{
 let reject;const messages=[],requests=[];let renders=0;
 const action=new HeraKanban({json:(...args)=>{requests.push(args);return new Promise((_,no)=>reject=no);}},()=>renders++,async()=>{},message=>messages.push(message));
 const run=action.run(coordinators(roster).get('a'),'kanban','backlog');
 assert.equal(requests[0][0],'/api/hera/orchestrators/1/kanban');assert.deepEqual(JSON.parse(requests[0][1].body),{status:'backlog'});
 action.clear();reject(new Error('Stopped'));await run;
 assert.equal(action.overrides.size,0);assert.equal(action.pending.size,0);assert.equal(renders,1);assert.deepEqual(messages,[]);
});
test('nested and invalid destinations never write, and pin toggles to unpin',async()=>{
 const paths=[];const action=new HeraKanban({json:async(path)=>paths.push(path)},()=>{},async()=>{},()=>{});
 await action.run(coordinators(roster).get('nested'),'kanban','active');await action.run(coordinators(roster).get('a'),'kanban','invalid');await action.run(undefined,'pin');
 assert.deepEqual(paths,[]);await action.run(coordinators(roster).get('c'),'pin');assert.deepEqual(paths,['/api/hera/orchestrators/3/unpin']);
});

test('rejected pin restores authoritative value and clears pending optimistic state',async()=>{
 let reject;const requests=[],messages=[],renders=[];
 const coordinator=coordinators(roster).get('a');
 const action=new HeraKanban({json:(...args)=>{requests.push(args);return new Promise((_,no)=>reject=no);}},()=>renders.push(action.value(coordinator).pinned),async()=>assert.fail('Rejected pins must not refresh'),message=>messages.push(message));
 const run=action.run(coordinator,'pin');
 assert.equal(action.value(coordinator).pinned,true);
 assert.equal(action.pending.has(coordinator.id),true);assert.deepEqual(action.overrides.get(coordinator.id),{pinned:true});
 assert.equal(requests[0][0],'/api/hera/orchestrators/1/pin');
 reject(new Error('Denied'));await run;
 assert.equal(action.value(coordinator).pinned,false);
 assert.equal(action.pending.size,0);assert.equal(action.overrides.size,0);
 assert.deepEqual(renders,[true,false]);assert.match(messages[0],/not confirmed.*Denied/);
});

test('active triage uses plain In progress copy',()=>{assert.equal(kanbanLabel('active'),'In progress');});
test('unknown triage copy follows the default In progress classification',()=>{
 for(const status of [undefined,'unknown','','__proto__','constructor'])assert.equal(kanbanLabel(status),'In progress');
});
