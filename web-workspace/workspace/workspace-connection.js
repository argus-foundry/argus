import { connectionState } from './connection-state.js';
import { $ } from '../shared/dom.js';
import { TokenStorage } from '../core/token-storage.js';
import { readProjects } from './rail/projects.js';
import { readHierarchy } from './rail/task-hierarchy.js';
import { log } from '../core/api.js';

export class WorkspaceConnection {
  constructor(api, callbacks) {
    Object.assign(this, { api, callbacks, storage: new TokenStorage(), retry: '', connected: false });
    $('connect-form').addEventListener('submit', event => {
      event.preventDefault(); const entered = $('token').value.trim(); this.connect(entered || this.retry, !entered && !!this.retry);
    });
    $('disconnect').addEventListener('click', () => this.disconnect());
    $('connect-dialog').addEventListener('cancel', event => event.preventDefault());
    window.addEventListener('pageshow', event => { if (event.persisted) location.reload(); });
    window.addEventListener('pagehide', () => this.suspend());
    document.addEventListener('visibilitychange', () => { if (this.timer && this.connected && !document.hidden) this.callbacks.refresh(); });
  }
  start() { $('connect-dialog').showModal(); this.retry = this.storage.read(); if (this.retry) this.connect(this.retry, true); }
  async connect(token, fromSaved = false) {
    if ($('connect-button').disabled) return;
    connectionState('Connecting to Argus', 'connecting');
    $('connect-button').disabled = true; $('connect-button').textContent = 'Connecting…'; $('connect-error').hidden = true;
    const remember = $('remember-token').checked; this.retry = ''; this.api.setToken(token); $('token').value = '';
    const abort = this.abort = new AbortController(), timeout = setTimeout(() => abort.abort(), 15000);
    try {
      await this.callbacks.openEvents?.();
      const [result, projects, hierarchy] = await Promise.all([
        this.api.json('/api/tasks?archived=all', { signal: abort.signal }), readProjects(this.api, abort.signal), readHierarchy(this.api, abort.signal),
      ]);
      if (abort.signal.aborted) return;
      this.storage.connected(token, remember); this.connected = true;
      $('disconnect').hidden = false; $('connect-dialog').close(); connectionState('Connected to Argus', 'connected');
      this.callbacks.connected(result.tasks || [], projects, hierarchy);
      if (!this.callbacks.openEvents) this.timer = setInterval(() => { if (!document.hidden) this.callbacks.refresh(); else log('connection', 'skipped hidden poll'); }, 8000); log('connection', 'connected', { count: result.tasks?.length || 0 });
    } catch (error) {
      if (this.abort !== abort) return;
      this.callbacks.closeEvents?.(); this.api.setToken(''); const rejected = this.storage.rejected(error); if (fromSaved && !rejected) this.retry = token;
      $('connect-error').textContent = fromSaved && rejected ? 'Saved token was rejected. Enter a valid token.' : error.name === 'AbortError' ? 'Connection timed out. Check that the daemon is reachable, then retry.' : `Could not connect. ${error.message}`;
      connectionState(rejected ? 'Token rejected' : 'Connection unavailable', 'error');
      $('connect-error').hidden = false; $('token').focus(); log('connection', 'failed', { status: error.status || 'error' });
    } finally { clearTimeout(timeout); this.abort = null; $('connect-button').disabled = false; $('connect-button').textContent = this.retry ? 'Retry connection' : 'Connect to Argus'; }
  }
  disconnect() {
    this.storage.clear(); this.suspend(); this.connected = false; this.callbacks.disconnected();
    $('disconnect').hidden = true; $('token').value = ''; connectionState('Not connected');
    $('connect-dialog').showModal(); log('connection', 'disconnected', { status: 'ok' });
  }
  suspend() { this.callbacks.closeEvents?.(); this.abort?.abort(); this.abort = null; clearInterval(this.timer); this.timer = null; this.api.setToken(''); this.retry = ''; }
}
