import {confirmDialog} from '../shared/confirm-dialog.js';
import { $, notice } from '../shared/dom.js';
import { log } from '../core/api.js';

export class TaskArchive {
  constructor(api, onChanged) {
    this.api = api;
    this.onChanged = onChanged;
    $('task-groups').addEventListener('task-archive', event => this.open(event.detail));
  }
  selectTask(task) { this.task = task; }
  open(task = this.task) {
    if (!task || this.busy) return;
    this.target = { ...task };
    this.dialog?.close();this.abort?.abort();this.abort=new AbortController();
    this.dialog=confirmDialog({id:'archive-dialog',title:`You're about to ${task.archived?'restore':'archive'} ${task.name}.`,message:task.archived?'This returns the task to Active. It does not restart its agent or recover cleared intertask messages. The existing worktree is retained.':'Archiving clears this task’s intertask messages. Its agent will stop later when idle. The existing worktree is retained.',confirmLabel:task.archived?'Restore task':'Archive task',signal:this.abort.signal,onConfirm:()=>this.confirm()}).dialog;
  }
  async confirm() {
    const task = this.target;
    if (!task || this.busy) return;
    this.busy = true;

    try {
      await this.api.json(`/api/tasks/${encodeURIComponent(task.id)}/${task.archived ? 'unarchive' : 'archive'}`, { method: 'POST', signal: this.abort.signal });
      if (this.target !== task) return;
      notice(task.archived ? 'Task restored to Active.' : 'Task archived. Its worktree is retained.');
      await this.onChanged(task);
      log('archive', 'updated', { status: task.archived ? 'restored' : 'archived' });
    } catch (error) {
      if (error.name === 'AbortError' || this.target !== task) return;
      log('archive', 'failed', { status: error.status || 'error' });
      throw new Error(`Task was not updated. ${error.message}`);
    } finally { this.busy = false;  }
  }
  clear() { this.target = null; this.abort?.abort(); this.dialog?.close(); this.selectTask(null); }
}
