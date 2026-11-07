import { node, button, errorMessage } from '../shared/dom.js';
import { field, select, actions, values, settingsSignal } from './settings-fields.js';
import { generalPayload } from './settings-policy.js';
import { log } from '../core/api.js';

export class GeneralSettings {
  constructor(api, host, status, changed, baseline, busy) {
    Object.assign(this, { api, host, status, changed, baseline, busy });
    this.abort = new AbortController(); this.load();
  }
  async load() {
    this.host.replaceChildren(node('p', 'settings-note', 'Loading General settings…'));
    try {
      const signal = settingsSignal(this.abort.signal);
      const [settings, backends, projects] = await Promise.all([
        this.api.json('/api/settings', { signal }), this.api.json('/api/backends', { signal }), this.api.json('/api/projects/full', { signal }),
      ]);
      if (this.abort.signal.aborted) return;
      this.initial = this.baseline(settings); this.settings = settings; this.render(backends.backends || [], projects.projects || []);
      log('settings-general', 'loaded');
    } catch (error) {
      if (this.abort.signal.aborted) return;
      this.host.replaceChildren(errorMessage(`General settings unavailable. ${error.message}`), button('Retry General', () => this.load()));
      log('settings-general', 'load failed', { status: error.status || 'error' });
    }
  }
  render(backends, projects) {
    const s = this.settings, form = node('form', 'settings-form'), fields = node('fieldset', 'settings-fields');
    form.append(fields);
    fields.append(node('legend', '', 'General'));
    select(fields, 'Default agent', 'backend', [['', 'Default'], ...backends.map(backend => [backend.name, backend.name])], s.defaults?.backend || '', 'Agent used for new tasks by default.');
    select(fields, 'Default share project', 'shareProject', [['', 'None'], ...projects.map(project => [project.name, project.name])], s.defaults?.share_project || '', 'Repository used by the share helper.');
    field(fields, 'Knowledge base enabled', 'kbEnabled', s.kb?.enabled, 'checkbox', 'Make the knowledge base available to agents.');
    field(fields, 'Vault path', 'vaultPath', s.kb?.metis_vault_path || '', 'text', 'Changing the vault path requires a daemon restart.');
    field(fields, 'HTTP API enabled', 'apiEnabled', s.api?.enabled, 'checkbox', 'Changing this requires a daemon restart.');
    fields.append(node('p', 'settings-note', `HTTP port ${s.api?.http_port ?? 'Unavailable'} · Read-only`));
    this.restart = node('p', 'settings-note'); this.restart.id = 'settings-restart';
    actions(fields, 'Save General'); form.append(this.restart);
    form.addEventListener('submit', event => { event.preventDefault(); this.save(form, fields); });
    this.host.replaceChildren(form);
  }
  async save(form, fields) {
    const body = generalPayload(values(form)); fields.disabled = true; this.busy(true); this.status('Saving General…');
    try {
      const result = await this.api.json('/api/settings', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body), signal: settingsSignal(this.abort.signal) });
      if (this.abort.signal.aborted) return;
      const restart = result.kb?.metis_vault_path !== this.initial.kb?.metis_vault_path || result.api?.enabled !== this.initial.api?.enabled;
      this.restart.textContent = restart ? 'Daemon restart required for the vault path or API change. The daemon has not been restarted.' : '';
      this.status('Saved General settings.'); this.changed(); log('settings-general', 'saved');
    } catch (error) {
      if (!this.abort.signal.aborted) this.status(`Save was not confirmed. Your edits are kept. Refresh before retrying. ${error.message}`, true);
      log('settings-general', 'save failed', { status: error.status || 'error' });
    } finally { fields.disabled = false; this.busy(false); }
  }
  destroy() { this.abort.abort(); }
}
