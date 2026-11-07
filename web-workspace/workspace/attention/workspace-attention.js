import {node,button} from '../../shared/dom.js';
import {attentionPreferences} from './attention-preferences.js';
import {AttentionPolicy} from './attention-policy.js';
import {AttentionTab} from './attention-tab.js';
import {log} from '../../core/api.js';
export class WorkspaceAttention {
 constructor(select){
  const saved=attentionPreferences.get();
  Object.assign(this,{select,policy:new AttentionPolicy(),tasks:[],selected:null,idle:saved.idle===true,enabled:saved.enabled!==false});
  this.unsubscribePreferences=attentionPreferences.subscribe(value=>{this.enabled=value.enabled;this.idle=value.idle;});
  this.alerts=node('div','attention-alerts');this.alerts.id='attention-alerts';this.alerts.setAttribute('aria-live','polite');document.body.append(this.alerts);
  this.tab=new AttentionTab();this.update([]);
 }
 update(tasks,selected=this.selected){
  this.tasks=tasks;this.selected=selected;const waiting=tasks.filter(task=>!task.archived&&task.needs_input),ids=new Set(waiting.map(task=>task.id));this.policy.reconcile(ids);
  for(const alert of this.alerts.querySelectorAll('[data-attention-task]'))if(!ids.has(alert.dataset.attentionTask))alert.remove();
  const count=waiting.length;
  this.tab.update(count);
  if(!count){this.summary?.remove();this.summary=null;}

 }
 event(event){
  const task=this.tasks.find(item=>item.id===event.task_id);
  if(!task||task.archived||task.id===this.selected&&!document.hidden)return;
  if(event.type==='session.needs_input'&&event.payload?.needs_input&&this.enabled){const decision=this.policy.arrive(task.id);if(!decision){log('attention','duplicate skipped');return;}this.notify(decision);}
  else if(event.type==='session.idle'&&this.idle)this.notify({kind:'individual',ids:[task.id],idle:true});
 }
 notify(decision){
  const tasks=decision.ids.map(id=>this.tasks.find(task=>task.id===id)).filter(Boolean),summary=decision.kind==='summary';
  const text=summary?`${tasks.length} more tasks need input · ${tasks.map(task=>task.name).join(', ')}`:`${tasks[0]?.name||'Task'} ${decision.idle?'is idle':'needs input'}`;
  let alert=summary&&this.summary?.isConnected?this.summary:null;
  if(!alert){alert=node('div','attention-alert');if(summary)this.summary=alert;else alert.dataset.attentionTask=tasks[0]?.id;this.alerts.append(alert);}
  alert.replaceChildren(button(text,()=>{if(tasks[0])this.select(tasks[0]);alert.remove();}),button('Dismiss',()=>alert.remove()));
  clearTimeout(this.alertTimer);this.alertTimer=setTimeout(()=>{this.alerts.replaceChildren();this.summary=null;},10000);
  log('attention',summary?'burst coalesced':'alert posted',{count:tasks.length});
 }
 destroy(){this.clear();this.unsubscribePreferences();}
 clear(){clearTimeout(this.alertTimer);this.policy=new AttentionPolicy();this.alerts.replaceChildren();this.summary=null;this.update([]);}
}
