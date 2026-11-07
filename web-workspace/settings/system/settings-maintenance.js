import {captureFocus} from '../../shared/focus-recovery.js';
import {confirmDialog} from '../../shared/confirm-dialog.js';
import { node, button } from '../../shared/dom.js';
import { settingsSignal } from '../settings-fields.js';
import { log } from '../../core/api.js';

export class MaintenanceSettings {
  constructor(context) {
    Object.assign(this, context); this.abort = new AbortController();
    this.fields = node('fieldset', 'settings-fields'); this.fields.append(node('legend', '', 'Maintenance'));
    this.fields.append(node('p', 'settings-warning', 'Cleanup deletes completed task data and session logs, stops their sessions, removes associated worktrees and branches, and sweeps orphaned worktree directories. This cannot be undone.'));
    const actions = node('div', 'settings-actions'); actions.append(button('Clean up completed tasks', () => this.prune())); this.fields.append(actions);
    this.result = node('p', 'settings-note'); this.result.id = 'maintenance-result'; this.result.setAttribute('role', 'status'); this.host.replaceChildren(this.fields, this.result);
  }
  async prune() {
    if (this.pending || !await confirmDialog({signal:this.abort.signal,message:'Delete every completed task and its sessions/logs, remove its worktrees and branches, and sweep orphaned worktree directories? This cannot be undone. Tasks still attached to a team are skipped.'})) return;
    const restore=captureFocus(this.host,()=>document.querySelector('.settings-back'),{signal:this.abort.signal});
    this.pending = true; this.fields.disabled = true; this.busy(true); this.result.textContent = ''; this.status('Cleaning up completed tasks…');
    try {
      const result = await this.api.json('/api/maintenance/prune-completed', { method: 'POST', signal: settingsSignal(this.abort.signal) });
      if (this.abort.signal.aborted) return;
      const count = key => Number.isInteger(result[key]) && result[key] >= 0 ? result[key] : 'Unavailable';
      this.result.textContent = `${count('pruned')} tasks pruned; ${count('worktrees')} worktrees removed; ${count('orphans')} orphan directories removed; ${count('skippedHeraBound')} skipped because still attached to a team.`;
      this.status('Cleanup completed.'); this.changed(); log('settings-maintenance', 'cleanup completed', { count: result.pruned });
    } catch (error) {
      if (!this.abort.signal.aborted) this.status(`Cleanup was not confirmed. Refresh tasks before retrying. ${error.message}`, true);
      log('settings-maintenance', 'cleanup failed', { status: error.status });
    } finally { this.pending = false; this.fields.disabled = false; this.busy(false); restore(); }
  }
  destroy() { this.abort.abort(); }
}
