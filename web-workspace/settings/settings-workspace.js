import { log } from '../core/api.js';
import { $, node } from '../shared/dom.js';
import { settingsURL } from './settings-policy.js';
import { SettingsNavigation } from './settings-navigation.js';
import { createSettingsSection, validSettingsSection, settingsSectionTitle } from './settings-sections.js';

export class WorkspaceSettings {
  constructor(api, callbacks) {
    Object.assign(this, { api, callbacks, active: false, busy: false, generation: 0 });
    this.panel = node('section', 'settings-panel'); this.panel.id = 'settings-panel'; this.panel.hidden = true;
    this.panel.setAttribute('aria-label', 'Settings');
    const body = node('div', 'settings-body'); this.navigation = new SettingsNavigation(api, id => this.open(id), () => this.close());
    this.back = this.navigation.back;
    const content = node('div', 'settings-content'); this.status = node('p', 'settings-status'); this.status.id = 'settings-status'; this.status.setAttribute('role', 'status'); this.host = node('div', 'settings-section'); this.title = node('h1', 'settings-page-title');
    this.page = node('div', 'settings-page');
    this.page.append(this.title, this.status, this.host); content.append(this.page); body.append(this.navigation.element, content); this.panel.append(body); $('main').append(this.panel);
    $('workspace-settings').dataset.tooltip = 'Settings'; $('workspace-settings').setAttribute('aria-label', 'Settings'); $('workspace-settings').removeAttribute('title');
    $('workspace-settings').setAttribute('aria-controls', this.panel.id); $('workspace-settings').setAttribute('aria-expanded', 'false');
    $('workspace-settings').addEventListener('click', () => this.open());
    document.addEventListener('keydown', event => {
      if (event.key === '/' && !event.metaKey && !event.ctrlKey && !event.altKey && !event.defaultPrevented && this.active && !event.target.closest?.('input,textarea,select,[contenteditable=true]') && !document.querySelector('dialog[open]') && this.navigation.search.getClientRects().length) { event.preventDefault(); this.navigation.search.focus(); }
      if (!event.defaultPrevented && event.key === 'Escape' && this.active && !this.busy && !document.querySelector('dialog[open]')) { event.preventDefault(); this.close(); }
    });
  }
  setConnected(connected) { this.connected = connected; $('workspace-settings').disabled = !connected; if (!connected) this.clear(); }
  setBusy(value, { cancellable = false } = {}) { this.busy = value; this.cancellable = value && cancellable; this.back.disabled = value; this.navigation.setBusy(value); }
  message(text, error = false) { this.status.textContent = text; this.status.classList.toggle('error-text', error); }
  open(section = 'general', { history = true } = {}) {
    if (!this.connected || !this.canLeave()) return;
    section = validSettingsSection(section) ? section : 'general';
    const opening = !this.active;
    if (opening) {
      this.callbacks.beforeOpen?.();
      if (history) this.callbacks.history.push(settingsURL(location.href, section), { workspaceSettings: true });
      this.active = true; this.navigation.load(); this.hidden = [...$('main').children, ...$('workspace').children, document.querySelector('.workspace-header')].filter(child => child !== this.panel && child !== $('main')).map(child => [child, child.inert]);
      for (const [child] of this.hidden) child.inert = true;
      document.body.classList.add('settings-mode'); $('main').classList.add('settings-open'); this.panel.hidden = false; this.callbacks.opened();
      $('workspace-settings').setAttribute('aria-expanded', 'true'); log('settings-workspace', 'full-window mode opened');
    } else if (history) this.callbacks.history.replace(settingsURL(location.href, section), window.history.state);
    if (this.section === section && this.view) return;
    this.view?.destroy(); this.setBusy(false); const generation = ++this.generation; this.section = section; this.message('');
    this.navigation.select(section); this.title.textContent = settingsSectionTitle(section);
    const current = () => this.active && generation === this.generation;
    const busy = (value, options) => { if (current()) this.setBusy(value, options); };
    this.view = createSettingsSection(section, { api: this.api, host: this.host, status: (text, error) => { if (current()) this.message(text, error); },
      changed: () => { if (current()) { this.navigation.load(); this.callbacks.changed(); } }, busy, baseline: initial => (this.bootstrap ||= structuredClone(initial)) });
    if (opening) this.back.focus({ preventScroll: true });
  }
  close() {
    if (!this.active || this.busy) return;
    if (window.history.state?.workspaceSettings) window.history.back();
    else { this.callbacks.history.replace(settingsURL(location.href, null)); this.dismiss(); this.callbacks.restore(); }
  }
  dismiss(silent = false) {
    if (!this.active) return;
    this.active = false; this.navigation.destroy(); this.generation++; this.view?.destroy(); this.view = null; this.section = null; this.setBusy(false);
    this.host.replaceChildren(); this.message(''); this.panel.hidden = true; $('main').classList.remove('settings-open'); document.body.classList.remove('settings-mode');
    $('workspace-settings').setAttribute('aria-expanded', 'false');
    for (const [child, inert] of this.hidden || []) child.inert = inert;
    log('settings-workspace', 'full-window mode closed');
    if (!silent) this.callbacks.closed();
  }
  openProject(name, options = {}) { this.open(`repository:${name}`, options); }
  canLeave() {
    if (this.active && this.busy && !this.cancellable) { log('settings-workspace', 'departure blocked during save'); return false; }
    return true;
  }
  leaveForTask() {
    if (!this.canLeave()) return false;
    if (this.active) { this.callbacks.history.replace(settingsURL(location.href, null)); this.dismiss(); }
    return true;
  }
  clear() { this.dismiss(true); this.navigation.destroy(true); this.bootstrap = null; }
}
