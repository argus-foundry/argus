import {captureFocus} from '../../shared/focus-recovery.js';
import {confirmDialog} from '../../shared/confirm-dialog.js';
import { node, button, errorMessage } from '../../shared/dom.js';
import { field, actions, values, settingsSignal } from '../settings-fields.js';
import { SandboxProposals } from './settings-proposals.js';
import { log } from '../../core/api.js';
const paths = text => text.split('\n').map(path => path.trim()).filter(Boolean);

export class SandboxSettings {
  constructor(context) { Object.assign(this, context); this.abort = new AbortController(); this.load(); }
  async load() {
    const restore=captureFocus(this.host,()=>document.querySelector('.settings-back'),{signal:this.abort.signal});
    this.proposals?.destroy();
    this.host.replaceChildren(node('p', 'settings-note', 'Loading Sandbox…'));
    try {
      const { sandbox: s } = await this.api.json('/api/settings', { signal: settingsSignal(this.abort.signal) });
      if (this.abort.signal.aborted) return;
      this.grantsChanged = false;
      const form = node('form', 'settings-form'), fields = node('fieldset', 'settings-fields'); fields.append(node('legend', '', 'Sandbox')); form.append(fields);
      fields.append(node('p', 'settings-note', 'Global changes require a master token and apply to new sessions. Existing sessions are not restarted.'));
      const enabled = field(fields, 'Sandbox enabled', 'enabled', s.enabled, 'checkbox'); enabled.disabled = !s.available;
      fields.append(node('p', 'settings-note', s.available ? 'Host sandbox-exec is available.' : 'sandbox-exec is unavailable on this host.'));
      field(fields, 'Global deny read paths', 'denyRead', (s.deny_read || []).join('\n'), 'textarea', 'One path per line.');
      field(fields, 'Global extra write paths', 'extraWrite', (s.extra_write || []).join('\n'), 'textarea', 'One path per line. Unedited Apple Events grants are preserved.');
      actions(fields, 'Save sandbox');
      fields.append(button('Reload Sandbox', async () => { if (await confirmDialog({signal:this.abort.signal,message:'Reload Sandbox and discard unsaved global edits?'})) this.load(); }));
      form.addEventListener('submit', event => { event.preventDefault(); this.save(values(form), fields); });
      const proposals = node('div'); this.host.replaceChildren(form, proposals);
      this.proposals = new SandboxProposals({ api: this.api, host: proposals, status: this.status, busy: value => this.setBusy(value), grantsChanged: () => { this.grantsChanged = true; } });
      log('settings-sandbox', 'loaded');
    } catch (error) {
      if (this.abort.signal.aborted) return;
      const retry = document.createElement('button'); retry.textContent = 'Retry Sandbox'; retry.addEventListener('click', () => this.load());
      this.host.replaceChildren(errorMessage(`Sandbox unavailable. ${error.message}`), retry); log('settings-sandbox', 'load failed', { status: error.status });
    } finally { restore(); }
  }
  async save(input, fields) {
    if (this.grantsChanged) { this.status('Proposal grants changed. Reload Sandbox before saving global paths; your current edits are kept.', true); return; }
    const restore=captureFocus(this.host,()=>document.querySelector('.settings-back'),{signal:this.abort.signal});
    fields.disabled = true; this.setBusy(true);
    try {
      await this.api.json('/api/settings', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ sandbox: { enabled: input.enabled, deny_read: paths(input.denyRead), extra_write: paths(input.extraWrite) } }), signal: settingsSignal(this.abort.signal) });
      if (this.abort.signal.aborted) return;
      this.status('Saved sandbox settings. Changes apply to new sessions only.'); this.changed(); log('settings-sandbox', 'saved');
    } catch (error) {
      if (!this.abort.signal.aborted) this.status(error.status === 403 ? 'A master token is required to save Sandbox. Your edits are kept.' : `Save was not confirmed. Your edits are kept. ${error.message}`, true);
      log('settings-sandbox', 'save failed', { status: error.status });
    } finally { fields.disabled = false; this.setBusy(false); restore(); }
  }
  setBusy(value) { for (const fields of this.host.querySelectorAll('fieldset')) fields.disabled = value; this.busy(value); }
  destroy() { this.abort.abort(); this.proposals?.destroy(); }
}
