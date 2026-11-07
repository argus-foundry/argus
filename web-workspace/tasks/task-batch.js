import {confirmDialog} from '../shared/confirm-dialog.js';
import {log} from '../core/api.js';

export class TaskBatch {
 constructor(api,selection,deletion,onSuccess,onChanged){Object.assign(this,{api,selection,deletion,onSuccess,onChanged});}
 get deletingIds(){return this.busy&&this.action==='delete'?this.targets:new Set();}
 open(tasks){
  if(!tasks.length||this.busy)return;
  this.abort?.abort();this.abort=new AbortController();
  const signal=this.abort.signal,action=this.selection.navigation.filter==='archive'?'delete':'archive',captured=tasks.map(task=>({...task}));
  this.action=action;this.targets=new Set(captured.map(task=>task.id));
  const inScope=task=>this.selection.navigation.filter===(action==='delete'?'archive':'active')&&this.selection.membership().availableIds.has(task.id)&&this.selection.ids.has(task.id);
  let outcome;const label=action==='delete'?'Delete':'Archive';
  const result=confirmDialog({id:'bulk-dialog',title:action==='delete'?`Delete ${captured.length} tasks permanently`:`Archive ${captured.length} tasks?`,message:action==='delete'?'This cannot be undone. Task data, sessions and worktrees will be removed.':'Archiving clears intertask messages and removes task pins. Agents stop later when idle. Worktrees, team attachments and the open draft remain.',tasks:captured,taskAction:action,roster:this.deletion.roster(),links:this.deletion.links(),confirmLabel:`${label} ${captured.length} tasks`,signal,request:(path,options)=>this.api.json(path,options),validate:()=>captured.every(inScope),canDispatch:inScope,onBusy:busy=>{
   if(this.abort?.signal===signal){this.busy=busy;this.selection.busy=busy;this.selection.header();}
  },onTaskSuccess:task=>{if(!signal.aborted)this.onSuccess(task,action);},onComplete:async result=>{
   outcome=result;if(signal.aborted)return;
   this.selection.finish(result);await this.onChanged();log('task-batch','completed',{count:result.succeeded.length,status:!result.failed.length});
  },restoreFocus:()=>{
   if(signal.aborted)return;
   const selection=this.selection;
   if(!outcome&&selection.active){selection.action.focus({preventScroll:true});return;}
   if(selection.active)(selection.visible().find(control=>selection.ids.has(control.dataset.taskId))||selection.cancel).focus({preventScroll:true});
   else{const row=[...document.querySelectorAll('#task-groups .task-row')].find(item=>item.getClientRects().length);(row||document.getElementById(selection.navigation.filter==='archive'?'archive-filter':'active-filter')).focus({preventScroll:true});}
  }});
  const unwatch=this.selection.watchScope(()=>{
   if(signal.aborted||this.busy||captured.every(inScope))return;
   this.abort.abort();this.selection.alert.textContent='The reviewed task scope changed. Review the current selection before continuing.';this.selection.alert.hidden=false;
   queueMicrotask(()=>{if(this.selection.active)(this.selection.action.disabled?this.selection.cancel:this.selection.action).focus({preventScroll:true});});
   log('task-batch','review invalidated',{status:false});
  });
  result.then(()=>{unwatch();if(this.abort?.signal===signal)this.abort.abort();});return result;
 }
 clear(){this.abort?.abort();this.busy=false;}
}
