import {confirmDialog} from '../../shared/confirm-dialog.js';
import { node, button, errorMessage } from '../../shared/dom.js';
import { field, settingsSignal } from '../settings-fields.js';
import { copyCredential } from './settings-credential-copy.js';
import { log } from '../../core/api.js';

export class DeviceTokens {
  constructor(context) {
    Object.assign(this, context); this.abort = new AbortController(); this.host.append(node('h3', '', 'API tokens'), node('p', 'settings-note', 'Listing, creating and revoking device tokens require a master token.'));
    const form = node('form'); this.label = field(form, 'Token label', 'tokenLabel', ''); const create = node('button', 'primary', 'Create device token'); create.type = 'submit'; form.append(create);
    form.addEventListener('submit', event => { event.preventDefault(); this.mint(); });
    this.secret = node('div'); this.list = node('div'); this.host.append(form, this.secret, button('Refresh tokens', () => this.load()), this.list); this.load();
  }
  async load() {
    this.read?.abort(); const read = this.read = new AbortController();
    try {
      const { tokens = [] } = await this.api.json('/api/tokens', { signal: settingsSignal(AbortSignal.any([read.signal, this.abort.signal])) });
      if (read.signal.aborted || this.abort.signal.aborted) return;
      this.list.replaceChildren(); if (!tokens.length) this.list.append(node('p', 'settings-note', 'No device tokens.'));
      for (const token of tokens) {
        const row = node('div', 'settings-record'); row.append(node('strong', '', token.label || 'Device'), node('p', 'settings-note', `Ending ${token.last4 || 'unknown'} · ${token.revoked ? 'Revoked' : 'Active'}`));
        if (token.last_used) row.append(node('p', 'settings-note', `Last used ${new Date(token.last_used * 1000).toLocaleString()}`));
        if (!token.revoked) { const revoke = button('Revoke', () => this.revoke(token)); revoke.setAttribute('aria-label', `Revoke ${token.label || 'Device'}`); row.append(revoke); }
        this.list.append(row);
      }
      log('settings-tokens', 'loaded', { count: tokens.length });
    } catch (error) {
      if (!read.signal.aborted && !this.abort.signal.aborted) this.list.replaceChildren(errorMessage(error.status === 403 ? 'A master token is required to list API tokens.' : `Tokens unavailable. ${error.message}`));
      log('settings-tokens', 'load failed', { status: error.status });
    }
  }
  async action(operation, success) {
    this.busy(true);
    try { const result = await operation(); if (!this.abort.signal.aborted) { success(result); await this.load(); } }
    catch (error) {
      if (!this.abort.signal.aborted) this.status(error.status === 403 ? 'A master token is required to manage API tokens.' : `Token operation was not confirmed. Refresh before retrying. ${error.message}`, true);
      log('settings-tokens', 'operation failed', { status: error.status });
    } finally { this.busy(false); }
  }
  mint() {
    const body = JSON.stringify({ label: this.label.value.trim() || 'device' });
    if (new TextEncoder().encode(body).length > 4096) { this.status('Token label exceeds the existing request limit.', true); return; }
    return this.action(() => this.api.json('/api/tokens', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body, signal: settingsSignal(this.abort.signal) }), result => {
      this.clearSecret(); const output = field(this.secret, 'New device token', 'newToken', result.token || ''); output.readOnly = true;
      const copy = button('Copy device token', () => copyCredential(output.value, this.status, this.abort.signal));
      const hide = button('Hide device token', () => {
        const ownedFocus = document.activeElement === hide;
        this.clearSecret();
        if (ownedFocus) { this.label.focus({ preventScroll: true }); log('settings-tokens', 'hide focus restored'); }
      });
      this.secret.append(node('p', 'settings-warning', 'Save this credential now. It is shown only in this section and cannot be retrieved again.'), copy, hide);
      this.status('Created device token.'); log('settings-tokens', 'created', { status: true });
    });
  }
  async revoke(token) {
    if (!await confirmDialog({signal:this.abort.signal,message:`Revoke ${token.label || 'Device'}? Clients using this token will lose access.`})) return;
    return this.action(() => this.api.json(`/api/tokens/${encodeURIComponent(token.id)}`, { method: 'DELETE', signal: settingsSignal(this.abort.signal) }), () => { this.clearSecret(); this.status('Revoked device token.'); log('settings-tokens', 'revoked', { status: true }); });
  }
  clearSecret() { for (const input of this.secret.querySelectorAll('input')) input.value = ''; this.secret.replaceChildren(); }
  destroy() { this.abort.abort(); this.read?.abort(); this.clearSecret(); this.label.value = ''; }
}
