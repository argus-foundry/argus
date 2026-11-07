import {test} from 'node:test';
import assert from 'node:assert/strict';
import {focusDOM} from '../../shared/focus-dom.mjs';
import {taskParents,taskForest} from '../../../workspace/rail/task-hierarchy.js';
import {projectGroup} from '../../../workspace/rail/project-group.js';
import {KanbanDisclosure,projectKanban} from '../../../workspace/rail/kanban-group.js';

test('failed-only deferred descendants roll attention up through collapsed projects without input',t=>{
 const {body,element}=focusDOM(t),prototype=Object.getPrototypeOf(body);
 Object.defineProperty(prototype,'classList',{configurable:true,get(){return {toggle:(name,on)=>{const names=new Set(this.className.split(' ').filter(Boolean));if(on)names.add(name);else names.delete(name);this.className=[...names].join(' ');}};}});
 const tasks=[{id:'root',name:'Root'},{id:'child',name:'Child'}];
 const roster={orchestrators:[{id:1,roles:[{kind:'coordinator',task_id:'root',live:true},{kind:'worker',task_id:'child',live:true,status:'failed'}]}]};
 const hierarchy=taskParents(tasks,roster),entries=taskForest(tasks,hierarchy);
 const navigation={hierarchy,kanbanDisclosure:new KanbanDisclosure(),taskItem:()=>element('div')};
 const coordinators=new Map([['root',{root:true,kanban_status:'backlog'}]]);
 const group=projectGroup('p',entries,{collapsed:true,renderEntries:items=>projectKanban(navigation,'p',items,'',coordinators)});
 assert.equal(entries[0].needsInput,false);
 assert.equal(entries[0].failed,true);
 assert.equal(group.open,false);
 assert.match(group.className,/needs-attention/);
 assert.match(group.children[0].className,/needs-attention/);
 const footer=group.querySelector('.kanban-group');assert.equal(footer.open,false);
 assert.equal(footer.children[0].getAttribute('aria-label'),'Backlog (1), 1 needs attention task groups in p');
});

test('archived failed descendants do not roll up project failure',()=>{
 const hierarchy={parents:new Map([['child','root']]),attention:new Set(),failed:new Set(['child'])};
 const entries=taskForest([{id:'root'},{id:'child',archived:true}],hierarchy);
 assert.equal(entries[0].needsInput,false);assert.equal(entries[0].failed,false);
});
