import {test} from 'node:test';import assert from 'node:assert/strict';import {workspaceShortcutAction} from '../../../workspace/shortcuts/workspace-shortcut-actions.js';
test('late shortcut read cannot fork or delete a newly selected task',async()=>{
 for(const action of ['fork','delete']){let currentTask={id:'a'},release;const mutations=[];const taskSettings={active:true,task:{id:'a'},open:()=>new Promise(resolve=>release=resolve),confirm:()=>mutations.push('delete'),mutate:()=>mutations.push('fork')};
  const pending=workspaceShortcutAction(action,{state:()=>({currentTask,tasks:[]}),taskSettings});currentTask={id:'b'};taskSettings.task={id:'b'};release(true);await pending;assert.deepEqual(mutations,[]);
 }
});
test('terminal previous detail from Agent reaches Info equivalent',async()=>{
 const selected=[];globalThis.document={activeElement:null,getElementById:id=>({hidden:false,focus:()=>selected.push(id)})};
 await workspaceShortcutAction('tab-previous',{state:()=>({tasks:[]}),layout:{visible:{tools:true}},tools:{selectTab:id=>selected.push(id)}});
 assert.deepEqual(selected,['tool-status','tool-status']);
});
test('task jumps use rail filters and previous starts at the final matching task',async()=>{
 const tasks=[{id:'a',needs_input:true},{id:'b',needs_input:true},{id:'c'}],selected=[];
 const dependencies={state:()=>({tasks,currentTask:{id:'c'}}),navigation:{filteredTasks:()=>[tasks[1]],hierarchy:{attention:new Set()}},selectTask:task=>selected.push(task.id)};
 for(const action of ['previous','next','attention'])await workspaceShortcutAction(action,dependencies);
 assert.deepEqual(selected,['b','b','b']);
 dependencies.navigation.filteredTasks=()=>[];await workspaceShortcutAction('attention',dependencies);assert.equal(selected.length,3);
});
test('surface shortcuts honor the Settings leave refusal before any destination effects',async()=>{
 for(const action of ['new','rename','fork','delete','archive','split','filter','previous','next','attention','agent','review','files','info','tab-next','tab-previous']){
  const calls=[];
  await workspaceShortcutAction(action,{state:()=>({tasks:[],currentTask:{id:'a'}}),settings:{leaveForTask:()=>{calls.push('leave');return false;}}});
  assert.deepEqual(calls,['leave'],action);
 }
});
test('new task releases Settings then task settings before opening creation',async()=>{
 const calls=[];
 await workspaceShortcutAction('new',{state:()=>({tasks:[],currentTask:{id:'a',project:'demo'}}),
  settings:{leaveForTask:()=>{calls.push('settings');return true;}},taskSettings:{leaveForTask:()=>calls.push('task-settings')},
  newTask:{open:project=>calls.push(project)},composer:{focus:()=>{}},navigation:{projects:[]}});
 assert.deepEqual(calls,['settings','task-settings','demo']);
});
