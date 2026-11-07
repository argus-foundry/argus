import {test} from 'node:test';
import assert from 'node:assert/strict';
import {focusDOM} from '../../shared/focus-dom.mjs';
import {KanbanDisclosure,projectKanban} from '../../../workspace/rail/kanban-group.js';
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
