import {confirmDialog} from '../../shared/confirm-dialog.js';
import { node, button } from '../../shared/dom.js';
import { field, settingsSignal } from '../settings-fields.js';
import { log } from '../../core/api.js';

export class UpdateSettings {
  constructor(context) {
    Object.assign(this, context); this.abort = new AbortController();
    this.fields = node('fieldset', 'settings-fields'); this.fields.append(node('legend', '', 'Update Argus'));
    this.fields.append(node('p', 'settings-note', 'Source path and updates require a master token. Update builds the saved source checkout with go install ./... and restarts the daemon.'));
    this.path = field(this.fields, 'Source path', 'argusSourcePath', '', 'text', 'Local Argus checkout on the daemon host. Save changes before updating.');
    const actions = node('div', 'settings-actions'); actions.append(button('Save source path', () => this.save()), button('Reload saved source path', () => this.load()), button('Update Argus', () => this.update(), 'primary')); this.fields.append(actions);
    this.output = node('pre', 'settings-build-output'); this.output.id = 'update-output'; this.output.tabIndex = 0; this.output.hidden = true; this.output.setAttribute('aria-label', 'Argus build output');
    this.host.replaceChildren(this.fields, this.output); this.load();
  }
  async load() {
    if (this.pending || this.restartRequested) return;
    this.read?.abort(); const read = this.read = new AbortController(); this.fields.disabled = true;
    try {
      const result = await this.api.json('/api/source-path', { signal: settingsSignal(AbortSignal.any([read.signal, this.abort.signal])) });
      if (read.signal.aborted || this.abort.signal.aborted) return;
      this.path.value = this.savedPath = result.path || ''; this.status(''); log('settings-update', 'source path loaded', { status: true });
    } catch (error) {
      if (!read.signal.aborted && !this.abort.signal.aborted) this.status(error.status === 403 ? 'A master token is required to read the source path.' : `Source path unavailable. Your edits are kept. ${error.message}`, true);
      log('settings-update', 'source path read failed', { status: error.status });
    } finally { if (this.read === read && !this.abort.signal.aborted) this.fields.disabled = false; }
  }
  async run(operation, updating = false) {
    if (this.pending || this.restartRequested) return;
    this.pending = true; this.fields.disabled = true; this.busy(true);
    try { await operation(); }
    catch (error) {
      if (!this.abort.signal.aborted) this.status(error.status === 403 ? 'A master token is required for source-path changes and updates.' : updating ? `Update was not confirmed. The daemon may still be rebuilding or restarting. Check its connection before retrying. ${error.message}` : `Source path save was not confirmed. Your edits are kept. ${error.message}`, true);
      log('settings-update', 'operation failed', { status: error.status });
    } finally { this.pending = false; this.fields.disabled = !!this.restartRequested; this.busy(false); }
  }
  save() {
    const path = this.path.value.trim(), body = JSON.stringify({ path });
    if (new TextEncoder().encode(body).length > 4096) { this.status('Source path exceeds the existing request limit.', true); return; }
    return this.run(async () => {
      const result = await this.api.json('/api/source-path', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body, signal: settingsSignal(this.abort.signal) });
      if (this.abort.signal.aborted) return;
      this.path.value = this.savedPath = result.path ?? path; this.status('Saved source path.'); log('settings-update', 'source path saved', { status: true });
    });
  }
  async update() {
    const path = this.path.value.trim();
    if (!path || path !== this.savedPath) { this.status('Save source path before updating Argus.', true); return; }
    if (this.pending || this.restartRequested || !await confirmDialog({signal:this.abort.signal,message:'Rebuild Argus with go install ./... from the saved source path and restart the daemon? The workspace reloads in 5 seconds after success and unsent drafts are lost.'})) return;
    return this.run(async () => {
      this.output.hidden = false; this.output.textContent = 'Running go install ./...'; this.status('Updating Argus…');
      const response = await this.api.request('/api/update', { method: 'POST', allowErrorResponse: true, signal: AbortSignal.any([this.abort.signal, AbortSignal.timeout(60000)]) });
      let result;
      try { result = await response.json(); } catch { if (response.ok) throw new Error('The update response was incomplete.'); result = {}; }
      if (this.abort.signal.aborted) return;
      if (response.ok && (!result || typeof result.restart !== 'boolean')) throw new Error('The update response did not confirm restart state.');
      result ||= {};
      this.output.textContent = result.output || '(No build output returned.)';
      if (!response.ok) { this.status(response.status === 403 ? 'A master token is required to update Argus.' : `Update failed. ${result.error || 'HTTP ' + response.status}`, true); log('settings-update', 'build failed', { status: response.status }); return; }
      this.restartRequested = result.restart === true;
      this.status(this.restartRequested ? 'Update completed; daemon restart requested. Workspace reloads in 5 seconds.' : 'Update completed. No restart was requested.');
      if (this.restartRequested) this.timer = setTimeout(() => location.reload(), 5000);
      log('settings-update', 'update response received', { status: true });
    }, true);
  }
  destroy() { this.abort.abort(); this.read?.abort(); clearTimeout(this.timer); this.path.value = ''; this.output.textContent = ''; }
}
