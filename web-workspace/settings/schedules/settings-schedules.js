import {captureFocus} from '../../shared/focus-recovery.js';
import {confirmDialog} from '../../shared/confirm-dialog.js';
import {node,button,errorMessage} from '../../shared/dom.js';
import {log} from '../../core/api.js';
import {settingsSignal} from '../settings-fields.js';
import {schedulePayload} from './settings-schedule-policy.js';
import {scheduleEditor} from './settings-schedule-editor.js';
const when=value=>value && Number.isFinite(Date.parse(value))?new Date(value).toLocaleString():'Pending';

export class ScheduleSettings {
  constructor(context) {
    Object.assign(this,context,{abort:new AbortController(),version:0,pending:false,rows:[],projects:[],backends:[]});
    const header=node('div','settings-section-heading');
    this.newButton=button('New schedule',()=>this.edit());
    header.append(node('h2','','Schedules'),this.newButton,button('Refresh schedules',()=>this.load()));
    this.list=node('div','settings-schedule-list');this.editor=node('div');
    this.host.replaceChildren(header,node('p','settings-note','Each fire creates a fresh task and worktree in the selected project.'),this.list,this.editor);this.load();
  }
  lock(value) {
    this.pending=value;this.busy(value);
    for(const control of this.host.querySelectorAll('button,fieldset'))control.disabled=value;
    this.newButton.disabled=value || this.loading || !this.projects.length;
  }
  async load() {
    this.read?.abort();this.read=new AbortController();const version=++this.version;
    const restore=captureFocus(this.list,()=>this.newButton,{signal:this.read.signal});
    try {
      this.loading=true;this.newButton.disabled=true;
      const signal=settingsSignal(AbortSignal.any([this.abort.signal,this.read.signal]));
      this.list.replaceChildren(node('p','settings-note','Loading schedules…'));
      const results=await Promise.allSettled(['/api/schedules','/api/projects/full','/api/backends'].map(path=>this.api.json(path,{signal})));
      if(this.abort.signal.aborted || version!==this.version){log('settings-schedules','obsolete read ignored',{stale:true});return;}
      if(results[0].status==='rejected') {
        this.list.replaceChildren(errorMessage(`Schedules unavailable. ${results[0].reason.message}`),button('Retry schedules',()=>this.load()));
        log('settings-schedules','load failed',{status:results[0].reason.status});return;
      }
      this.rows=results[0].value.schedules || [];
      if(results[1].status==='fulfilled')this.projects=results[1].value.projects || [];
      if(results[2].status==='fulfilled')this.backends=results[2].value.backends || [];
      if(results.slice(1).some(result=>result.status==='rejected')) {
        this.status('Some project or backend choices could not load. Refresh schedules before creating a new one.',true);
        log('settings-schedules','choices unavailable');
      }
      this.loading=false;this.newButton.disabled=this.pending || !this.projects.length;
      this.renderList();log('settings-schedules','loaded',{count:this.rows.length});
    } finally { restore(); }
  }
  renderList() {
    this.list.replaceChildren();this.editControls=new Map();
    if(!this.rows.length)this.list.append(node('p','settings-note','No schedules yet.'));
    for(const schedule of this.rows) {
      const row=node('article','settings-schedule'),heading=node('div','settings-section-heading');
      heading.append(node('h3','',schedule.name));
      for(const [label,operation] of [['Edit',()=>this.edit(schedule)],['Run',()=>this.run(schedule)],['Delete',()=>this.remove(schedule)]]) {
        const control=button(label==='Run'?'Run now':label,operation);control.dataset.focusKey=JSON.stringify([schedule.id,label]);control.setAttribute('aria-label',label==='Run'?`Run ${schedule.name} now`:`${label} ${schedule.name}`);control.disabled=this.pending;heading.append(control);
        if(label==='Edit')this.editControls.set(schedule.id,control);
      }
      const cadence=schedule.run_once_at?`Once ${when(schedule.run_once_at)}`:schedule.schedule;
      row.append(heading,node('p','settings-note',[schedule.project,schedule.backend,schedule.model,cadence,schedule.enabled?'Enabled':'Disabled'].filter(Boolean).join(' · ')));
      row.append(node('p','settings-note',`Next ${schedule.enabled?when(schedule.next_run_at):'Disabled'} · Last ${schedule.last_run_at?when(schedule.last_run_at):'Never'}`));
      if(schedule.last_task_id)row.append(node('p','settings-note',`Last task ${schedule.last_task_id}`));
      if(schedule.last_error)row.append(node('p','error-text',`Last error ${schedule.last_error}`));
      this.list.append(row);
    }
  }
  edit(schedule=null) {
    if(this.pending)return;
    this.status('');
    const opener=document.activeElement;
    const form=scheduleEditor(schedule,this.projects,this.backends,values=>this.save(values,schedule),()=>{
      const ownedFocus=this.editor.contains(document.activeElement);
      this.editor.replaceChildren();
      if(ownedFocus) {
        (opener?.isConnected?opener:this.newButton).focus({preventScroll:true});
        log('settings-schedules','editor focus restored');
      }
    });
    this.editor.replaceChildren(form);form.querySelector('input')?.focus();
  }
  async save(values,original) {
    let body;
    try{body=schedulePayload(values,original || {});}catch(error){this.status(error.message,true);return;}
    const restore=captureFocus(this.editor,()=>this.editControls.get(original?.id)||this.newButton,{signal:this.abort.signal});
    await this.mutate(async()=>{
      await this.api.json(original?`/api/schedules/${encodeURIComponent(original.id)}`:'/api/schedules',{method:original?'PUT':'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body),signal:settingsSignal(this.abort.signal)});
      return 'Saved schedule.';
    },'Save');
    restore();
  }
  async remove(schedule) {
    if(this.pending || !await confirmDialog({signal:this.abort.signal,message:`Delete schedule "${schedule.name}"? Tasks already created by this schedule are not affected.`}))return;
    await this.mutate(async()=>{
      await this.api.request(`/api/schedules/${encodeURIComponent(schedule.id)}`,{method:'DELETE',signal:settingsSignal(this.abort.signal)});return 'Deleted schedule.';
    },'Delete');
  }
  async run(schedule) {
    await this.mutate(async()=>{
      const result=await this.api.json(`/api/schedules/${encodeURIComponent(schedule.id)}/run`,{method:'POST',signal:settingsSignal(this.abort.signal)});
      return result.task_id?`Started task ${result.task_id}.`:'Schedule fired.';
    },'Run');
  }
  async mutate(operation,label) {
    if(this.pending || this.abort.signal.aborted)return;
    const restore=captureFocus(this.list,()=>this.newButton,{signal:this.abort.signal});
    this.lock(true);this.status(`${label} in progress…`);
    try {
      const message=await operation();if(this.abort.signal.aborted)return;
      this.editor.replaceChildren();this.status(message);this.changed();log('settings-schedules','mutation completed');await this.load();return true;
    }catch(error){
      if(!this.abort.signal.aborted)this.status(`${label} was not confirmed. Your edits are kept. Check ${label==='Run'?'tasks':'schedules'} before retrying. ${error.message}`,true);
      log('settings-schedules','mutation failed',{status:error.status});
    }finally{if(!this.abort.signal.aborted)this.lock(false);restore();}
  }
  destroy(){this.abort.abort();this.read?.abort();this.version++;this.busy(false);}
}
