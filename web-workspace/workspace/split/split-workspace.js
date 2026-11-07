import {$,node,button,preference,savePreference,notice} from '../../shared/dom.js';
import {coordinatorContext,secondaryTask} from './split-policy.js';
import {compactTaskRow} from '../rail/task-row.js';
import {log} from '../../core/api.js';

export class SplitWorkspace {
  constructor(cache) {
    Object.assign(this,{cache,enabled:preference('split-view',false)===true,visible:true,tasks:[],roster:{},choices:new Map(),workers:new Map()});
    this.toggleButton=button('',()=>this.toggle(),'header-icon');this.toggleButton.id='split-toggle';this.toggleButton.disabled=true;
    this.toggleButton.setAttribute('aria-label','Split view');this.toggleButton.setAttribute('aria-controls','secondary-pane');
    this.toggleButton.innerHTML='<svg viewBox="0 0 24 24" aria-hidden="true"><rect x="3" y="4" width="18" height="16" rx="2"/><path d="M12 4v16"/></svg>';
    $('fit-session').before(this.toggleButton);
    this.layout=node('div','split-terminals');$('terminal').before(this.layout);this.layout.append($('terminal'));
    this.pane=node('section','secondary-pane');this.pane.id='secondary-pane';this.pane.setAttribute('aria-label','Second task terminal');
    const toolbar=node('div','secondary-toolbar'),label=node('label','','Alongside');label.htmlFor='secondary-task-picker';
    this.picker=node('select');this.picker.id='secondary-task-picker';this.picker.setAttribute('aria-label','Second task');
    this.picker.addEventListener('change',()=>this.choose(this.picker.value));
    this.status=node('span','secondary-state','Choose another task');this.status.id='secondary-terminal-state';this.status.setAttribute('role','status');
    this.reconnect=button('Reconnect',()=>this.cache()?.secondaryTerminal?.reconnect());this.reconnect.hidden=true;this.reconnect.dataset.tooltip='Reconnect terminal output';this.reconnect.setAttribute('aria-label','Reconnect secondary terminal');
    this.fit=button('Fit session',async()=>{try{await this.cache()?.secondaryTerminal?.fitSession();}catch(error){notice(error.message);log('split','fit failed');}});this.fit.disabled=true;this.fit.dataset.tooltip='Fit session to the secondary pane';this.fit.setAttribute('aria-label','Fit secondary session');
    toolbar.append(label,this.picker,this.status,this.reconnect,this.fit);
    this.rosterDetails=node('details','split-roster');this.rosterDetails.id='split-roster';this.rosterSummary=node('summary','','Team tasks');this.rosterRows=node('div','split-roster-rows');this.rosterDetails.append(this.rosterSummary,this.rosterRows);
    this.rosterDetails.addEventListener('toggle',()=>{if(this.rosterDetails.open&&this.rosterDirty)this.renderRoster();});
    this.host=node('div','secondary-terminal');this.host.id='secondary-terminal';
    this.empty=node('p','empty-state','Choose any other task to show alongside this one.');
    this.pane.append(toolbar,this.host,this.empty);this.layout.append(this.pane);this.render();
  }
  beforeSelect(task){if(this.cache()?.secondary?.id===task?.id)this.cache().releaseSecondary();}
  select(task,tasks,roster){
    this.task=task;this.tasks=tasks;this.roster=roster||{};
    for(const orch of this.roster.orchestrators||[])if((orch.roles||[]).some(role=>role.kind==='worker'&&role.live&&role.task_id===task?.id)){
      const coordinator=orch.roles.find(role=>role.kind==='coordinator'&&role.live);if(coordinator)this.workers.set(coordinator.task_id,task.id);
    }
    this.render();
  }
  update(tasks,roster=this.roster){this.select(this.task&&tasks.find(task=>task.id===this.task.id),tasks,roster);}
  setVisible(visible){visible=!!visible;if(this.visible===visible)return;this.visible=visible;this.render();}
  toggle(){if(!this.task)return;const terminalFocused=!!document.activeElement?.closest('.xterm');this.enabled=!this.enabled;savePreference('split-view',this.enabled);this.render();if(!this.enabled&&terminalFocused)this.cache()?.term?.focus();log('split','toggled',{status:this.enabled});}
  choose(id){
    if(!this.task)return;
    this.choices.set(this.task.id,id||null);
    const coordinator=coordinatorContext(this.task.id,this.roster);
    if(coordinator?.roles.some(role=>role.kind==='worker'&&role.live&&role.task_id===id))this.workers.set(this.task.id,id);
    this.render();log('split','task selected');
  }
  render(){
    const active=this.enabled&&!!this.task;
    this.toggleButton.disabled=!this.task;this.toggleButton.setAttribute('aria-pressed',String(this.enabled));
    this.toggleButton.dataset.tooltip=this.enabled?'Close split view':'Split view';
    this.toggleButton.dataset.tooltipShortcut='⌥⇧D';
    this.pane.hidden=!active;$('agent-panel').classList.toggle('split-open',active);
    if(!active||!this.visible){this.cache()?.releaseSecondary();return;}
    const explicit=this.choices.has(this.task.id),chosen=this.choices.get(this.task.id);
    const id=explicit&&chosen===null?null:secondaryTask(this.task.id,this.tasks,this.roster,chosen,this.workers.get(this.task.id));
    const second=this.tasks.find(task=>task.id===id);
    const focus=document.activeElement===this.picker;
    const options=this.tasks.filter(task=>task.id!==this.task.id).map(task=>[task.id,`${task.project||'Other tasks'} / ${task.name}${task.archived?' · archived':''}`]);
    const signature=JSON.stringify(options);
    if(signature!==this.optionsSignature){
      this.optionsSignature=signature;this.picker.replaceChildren();const none=node('option','','Choose another task');none.value='';this.picker.append(none);
      for(const [id,text] of options){const option=node('option','',text);option.value=id;this.picker.append(option);}
    }
    this.picker.dataset.tooltipOverflow='';this.picker.value=second?.id||'';if(second)this.picker.dataset.tooltip=this.picker.selectedOptions[0].textContent;else delete this.picker.dataset.tooltip;if(focus)this.picker.focus({preventScroll:true});
    this.scheduleRosterRender();this.empty.hidden=!!second;
    if(!second){this.cache()?.releaseSecondary();this.host.replaceChildren();this.status.textContent='Choose another task';this.fit.disabled=true;this.reconnect.hidden=true;return;}
    this.cache()?.attachSecondary(second,this.host,state=>{this.status.textContent=state.message;this.fit.disabled=!state.interactive;this.reconnect.hidden=!['error','warning'].includes(state.kind);});
  }
  rosterEntries(coordinator){
    return [...(coordinator?.roles||[]),...(this.roster.freelance||[]).filter(role=>role.orch_id===coordinator?.id)].map(role=>({role,task:role.live?this.tasks.find(task=>task.id===role.task_id):null}));
  }
  rosterRenderKey(coordinator){
    return JSON.stringify([coordinator?.id,coordinator?.name,this.task?.id,this.picker.value,
      this.rosterEntries(coordinator).map(({role,task})=>[
        role.role_id,role.name,role.kind,role.live,role.status,role.ready_to_close,role.archived,role.needs_input,
        task?.id,task?.name,task?.status,task?.archived,task?.archive_pending,task?.needs_input,task?.live,task?.idle,task?.pr_state,
      ]),
    ]);
  }
  scheduleRosterRender(){
    const coordinator=coordinatorContext(this.task.id,this.roster);
    if(!coordinator){this.rosterDetails.remove();this.rosterRenderedKey=null;return;}
    if(this.rosterDetails.parentNode!==this.pane)this.pane.insertBefore(this.rosterDetails,this.host);
    this.rosterSummary.textContent=coordinator.name?`${coordinator.name} tasks`:'Team tasks';
    this.rosterDirty=this.rosterRenderKey(coordinator)!==this.rosterRenderedKey;
    if(!this.rosterDirty||!this.rosterDetails.open||this.rosterFrame!=null)return;
    this.rosterFrame=requestAnimationFrame(()=>{this.rosterFrame=null;this.renderRoster();});
  }
  renderRoster(){
    if(this.rosterFrame!=null){cancelAnimationFrame(this.rosterFrame);this.rosterFrame=null;}
    if(!this.enabled||!this.visible||!this.task||!this.rosterDetails.open)return;
    const coordinator=coordinatorContext(this.task.id,this.roster);
    if(!coordinator)return;
    const key=this.rosterRenderKey(coordinator);
    if(key===this.rosterRenderedKey){this.rosterDirty=false;return;}
    this.rosterRenderedKey=key;this.rosterDirty=false;
    const focused=this.rosterRows.contains(document.activeElement)?document.activeElement.dataset.roleId:null;
    this.rosterRows.replaceChildren();
    const entries=this.rosterEntries(coordinator);
    for(const {role,task} of entries){
      const row=compactTaskRow({task,role,selected:this.picker.value,needsInput:!task?.archived&&(task?.needs_input||role.needs_input||role.status==='blocked'),select:selected=>{const current=this.tasks.find(task=>task.id===selected.id);if(current)this.choose(current.id);}});
      if(task?.id===this.task.id)row.disabled=true;this.rosterRows.append(row);if(focused===String(role.role_id)&&!row.disabled)row.focus({preventScroll:true});
    }
    log('split','roster rendered',{count:entries.length});
  }
  terminalFor(target){return this.cache()?.terminalFor(target);}
  clear(){if(this.rosterFrame!=null){cancelAnimationFrame(this.rosterFrame);this.rosterFrame=null;}this.rosterRenderedKey=null;this.rosterDirty=false;this.cache()?.releaseSecondary();this.task=null;this.tasks=[];this.roster={};this.choices.clear();this.workers.clear();this.rosterRows.replaceChildren();this.rosterDetails.open=false;this.rosterDetails.remove();this.rosterSummary.textContent='Team tasks';this.picker.replaceChildren();this.optionsSignature=null;this.host.replaceChildren();this.status.textContent='Choose another task';this.render();}
}
