import { log } from '../core/api.js';
import { submitAttachment } from './terminal-submit.js';
import { composePaste, isInteractive, requestSignal } from './terminal-policy.js';

export class TerminalInput {
  constructor(api, task, signal, ready, bracketed = () => true, observe = () => null) {
    Object.assign(this, { api, task, signal, ready, bracketed, observe });
    this.path = '/api/tasks/' + encodeURIComponent(task.id);
    this.chain = Promise.resolve();
  }

  assertReady() {
    if (this.signal.aborted || !this.ready()) throw new Error('Terminal is not connected');
  }

  enqueue(operation) {
    const pending = this.chain.then(operation);
    this.chain = pending.catch(() => {});
    return pending;
  }

  async post(data) {
    this.assertReady();
    const sanitized = data.replace(/\x1a/g, '');
    if (sanitized !== data) log('terminal-input','Ctrl+Z dropped',{bytes:data.length-sanitized.length});
    if (!sanitized) return;
    const task = this.api.inputTask(this.task.id);
    if (!task || task.archived || task.archive_pending) {
      log('terminal-input', 'workspace refused input');
      throw new Error('Task input is blocked until workspace reconciliation');
    }
    await this.api.request(this.path + '/input', {
      method: 'POST', headers: { 'Content-Type': 'text/plain' }, body: sanitized, signal: requestSignal(this.signal),
    });
  }

  raw(data) {
    this.assertReady();
    if (new TextEncoder().encode(data).length > 64 * 1024) return Promise.reject(new Error('Input exceeds 64 KB'));
    return this.enqueue(() => this.post(data));
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
