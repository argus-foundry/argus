import {captureFocus} from '../../shared/focus-recovery.js';
import {$,node,button,notice} from '../../shared/dom.js';
import {log} from '../../core/api.js';
import {selectionIndex,subtreeScope} from './task-selection-policy.js';

const noParents=new Map();
export class TaskSelection {
 constructor(navigation,run){
  Object.assign(this,{navigation,run,ids:new Set(),active:false});
  this.start=button('Select',()=>this.enter());this.start.id='rail-select';this.start.setAttribute('aria-label','Select tasks');
  this.action=button('Archive',()=>this.run(this.tasks()));this.action.id='rail-bulk-action';
  this.cancel=button('Cancel',()=>this.exit());this.cancel.setAttribute('aria-label','Cancel task selection');
  this.all=this.scopeCheckbox('Select all matching tasks',()=>this.scope(),'rail-select-all');
  this.controls=node('span','rail-selection-actions');this.controls.append(this.all,this.start,this.action,this.cancel);
  $('task-count').parentElement.prepend(this.all);$('task-count').parentElement.append(this.controls);$('task-count').setAttribute('role','status');$('task-count').setAttribute('aria-live','polite');
  this.alert=node('p','error-text');this.alert.id='bulk-alert';this.alert.setAttribute('role','alert');this.alert.hidden=true;$('task-count').parentElement.after(this.alert);
  $('task-groups').addEventListener('keydown',event=>this.keydown(event));
  document.getElementById('navigation').addEventListener('keydown',event=>{
   if(event.key==='Escape'&&this.active&&!event.defaultPrevented&&!document.querySelector('dialog[open]')){event.preventDefault();event.stopPropagation();this.exit();}
  });this.header();
 }
 membership(){
  const nav=this.navigation,parents=nav.hierarchy?.parents||noParents,key=[nav.tasks,parents,nav.filter,nav.query,this.version];
  if(!this.cache||key.some((value,index)=>value!==this.cache.key[index])){
   const available=nav.tasks.filter(task=>!!task.archived===(nav.filter==='archive')),matches=nav.filteredTasks?nav.filteredTasks():available;
   this.cache={key,available,availableIds:new Set(available.map(task=>task.id)),matches,...selectionIndex(matches,parents,this.ids)};
  }return this.cache;
 }
 available(){return this.membership().available;}
 scope(){return this.membership().matches;}
 tasks(){return this.available().filter(task=>this.ids.has(task.id));}
 subtree(task){return subtreeScope(task.id,this.scope(),this.navigation.hierarchy.parents);}
 state(control,{total=0,selected=0}={}){const checked=total>0&&total===selected,mixed=selected>0&&selected<total;control.checked=checked;control.indeterminate=mixed;control.setAttribute('aria-checked',mixed?'mixed':String(checked));}
 scopeCheckbox(label,scope,className){
  const control=node('input',className);control.type='checkbox';control.setAttribute('aria-label',label);
  control.addEventListener('click',event=>event.stopPropagation());control.addEventListener('change',()=>this.set(scope(),control.checked));return control;
 }
 projectCheckbox(project){const scope=()=>this.scope().filter(task=>(task.project||'Other tasks')===project),control=this.scopeCheckbox(`Select project ${project}`,scope,'project-action project-select');
  control.dataset.project=project;control.dataset.projectAction='select';const counts=this.membership().projects.get(project);control.disabled=!!this.busy||!counts?.total;this.state(control,counts);return control;
 }
 visible(){return [...$('task-groups').querySelectorAll('.task-select')].filter(control=>{
  for(let parent=control.parentElement;parent;parent=parent.parentElement)if(parent.tagName==='DETAILS'&&!parent.open&&!parent.querySelector('summary')?.contains(control))return false;
  return control.getClientRects().length;
 });}
 enter(){this.active=true;this.ids.clear();this.version=(this.version||0)+1;this.alert.hidden=true;this.navigation.menuTask=null;this.navigation.render();(this.visible()[0]||this.cancel).focus();log('task-selection','entered');}
 exit(focus=true){if(this.busy)return;this.active=false;this.ids.clear();this.version=(this.version||0)+1;this.alert.hidden=true;this.navigation.render();if(focus)this.start.focus();log('task-selection','exited');}
 set(tasks,checked){if(this.busy)return;for(const task of tasks)if(checked)this.ids.add(task.id);else this.ids.delete(task.id);this.version=(this.version||0)+1;this.navigation.render();log('task-selection','changed',{count:this.ids.size});}
 toggle(task){this.set([task],!this.ids.has(task.id));}
 checkbox(task){
  const control=this.scopeCheckbox(`Select ${task.name}`,()=>this.subtree(task),'task-control task-select');control.disabled=!!this.busy;
  control.dataset.taskId=task.id;control.dataset.taskAction='select';this.state(control,this.membership().subtrees.get(task.id));return control;
 }
 watchScope(callback){(this.scopeListeners??=new Set()).add(callback);return ()=>this.scopeListeners.delete(callback);}
 prune(){const available=this.membership().availableIds,removed=[];for(const id of this.ids)if(!available.has(id)){this.ids.delete(id);removed.push(id);}if(removed.length)this.version=(this.version||0)+1;this.header();if(removed.length)for(const callback of this.scopeListeners||[])callback(removed);}
 header(){
  const restore=captureFocus(this.controls.parentElement,()=>$(this.navigation.filter==='archive'?'archive-filter':'active-filter'),{neighbors:false});
  this.start.hidden=this.active;this.all.hidden=!this.active;this.action.hidden=!this.active;this.cancel.hidden=!this.active;
  const deleting=this.navigation.filter==='archive';this.action.textContent=deleting?'Delete':'Archive';this.action.classList.toggle('danger',deleting);this.action.setAttribute('aria-label',`${deleting?'Delete':'Archive'} selected tasks`);
  this.action.disabled=!!this.busy||!this.ids.size;this.cancel.disabled=!!this.busy;this.all.disabled=!!this.busy||!this.scope().length;this.start.disabled=!this.scope().length;
  this.state(this.all,this.membership());
  if(this.active){const hidden=this.ids.size-this.membership().selected;$('task-count').textContent=`${this.ids.size} selected${hidden?`, ${hidden} not shown`:''}`;}
  restore();
 }
 finish(result){
  this.busy=false;this.ids=new Set([...result.failed.map(item=>item.id),...result.notStarted]);this.version=(this.version||0)+1;
  if(!this.ids.size&&!result.skipped?.length){this.active=false;this.alert.hidden=true;notice(`${result.succeeded.length} tasks updated.`);}
  else{if(!this.ids.size)this.active=false;const failures=result.failed.map(item=>`${this.navigation.tasks.find(task=>task.id===item.id)?.name||item.id}: ${item.error?.message||'Action failed'}`).join('; ');this.alert.textContent=`${result.succeeded.length} succeeded, ${result.failed.length} failed, ${result.notStarted.length} not started. ${failures}${result.skipped?.length?` ${result.skipped.length} skipped because their reviewed scope changed.`:''} Check task state before an explicit retry.`;this.alert.hidden=false;}
  this.navigation.render();
 }
 keydown(event){
  if(!this.active||event.defaultPrevented||!['ArrowDown','ArrowUp'].includes(event.key))return;
  if(!event.target.matches('.task-select,.task-row'))return;
  const controls=this.visible(),index=controls.findIndex(control=>control.dataset.taskId===event.target.dataset.taskId),next=controls[index+(event.key==='ArrowDown'?1:-1)];if(next){event.preventDefault();event.stopPropagation();next.focus();}
 }
 clear(){this.busy=false;this.active=false;this.ids.clear();this.version=(this.version||0)+1;this.alert.hidden=true;this.header();}
}
