import { log } from '../core/api.js';
import { submitAttachment } from './terminal-submit.js';
import { composePaste, isInteractive, requestSignal } from './terminal-policy.js';

export class TerminalInput {
  constructor(api, task, signal, ready, bracketed = () => true, observe = () => null) {
    Object.assign(this, { api, task, signal, ready, bracketed, observe });
    this.path = '/api/tasks/' + encodeURIComponent(task.id);
    this.chain = Promise.resolve();
    this.inputSequence = 0;
    this.backendAttempts = 0;
    signal.addEventListener('abort', () => {
      clearTimeout(this.backendRetry);
      this.discardFocus();
    }, {once: true});
    if (!signal.aborted) this.resolveBackend();
  }

  assertReady() {
    if (this.signal.aborted || !this.ready()) throw new Error('Terminal is not connected');
  }

  enqueue(operation) {
    const pending = this.chain.then(operation);
    this.chain = pending.catch(() => {});
    return pending;
  }

  async post(data, sequence = ++this.inputSequence) {
    this.assertReady();
    const sanitized = data.replace(/\x1a/g, '');
    if (sanitized !== data) log('terminal-input','Ctrl+Z dropped',{bytes:data.length-sanitized.length});
    if (!sanitized) return;
    const task = this.api.inputTask(this.task.id);
    if (!task || task.archived || task.archive_pending) {
      log('terminal-input', 'workspace refused input');
      throw new Error('Task input is blocked until workspace reconciliation');
    }
    this.staleFocus(sequence);
    try { await this.api.request(this.path + '/input', {
      method: 'POST', headers: { 'Content-Type': 'text/plain' }, body: sanitized, signal: requestSignal(this.signal),
    }); } catch (error) {
      if (error.status === 409 && error.code === 'task_archived') {
        this.api.inputRejected?.(this.task.id);
        log('terminal-input', 'archived input rejected', {status:409});
      }
      throw error;
    }
  }

  async resolveBackend() {
    this.backendAttempts++;
    try {
      const {backends = []} = await this.api.json('/api/backends', {signal: requestSignal(this.signal)});
      if (this.signal.aborted) return;
      const backend = backends.find(backend => backend.name === this.task.backend);
      this.claudeFamily = backend?.kind === 'claude' || backend?.kind === '';
      log('terminal-input','backend resolved', {count: backends.length});
      this.releaseFocus();
    } catch (error) {
      if (this.signal.aborted) return;
      log('terminal-input','backend unavailable', {status: error.status});
      if (this.backendAttempts < 3) {
        const delay = [250,1000][this.backendAttempts - 1];
        this.backendRetry = setTimeout(() => {
          if (!this.signal.aborted) this.resolveBackend();
        }, delay);
        log('terminal-input','backend retry scheduled', {attempt: this.backendAttempts});
      } else log('terminal-input','backend retry budget exhausted', {attempt: this.backendAttempts});
    }
  }

  discardFocus() {
    if (!this.heldFocus) return;
    clearTimeout(this.heldFocus.timer);
    this.heldFocus = null;
    log('terminal-input','held focus-out discarded');
  }

  // A queued focus-in must not invalidate a newer blur.
  staleFocus(sequence) {
    if (this.heldFocus && sequence > this.heldFocus.sequence) this.heldFocus.stale = true;
  }

  holdFocus() {
    if (this.heldFocus && !this.heldFocus.stale) return;
    this.discardFocus();
    const held = this.heldFocus = {stale: false, sequence: this.inputSequence};
    held.timer = setTimeout(() => this.releaseFocus(), 2000);
    log('terminal-input','focus-out held pending backend');
  }

  releaseFocus() {
    const held = this.heldFocus;
    if (!held) return;
    clearTimeout(held.timer);
    if (this.signal.aborted || this.claudeFamily) { this.discardFocus(); return; }
    if (held.releasing) return;
    held.releasing = true;
    this.enqueue(async () => {
      if (this.heldFocus !== held) return;
      this.heldFocus = null;
      if (held.stale) { log('terminal-input','stale focus-out discarded'); return; }
      await this.post('\x1b[O');
      log('terminal-input','held focus-out forwarded');
    }).catch(error => log('terminal-input','held focus-out failed', {status: error.status}));
  }

  raw(data) {
    this.assertReady();
    if (data === '\x1b[O') {
      if (this.claudeFamily) { log('terminal-input','focus-out report dropped'); return Promise.resolve(); }
      if (this.claudeFamily === undefined) { this.holdFocus(); return Promise.resolve(); }
    }
    if (new TextEncoder().encode(data).length > 64 * 1024) return Promise.reject(new Error('Input exceeds 64 KB'));
    const sequence = ++this.inputSequence;
    if (data.replace(/\x1a/g, '')) this.staleFocus(sequence);
    return this.enqueue(() => this.post(data, sequence));
  }

  async verifyLive() {
    this.assertReady();
    const task = await this.api.json(this.path, { signal: requestSignal(this.signal) });
    this.assertReady();
    if (!isInteractive(task)) throw new Error('Agent is no longer running');
  }

  send(text, {attachments = false, imagePaths = []} = {}) {
    this.assertReady();
    composePaste(text, this.bracketed());
    return this.enqueue(async () => {
      await this.verifyLive();
      if (attachments) return submitAttachment(this, text, imagePaths);
      await this.post(composePaste(text, this.bracketed()));
      await new Promise((resolve, reject) => {
        const cancel = () => { clearTimeout(timer); reject(new Error('Message submit canceled')); };
        const timer = setTimeout(() => { this.signal.removeEventListener('abort', cancel); resolve(); }, 50);
        this.signal.addEventListener('abort', cancel, { once: true });
        if (this.signal.aborted) cancel();
      });
      await this.verifyLive();
      await this.post('\r');
    });
  }
}
