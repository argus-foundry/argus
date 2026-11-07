import {test} from 'node:test';
import assert from 'node:assert/strict';
import {focusDOM} from '../../shared/focus-dom.mjs';
import {TaskSelection} from '../../../workspace/rail/task-selection.js';
for(const count of [140,500,1000])test(`${count} checkbox construction makes one matching pass and linear ancestry reads`,t=>{
 focusDOM(t);let filters=0,gets=0;const parents=new Map(Array.from({length:count-1},(_,i)=>[String(i+1),'0']));const get=parents.get.bind(parents);parents.get=id=>{gets++;return get(id);};
 const tasks=Array.from({length:count},(_,i)=>({id:String(i),name:String(i)})),selection=Object.create(TaskSelection.prototype);
 Object.assign(selection,{ids:new Set(['0','1']),navigation:{tasks,filter:'active',query:'',hierarchy:{parents},filteredTasks(){filters++;return tasks;}}});
 for(const task of tasks)selection.checkbox(task);
 assert.equal(filters,1);assert.ok(gets<=count*4,`${gets} ancestry reads`);
});
