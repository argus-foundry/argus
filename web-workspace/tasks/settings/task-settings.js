import {accountList} from '../create/new-task-account.js';
import { $, node, button } from '../../shared/dom.js';
import { log } from '../../core/api.js';
import { taskSettingsURL } from './task-settings-policy.js';
import { taskSettingsForm } from './task-settings-form.js';
import { confirmTaskAction } from './task-settings-confirm.js';

export class TaskSettingsView {
  constructor(api, callbacks) {
    Object.assign(this, { api, callbacks, active: false, generation: 0, readVersion: 0 });
    this.panel = node('section', 'task-settings-panel'); this.panel.id = 'task-settings-panel'; this.panel.hidden = true;
    const header = node('header', 'settings-header'); this.back = button('Back to task', () => this.close()); header.append(node('h1', '', 'Task settings'), this.back);
    this.status = node('p', 'settings-status'); this.status.id = 'task-settings-status'; this.status.setAttribute('role', 'status');
    this.host = node('div', 'task-settings-content'); this.panel.append(header, this.status, this.host); $('main').append(this.panel);
    document.addEventListener('keydown', event => {
      if (!event.defaultPrevented && event.key === 'Escape' && this.active && !document.querySelector('dialog[open]')) { event.preventDefault(); this.close(); }
    });
  }
  message(text, error = false) { this.status.textContent = text; this.status.classList.toggle('error-text', error); }
  open(task, { history = true } = {}) {
    if (this.callbacks.beforeOpen() === false) return false;
    if (this.active) this.dismiss();
    this.task = { ...task }; this.active = true; this.busy = false; this.closing = false;
    this.abort = new AbortController(); this.generation++;
    if (history) this.callbacks.history.push(taskSettingsURL(location.href, task.id), { taskSettings: true });
    this.hidden = [...$('main').children].filter(child => child !== this.panel).map(child => [child, child.inert]);
    for (const [child] of this.hidden) child.inert = true;
    this.panel.hidden = false; $('main').classList.add('task-settings-open'); this.callbacks.opened(); this.back.focus({ preventScroll: true });
    return this.load();
  }
  async optional(path, signal, fallback) {
    try { return await this.api.json(path, { signal }); }
    catch (error) {
      if (signal.aborted || [401, 403].includes(error.status)) throw error;
      log('task-settings', 'optional read failed', { status: error.status || 0 }); return fallback;
    }
  }
  async load() {
    const generation = this.generation, id = this.task.id, version = ++this.readVersion;
    this.readAbort?.abort(); this.readAbort = new AbortController();
    const signal = AbortSignal.any([this.abort.signal, this.readAbort.signal, AbortSignal.timeout(15000)]);
    this.message('Loading task…');
    try {
      const base = '/api/tasks/' + encodeURIComponent(id);
      const [raw, dto, accounts, backends, meta, links] = await Promise.all([
        this.api.json(base + '/raw', { signal }), this.optional(base, signal, this.task),
        this.optional('/api/accounts?project=' + encodeURIComponent(this.task.project || ''), signal, {}),
        this.optional('/api/backends', signal, {}), this.optional(base + '/meta?namespace=pr', signal, {}), this.optional(base + '/links', signal, {}),
      ]);
      if (!this.active || generation !== this.generation || version !== this.readVersion || this.abort.signal.aborted) return;
      this.task = { ...dto, ...raw, worktree_path: raw.worktree || dto.worktree_path };
      this.fields = taskSettingsForm(this.host, { task: this.task, accounts: accountList(accounts), backends: backends.backends || [], entries: meta.entries || [], links: links.links || [] },
        (operation, body) => this.mutate(operation, body), (operation, body) => this.confirm(operation, body), task => this.callbacks.archive(task), task=>this.callbacks.switchSession?.(task));
      this.fields.disabled = !!this.busy; this.message(''); log('task-settings', 'loaded', { count: 1 }); return true;
    } catch (error) {
      if (!this.active || generation !== this.generation || version !== this.readVersion || this.abort.signal.aborted) return;
      this.message(`Task settings unavailable. ${error.message}`, true);
      this.host.replaceChildren(button('Retry task settings', () => this.load())); log('task-settings', 'load failed', { status: error.status || 0 }); return false;
    }
  }
  confirm(operation, body) {
    if (this.busy) return;
    const task = { ...this.task }, generation = this.generation;
    this.dialog = confirmTaskAction(task, operation, () => { if (this.active && generation === this.generation && this.task.id === task.id) return this.mutate(operation, body); }, this.abort.signal);
  }
  async mutate(operation, body) {
    if (this.busy || !this.active) return;
    this.busy = true; this.fields.disabled = true;
    const generation = this.generation, task = { ...this.task }, signal = AbortSignal.any([this.abort.signal, AbortSignal.timeout(120000)]);
    this.message('Saving…');
    try {
      const base = '/api/tasks/' + encodeURIComponent(task.id);
      const result = await this.api.json(operation === 'delete' ? base : base + '/' + operation, {
        method: operation === 'delete' ? 'DELETE' : 'POST', signal,
        ...(body ? { headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) } : {}),
      });
      if (!this.active || generation !== this.generation || this.abort.signal.aborted) return;
      log('task-settings', 'updated', { status: true });
      if (operation === 'delete') { this.leaveForTask(); await this.callbacks.changed({ deleted: task.id }); }
      else if (operation === 'fork') {
        let created = { project: task.project, backend: task.backend, model: task.model, ...result, live: false };
        try { created = await this.api.json('/api/tasks/' + encodeURIComponent(result.id), { signal }); }
        catch (error) { if (this.abort.signal.aborted) return; log('task-settings', 'fork details unavailable', { status: error.status || 0 }); }
        if (!this.active || generation !== this.generation || this.abort.signal.aborted) return;
        this.leaveForTask(); this.callbacks.created(created);
      } else {
        await this.callbacks.changed();
        if (!this.active || generation !== this.generation || this.abort.signal.aborted) return;
        if (operation === 'rename') this.task.name = body.name;
        else if (operation === 'status') this.task.status = body.status;
        else {
          const loaded = await this.load();
          if (!loaded || !this.active || generation !== this.generation || this.abort.signal.aborted) return;
        }
        this.message('Saved task.');
      }
    } catch (error) {
      if (!this.active || generation !== this.generation || this.abort.signal.aborted) return;
      this.message(`Change was not confirmed. Your edits are kept. Refresh the task before retrying. ${error.message}`, true);
      log('task-settings', 'update failed', { status: error.status || 0 });
    } finally { if (generation === this.generation) { this.busy = false; if (this.fields) this.fields.disabled = false; } }
  }
  close() {
    if (!this.active || this.closing) return;
    if (window.history.state?.taskSettings) { this.closing = true; window.history.back(); }
    else { this.leaveForTask(); this.callbacks.restore(); }
  }
  leaveForTask() {
    if (!this.active) return;
    this.callbacks.history.replace(taskSettingsURL(location.href, null)); this.dismiss();
  }
  dismiss() {
    if (!this.active) return;
    this.active = false; this.generation++; this.abort.abort(); this.readAbort?.abort(); this.dialog?.close(); this.dialog?.remove(); this.dialog = null;
    this.task = null; this.fields = null; this.closing = false;
    this.panel.hidden = true; this.host.replaceChildren(); this.message(''); $('main').classList.remove('task-settings-open');
    for (const [child, inert] of this.hidden) child.inert = inert;
    this.callbacks.closed();
  }
}
