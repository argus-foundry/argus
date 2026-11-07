import {test} from 'node:test';
import assert from 'node:assert/strict';
import {focusDOM} from '../../shared/focus-dom.mjs';
import {TaskNavigation} from '../../../workspace/rail/tasks.js';
import {HeraKanban} from '../../../workspace/rail/hera-kanban.js';
import {KanbanDisclosure} from '../../../workspace/rail/kanban-group.js';
import {taskParents} from '../../../workspace/rail/task-hierarchy.js';

test('one coordinator snapshot serves every project and descendant per render with fresh optimistic state',t=>{
 const {body,element}=focusDOM(t), prototype=Object.getPrototypeOf(body);
 Object.defineProperty(prototype,'childNodes',{get(){return this.children;},configurable:true});
 Object.defineProperty(prototype,'classList',{get(){return {contains:name=>this.className.split(' ').includes(name),add:name=>{this.className+=' '+name;},toggle:()=>{}};},configurable:true});
 prototype.removeAttribute=function(key){delete this.attributes[key];};
 document.activeElement=null;
 document.getElementById=id=>body.children.find(item=>item.id===id);
 document.createDocumentFragment=()=>element('fragment');
 for(const id of ['task-groups','active-count','archive-count','active-filter','archive-filter','task-count']){const item=element('div');item.id=id;body.append(item);}
 const navigation=Object.create(TaskNavigation.prototype);
 const tasks=[['root','a'],['child','a'],['plain','a'],['other','b']].map(([id,project])=>({id,name:id,project,status:'complete'}));
 const roster={orchestrators:[{id:1,kanban_status:'done',pinned:false,roles:[{kind:'coordinator',live:true,task_id:'root'},{kind:'worker',live:true,task_id:'child'}]},{id:2,roles:[{kind:'coordinator',live:true,task_id:'other'}]}]};
 const kanban=new HeraKanban({},()=>{},()=>{});
 Object.assign(navigation,{tasks,roster,projects:[{name:'a'},{name:'b'}],actions:{heraKanban:kanban},roles:new Map(),hierarchy:taskParents(tasks,roster),pinned:new Set(),collapsed:new Set(),collapsedTasks:new Set(['root']),kanbanDisclosure:new KanbanDisclosure(),query:'',filter:'active'});
 let builds=0;
 navigation.coordinators=()=>{builds++;return TaskNavigation.prototype.coordinators.call(navigation);};
 navigation.render();
 assert.equal(body.querySelectorAll('.task-row').length,4);
 assert.equal(builds,1);
 assert.equal(body.querySelectorAll('.task-pin').length,0);
 kanban.overrides.set(1,{pinned:true,kanban_status:'blocked'});
 builds=0;navigation.render();
 assert.equal(builds,1);
 assert.equal(body.querySelectorAll('.task-pin').length,1);
 assert.equal(body.querySelectorAll('.kanban-group').some(group=>group.dataset.kanban==='blocked'),false);
 assert.equal(body.querySelectorAll('.task-triage')[0].textContent,'Blocked');
 navigation.selection={active:true,ids:new Set(),checkbox:()=>element('input'),projectCheckbox:()=>element('input'),prune(){}};
 navigation.render();
 assert.equal(body.querySelectorAll('.task-triage')[0]?.textContent,'Blocked');
});
