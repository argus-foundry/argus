import test from 'node:test';
import assert from 'node:assert/strict';
import {isInteractive} from '../../terminal/terminal-policy.js';
import {taskIconState} from '../../workspace/rail/task-status-icon.js';
import {taskParents,taskForest} from '../../workspace/rail/task-hierarchy.js';
import {patchEvent} from '../../workspace/events/workspace-events-policy.js';
for(const status of ['in_review','complete','pending'])test(`${status} preserves authoritative live input and busy indication`,()=>{
 const task={id:'a',status,live:true,idle:false};assert.equal(isInteractive(task),true);assert.equal(taskIconState(task).kind,'working');assert.equal(isInteractive({...task,archived:true}),false);assert.equal(isInteractive({...task,archive_pending:true}),false);assert.equal(isInteractive({...task,live:false}),false);
});
for(const [label,before,payload,expected] of [
 ['busy to idle',{live:true,idle:false,needs_input:false},{live:true,idle:true,needs_input:false},{live:true,idle:true,needs_input:false}],
 ['input needed',{live:true,idle:true,needs_input:false},{live:true,idle:true,needs_input:true},{live:true,idle:true,needs_input:true}],
 ['clears input and idle',{live:true,idle:true,needs_input:true},{live:true,idle:false,needs_input:false},{live:true,idle:false,needs_input:false}],
 ['session disappears',{live:true,idle:true,needs_input:true},{live:false,idle:false,needs_input:false},{live:false,idle:false,needs_input:false}],
 ['absent session cannot idle',{live:true,idle:true,needs_input:true},{live:false,idle:true,needs_input:false},{live:false,idle:false,needs_input:false}],
 ['independent input signal',{live:false,idle:false,needs_input:false},{live:false,idle:true,needs_input:true},{live:false,idle:false,needs_input:true}],
])test(`runtime events reconcile ${label}`,()=>{
 const untouched={id:'b',live:true,idle:false,needs_input:false};
 const result=patchEvent([{id:'a',status:'complete',...before},untouched],{type:'session.runtime_changed',task_id:'a',payload});
 assert.deepEqual(result.tasks,[{id:'a',status:'complete',...expected},untouched]);assert.equal(result.resnapshot,true);
});
test('standalone failure joins hierarchy and clears without parsing Result',()=>{
 let tasks=[{id:'a',failure:{reason:'validation failed',source:'task_result'}}];
 assert.equal(taskForest(tasks,taskParents(tasks))[0].failed,true);
 tasks=patchEvent(tasks,{type:'task.failure_changed',task_id:'a',payload:{failure:null}}).tasks;
 assert.equal(tasks[0].failure,null);assert.equal(taskForest(tasks,taskParents(tasks))[0].failed,false);
 const roster={freelance:[{role_id:1,live:true,task_id:'a',status:'failed'}]};assert.equal(taskForest(tasks,taskParents(tasks,roster))[0].failed,true);
});

test('older daemon readiness retains workflow fallback and missing idle is busy',()=>{assert.equal(isInteractive({status:'in_progress'}),true);assert.equal(isInteractive({status:'complete'}),false);assert.equal(isInteractive({status:'in_progress',idle:true}),false);assert.equal(taskIconState({status:'complete',live:true}).kind,'working');});
