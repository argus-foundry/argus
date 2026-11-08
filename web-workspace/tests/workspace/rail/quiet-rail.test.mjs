import {test} from 'node:test';
import assert from 'node:assert/strict';
import {focusDOM} from '../../shared/focus-dom.mjs';
import {KanbanDisclosure,projectKanban} from '../../../workspace/rail/kanban-group.js';
import {TaskNavigation} from '../../../workspace/rail/tasks.js';
import {compactTaskRow} from '../../../workspace/rail/task-row.js';

function fixture(t){
 const {body,element}=focusDOM(t),prototype=Object.getPrototypeOf(body);
 Object.defineProperty(prototype,'classList',{get(){return {toggle(){},add(){}};},configurable:true});
 const entry=(id,children=[],extra={})=>({task:{id,name:id,...extra},children,needsInput:false});
 const entries=[entry('active'),entry('plain'),entry('blocked'),entry('backlog'),entry('done',[entry('prompt',[],{needs_input:true}),entry('failed')])];
 const coordinatorMap=new Map(['active','blocked','backlog','done'].map(id=>[id,{root:true,kanban_status:id}]));
 const navigation={kanbanDisclosure:new KanbanDisclosure(),hierarchy:{attention:new Set(['prompt']),failed:new Set(['failed'])},taskItem(entry){const row=element('div');row.dataset.taskId=entry.task.id;return row;}};
 return {entries,coordinatorMap,navigation};
}

test('only deferred states have summaries and each hidden signal is counted once',t=>{
 const {entries,coordinatorMap,navigation}=fixture(t);
 const rows=projectKanban(navigation,'p',entries,'',coordinatorMap);
 assert.deepEqual(rows.filter(row=>row.dataset.taskId).map(row=>row.dataset.taskId),['active','blocked','plain']);
 const groups=rows.filter(row=>row.dataset.kanban);
 assert.deepEqual(groups.map(row=>row.dataset.kanban),['backlog','done']);
 assert.deepEqual(groups.map(row=>row.open),[false,false]);
 assert.equal(groups[1].children[0].getAttribute('aria-label'),'Done (1), 1 needs input, 1 needs attention task groups in p');
});

test('temporary search opens footers without changing saved disclosure',t=>{
 const {entries,coordinatorMap,navigation}=fixture(t);
 const rows=projectKanban(navigation,'p',entries,'done',coordinatorMap);
 assert.equal(rows.find(row=>row.dataset.kanban==='done').open,true);
 assert.equal(navigation.kanbanDisclosure.collapsed('p','done'),true);
});

test('unavailable row and role kinds use plain accessible copy',t=>{
 fixture(t);
 const row=compactTaskRow({role:{kind:'freelance',live:false},select(){}});
 assert.match(row.getAttribute('aria-label'),/^Task unavailable,/);
 assert.equal(row.querySelector('.task-role-tag').textContent,'Unavailable');
 assert.equal(row.querySelector('.task-role-tag').getAttribute('aria-label'),'Agent unavailable');
});

test('an idle session does not keep a stale working tag',t=>{
 fixture(t);
 const role={role_id:15,kind:'coordinator',live:true,status:'working'};
 const idle=compactTaskRow({task:{id:'c',name:'Coord',live:true,idle:true},role,select(){}});
 assert.equal(idle.querySelector('.task-role-tag').textContent,'idle');
 const busy=compactTaskRow({task:{id:'c',name:'Coord',live:true,idle:false},role,select(){}});
 assert.equal(busy.querySelector('.task-role-tag').textContent,'working');
});

test('missing task roster rows are disabled with stopped or ready tags',t=>{
 fixture(t);
 for (const ready of [false,true]) {
  const row=compactTaskRow({role:{live:true,status:'working',ready_to_close:ready},select(){}});
  assert.equal(row.disabled,true);
  assert.equal(row.querySelector('.task-role-tag').textContent,ready?'ready':'stopped');
 }
});
for (const reversed of [false,true]) test(`navigation ranks bindings with current task activity (reversed ${reversed})`,t=>{
 fixture(t);
 const task={id:'t',name:'Task',live:true,idle:true};
 const roles=[{role_id:1,live:true,task_id:'t',status:'working'},{role_id:2,live:true,task_id:'t',status:'done',ready_to_close:true}];
 const navigation=Object.create(TaskNavigation.prototype);
 Object.assign(navigation,{roster:{orchestrators:[{id:1,roles:reversed?[...roles].reverse():roles}]},renderKey:()=>'',renderedKey:''});
 const check=(id,status)=>{
  navigation.update([task],'t');
  const role=navigation.roles.get('t');
  assert.equal(role.role_id,id);
  assert.equal(compactTaskRow({task,role,select(){}}).querySelector('.task-role-tag').textContent,status);
 };
 check(2,'ready');task.idle=false;check(1,'working');task.idle=true;check(2,'ready');
});
