import { imageFile, uploadedPath, validateFiles } from './attachment-policy.js';
import { log } from '../../core/api.js';

export class AttachmentStore {
  constructor(api, onChange = () => {}) {
    Object.assign(this, { api, onChange, tasks: new Map(), sequence: 0 });
  }
  list(taskId) { return this.tasks.get(taskId) || []; }
  ready(taskId) { return this.list(taskId).filter(item => item.state === 'ready'); }
  blocked(taskId) { return this.list(taskId).some(item => item.state !== 'ready'); }
  add(taskId, files) {
    validateFiles(files, this.list(taskId).map(item => item.file));
    const items = files.map(file => ({ id: ++this.sequence, taskId, file, state: 'uploading', version: 0, url: imageFile(file) ? URL.createObjectURL(file) : null }));
    this.tasks.set(taskId, [...this.list(taskId), ...items]);
    this.onChange();
    for (const item of items) this.upload(item);
  }
  async upload(item) {
    if (!this.list(item.taskId).includes(item) || item.abort && item.state === 'uploading') return;
    const version = ++item.version;
    const current = () => this.list(item.taskId).includes(item) && item.version === version;
    Object.assign(item, { state: 'uploading', error: '', path: null, abort: new AbortController() });
    const timer = item.timer = setTimeout(() => item.abort.abort(), 280000);
    this.onChange();
    try {
      const body = new FormData();
      body.append('files', item.file, item.file.name || 'attachment');
      const result = await this.api.json(`/api/tasks/${encodeURIComponent(item.taskId)}/upload`, { method: 'POST', body, signal: item.abort.signal });
      if (!current()) { log('attachments', 'obsolete upload ignored'); return; }
      item.path = uploadedPath(result);
      item.state = 'ready';
      log('attachments', 'upload complete', { count: 1, bytes: item.file.size });
    } catch (error) {
      if (!current()) return;
      item.state = 'error';
      item.error = item.abort.signal.aborted || error.name === 'AbortError'
        ? 'Upload not confirmed. Check Files before retrying; it may have completed.'
        : `Upload failed. ${error.message} Check Files before retrying.`;
      log('attachments', 'upload failed', { status: error.status || 'error' });
    } finally { clearTimeout(timer); if (current()) this.onChange(); }
  }
  remove(item) {
    const items = this.list(item.taskId);
    if (!items.includes(item)) return;
    this.tasks.set(item.taskId, items.filter(next => next !== item));
    clearTimeout(item.timer);
    item.abort?.abort();
    if (item.url) URL.revokeObjectURL(item.url);
    this.onChange();
  }
  consume(items) { for (const item of items) this.remove(item); }
  clear() {
    const items = [...this.tasks.values()].flat();
    this.tasks.clear();
    for (const item of items) { clearTimeout(item.timer); item.abort?.abort(); if (item.url) URL.revokeObjectURL(item.url); }
    this.onChange();
  }
}
