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

test('pin shortcut uses live root and nested coordinator bindings and ordinary task pins',async t=>{
 const {TaskNavigation}=await import('../../../workspace/rail/tasks.js');
 const {HeraKanban}=await import('../../../workspace/rail/hera-kanban.js');
 const {RailActions}=await import('../../../workspace/rail/rail-actions.js');
 t.mock.timers.enable({apis:['setTimeout']});
 globalThis.document={getElementById:()=>({textContent:'',hidden:false})};
 for(const kind of ['root','nested','ordinary','stale','archived','failure'])await t.test(kind,async()=>{
  const currentTask={id:'selected',name:'Selected',pinned:false,archived:kind==='archived'},requests=[],messages=[];
  const api={json:async(path,options)=>{requests.push([path,options?.method]);if(kind==='failure')throw new Error('Denied');return {...currentTask};}};
  const navigation=Object.create(TaskNavigation.prototype);
  const heraKanban=new HeraKanban(api,()=>{},async()=>{},message=>messages.push(message));
  navigation.actions={heraKanban};
  navigation.roster={orchestrators:[{id:7,pinned:kind==='nested',bridge_parent_orch_id:kind==='nested'?1:undefined,roles:[{kind:'coordinator',live:kind!=='stale',task_id:kind==='ordinary'?'other':'selected'}]}]};
  const railActions=new RailActions(api,async()=>{});
  await workspaceShortcutAction('pin',{state:()=>({currentTask,tasks:[currentTask]}),navigation,railActions});
  if(kind==='archived'){
   assert.deepEqual(requests,[['/api/tasks/selected/raw',undefined]]);assert.equal(railActions.pinned.has('selected'),false);
  }else if(['ordinary','stale'].includes(kind)){
   assert.deepEqual(requests,[['/api/tasks/selected/raw',undefined],['/api/tasks/selected/raw','PUT']]);assert.equal(railActions.pinned.has('selected'),true);
  }else{
   assert.deepEqual(requests,[[`/api/hera/orchestrators/7/${kind==='nested'?'unpin':'pin'}`,'POST']]);
   assert.match(messages[0],kind==='failure'?/not confirmed.*Denied/:/Task (?:un)?pinned/);
   assert.equal(heraKanban.pending.size,0);assert.equal(heraKanban.overrides.size,0);
   assert.equal(navigation.coordinators().get('selected').pinned,kind==='nested');
  }
  railActions.clear();
 });
});
