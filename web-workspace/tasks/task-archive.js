import {confirmDialog} from '../shared/confirm-dialog.js';
import { $, notice } from '../shared/dom.js';
import { log } from '../core/api.js';

export class TaskArchive {
  constructor(api, onChanged, context=()=>({})) {
    this.api = api;this.context=context;
    this.onChanged = onChanged;
    $('task-groups').addEventListener('task-archive', event => this.open(event.detail));
  }
  selectTask(task) { this.task = task; }
  open(task = this.task) {
    if (!task || this.busy) return;
    this.target = { ...task };
    this.dialog?.close();this.abort?.abort();this.abort=new AbortController();
    const signal=this.abort.signal,captured=this.target;
    this.dialog=confirmDialog({id:'archive-dialog',title:`You're about to ${task.archived?'restore':'archive'} ${task.name}.`,message:task.archived?'This returns the task to Active. It does not restart its agent or recover cleared intertask messages. The existing worktree is retained.':'Archiving clears this task’s intertask messages. Its agent will stop later when idle. The existing worktree is retained.',confirmLabel:task.archived?'Restore task':'Archive task',signal,keepFailuresOpen:true,tasks:[captured],taskAction:task.archived?'restore':'archive',...this.context(),request:(path,options)=>this.api.json(path,options),onBusy:busy=>{if(this.abort?.signal===signal)this.busy=busy;},onTaskSuccess:async()=>{
      if(signal.aborted||this.target!==captured)return;
      notice(captured.archived?'Task restored to Active.':'Task archived. Its worktree is retained.');
      await this.onChanged(captured);log('archive','updated',{status:true});
    }}).dialog;
  }
  clear() { this.target = null; this.abort?.abort(); this.dialog?.close(); this.selectTask(null); }
}
