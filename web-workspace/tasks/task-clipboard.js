import {button} from '../shared/dom.js';
import {copyNotice} from '../shared/clipboard.js';
import {log} from '../core/api.js';

export class TaskClipboard {
  constructor(api, host, restoreFocus) {
    Object.assign(this, {api, restoreFocus, revision: 0, text: null});
    this.control = button('Copy staged text', () => this.copy());
    this.control.hidden = true;
    host.prepend(this.control);
  }

  select(task) {
    if (this.id === task?.id) return;
    this.abort?.abort(); this.revision++;
    this.id = task?.id;
    this.render(null);
    if (task) return this.load();
  }

  update(id, value) {
    if (id !== this.id) return;
    if (value === undefined) return this.load();
    this.revision++;
    this.render(value);
  }

  async load() {
    this.abort?.abort(); this.abort = new AbortController();
    const revision = ++this.revision, signal = this.abort.signal;
    try {
      const response = await this.api.request(`/api/tasks/${encodeURIComponent(this.id)}/clipboard`, {
        signal: AbortSignal.any([signal, AbortSignal.timeout(10000)]),
      });
      const value = response.status === 204 ? null : (await response.json()).text;
      if (signal.aborted || revision !== this.revision) {
        log('staged-clipboard', 'stale read skipped', {stale: true}); return;
      }
      if (value !== null && typeof value !== 'string') throw new Error('Invalid staged text');
      this.render(value);
      log('staged-clipboard', 'loaded', {status: value !== null});
    } catch (error) {
      if (!signal.aborted && revision === this.revision) log('staged-clipboard', 'load failed', {status: error.status || 0});
    }
  }

  render(text) {
    const focused = document.activeElement === this.control;
    this.text = text;
    this.control.hidden = text === null;
    if (text === null) delete this.control.dataset.tooltip;
    else this.control.dataset.tooltip = text.length ? text.slice(0, 120).replace(/\s+/g, ' ') + (text.length > 120 ? '…' : '') : 'Empty staged text';
    if (text === null && focused) this.restoreFocus();
  }

  copy() {
    if (this.text === null) return;
    return copyNotice(this.text, 'Copied staged text.');
  }
}
