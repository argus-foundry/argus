import {accountWindows} from '../../composer/composer-context-policy.js';
import {captureFocus} from '../../shared/focus-recovery.js';
import { node, button, errorMessage } from '../../shared/dom.js';
import { settingsSignal } from '../settings-fields.js';
import { log } from '../../core/api.js';

export class AgentSettings {
  constructor(context) { Object.assign(this, context); this.abort = new AbortController(); this.load(); }
  async load() {
    this.read?.abort(); const read = this.read = new AbortController(), lifetime = AbortSignal.any([read.signal, this.abort.signal]), signal = settingsSignal(lifetime);
    const restore=captureFocus(this.host,()=>document.querySelector('.settings-back'),{signal:lifetime});
    const header = node('div', 'settings-section-heading'); header.append(node('h2', '', 'Agents'), button('Refresh agents', () => this.load()));
    const backends = node('section'), accounts = node('section'); backends.append(node('h3', '', 'Backends')); accounts.append(node('h3', '', 'Accounts'));
    this.host.replaceChildren(header, node('p', 'settings-note', 'Backend and account configuration is read-only here. Account creation and sign-in are managed on the host.'), backends, accounts);restore();
    await Promise.allSettled([
      this.list('/api/backends', backends, data => data.backends || [], backend => this.backend(backend), signal),
      this.list('/api/accounts', accounts, data => Array.isArray(data) ? data : [], account => this.account(account), signal),
    ]);
  }
  async list(path, host, items, render, signal) {
    const name = path.endsWith('accounts') ? 'Accounts' : 'Backends';
    try {
      const result = await this.api.json(path, { signal }); if (signal.aborted) return;
      const records = items(result); if (!records.length) host.append(node('p', 'settings-note', `No ${name.toLowerCase()} returned.`));
      for (const record of records) host.append(render(record));
      log('settings-agents', 'loaded', { count: records.length });
    } catch (error) {
      if (!signal.aborted) host.append(errorMessage(`${name} unavailable. ${error.message}`));
      log('settings-agents', 'load failed', { status: error.status });
    }
  }
  backend(backend) {
    const row = node('article', 'settings-record'); row.append(node('h4', '', backend.label || backend.name), node('p', 'settings-note', backend.name), node('code', '', backend.command || 'Command unavailable'));
    row.append(node('p', 'settings-note', `Family ${backend.kind || 'Custom'} · Default model ${backend.model || 'Not set'}`));
    row.append(node('p', 'settings-note', `Available models ${(backend.models || []).join(', ') || 'Not supplied'}`));
    if (backend.prompt_flag) row.append(node('p', 'settings-note', `Prompt flag ${backend.prompt_flag}`));
    return row;
  }
  account(account) {
    const row = node('article', 'settings-record'); row.append(node('h4', '', `${account.label || account.name}${account.is_default ? ' (default)' : ''}`));
    if (account.email) row.append(node('p', 'settings-note', account.email));
    if (account.org || account.plan) row.append(node('p', 'settings-note', [account.org, account.plan].filter(Boolean).join(' · ')));
    const claude = account.supports?.claude === false ? 'Claude unsupported' : account.supports?.claude !== true ? 'Claude support unknown' : typeof account.logged_in !== 'boolean' ? 'Claude sign-in unknown' : account.logged_in ? 'Claude signed in' : 'Claude not signed in';
    const codex = account.supports?.codex === false ? 'Codex unsupported' : typeof account.codex_logged_in === 'boolean' ? account.codex_logged_in ? 'Codex signed in' : 'Codex not signed in' : 'Codex sign-in unknown';
    row.append(node('p', 'settings-note', claude), node('p', 'settings-note', codex));
    for (const [key, label] of [['claude_config_dir', 'Claude config'], ['codex_home', 'Codex home']]) if (account[key]) row.append(node('p', 'settings-note', `${label} ${account[key]}`));
    if(account.supports?.claude !== false) {
      const windows=accountWindows(account);
      row.append(node('p','settings-note',windows.length?'Claude usage':'Claude usage unavailable'));
      for(const window of windows)row.append(node('p','settings-note',`${window.label} · ${Number((100-window.left).toFixed(2))}% used · resets ${new Date(window.reset).toLocaleString()}`));
      log('settings-agents','Claude usage rendered',{count:windows.length});
    }
    return row;
  }
  destroy() { this.abort.abort(); this.read?.abort(); }
}
