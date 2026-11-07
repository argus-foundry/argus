import { decodeOutput, retainedOutput, isInteractive, outputCursor, requestSignal, terminalSize } from './terminal-policy.js';
import { log } from '../core/api.js';

const MAX_PENDING_BYTES = 2 * 1024 * 1024;

export class TerminalStream {
  constructor(api, task, signal, hooks) {
    Object.assign(this, { api, task, signal, hooks, cursor: 0, attempts: 0, version: 0 });
    this.path = '/api/tasks/' + encodeURIComponent(task.id);
    this.active = true;
    this.pendingBytes = 0;
    this.pendingWrites = new Set();
    signal.addEventListener('abort', () => this.destroy(), { once: true });
  }

  async start() {
    const version = ++this.version;
    await this.snapshot(undefined, undefined, Infinity, version);
    if (!this.active || version !== this.version) return;
    if (isInteractive(this.task)) this.open();
    else this.hooks.state('Recorded output', 'recorded', false);
  }

  async snapshot(size, task, deadline = Infinity, version = this.version) {
    let bytes = new Uint8Array(), cursor = 0;
    try {
      const signal = requestSignal(this.signal, Math.max(1, Math.min(10000, deadline - Date.now())));
      const response = await this.api.request(this.path + '/output?bytes=1048576', { signal });
      bytes = new Uint8Array(await response.arrayBuffer());
      if (!this.active || version !== this.version) return;
      cursor = outputCursor(response.headers.get('X-Output-Total'));
    } catch (error) {
      if (!this.active || version !== this.version) return;
      if (error.status !== 404) throw error;
    }
    if (!this.active || version !== this.version) return;
    this.needsReplacement = true;
    if (size) await this.hooks.replace(bytes, size, task);
    else await this.hooks.write(bytes);
    if (!this.active || version !== this.version) return;
    this.cursor = cursor;
    this.needsReplacement = false;
    log('terminal', 'snapshot loaded', { bytes: bytes.byteLength });
  }

  async size(task, signal) {
    try { return await this.api.json(this.path + '/size', { signal }); }
    catch (error) {
      if (error.status !== 404 || isInteractive(task)) throw error;
      return this.hooks.currentSize?.() || { cols: 80, rows: 24 };
    }
  }

  async refresh(task, deadline = Infinity, version = ++this.version) {
    this.hooks.state('Refreshing terminal history…', 'reconnecting', false);
    const signal = requestSignal(this.signal, Math.max(1, Math.min(10000, deadline - Date.now())));
    const size = await this.size(task, signal);
    if (!this.active || version !== this.version) return;
    if (!terminalSize(size)) throw new Error('Invalid terminal dimensions');
    await this.snapshot(size, task, deadline, version);
    if (!this.active || version !== this.version) return;
    this.task = task;
    this.hooks.updateTask?.(task);
    if (isInteractive(task)) this.open();
    else this.hooks.state('Session ended', 'ended', false);
  }

  open() {
    if (!this.active || this.suspended) return;
    this.close();
    const source = new EventSource(this.api.streamUrl(this.path + '/stream?since=' + this.cursor));
    this.source = source;
    let opened = false;
    const current = () => this.active && this.source === source;
    const healthy = () => {
      if (!current()) return;
      clearTimeout(this.healthyTimer);
      if (this.attempts) log('terminal', 'stream healthy; retry budget restored');
      this.attempts = 0;
    };
    const fail = () => {
      if (!current()) return;
      this.close();
      this.reconnect();
    };
    this.connectionTimer = setTimeout(fail, 10000);
    source.onopen = () => {
      if (!current()) return;
      clearTimeout(this.connectionTimer);
      opened = true;
      this.healthyTimer = setTimeout(healthy, 10000);
      this.hooks.state('Live terminal', 'live', true);
      this.hooks.clipboardRefresh?.();
      log('terminal', 'stream connected');
    };
    source.onmessage = event => {
      if (!current() || !opened) return;
      try {
        const bytes = decodeOutput(event.data);
        if (!Number.isSafeInteger(this.cursor + bytes.byteLength)) throw new Error('Cursor overflow');
        if (!bytes.byteLength) return;
        if (this.pendingBytes + bytes.byteLength > MAX_PENDING_BYTES) {
          this.pauseForDrain();
          return;
        }
        this.pendingBytes += bytes.byteLength;
        const writing = Promise.resolve(this.hooks.write(bytes));
        this.pendingWrites.add(writing);
        writing.finally(() => {
          this.pendingBytes -= bytes.byteLength;
          this.pendingWrites.delete(writing);
        });
        this.cursor += bytes.byteLength;
        if (bytes.byteLength) healthy();
      } catch {
        this.close();
        this.hooks.state('Invalid terminal stream. Use Reconnect to try again.', 'error', false);
        log('terminal', 'invalid output frame');
      }
    };
    source.addEventListener('clipboard', event => {
      if (!current()) return;
      try {
        const value = JSON.parse(event.data);
        if (value?.cleared === true) this.hooks.clipboardRefresh?.();
        else if (typeof value?.text === 'string') this.hooks.clipboard?.(value.text);
        else throw new Error('Invalid clipboard frame');
        log('staged-clipboard', value.cleared === true ? 'presence refresh requested' : 'stream updated');
      } catch { log('staged-clipboard', 'invalid frame'); }
    });
    source.addEventListener('exit', event => {
      if (!current()) return;
      this.close();
      let rerendering = Date.now() < (this.expectRerenderUntil || 0);
      try { rerendering ||= JSON.parse(event.data).rerendering === true; } catch { /* Older payload. */ }
      if (rerendering) this.beginRecovery();
      else { this.recovering = false; this.hooks.state('Session ended', 'ended', false); }
    });
    source.onerror = fail;
  }

  reconnect() {
    clearTimeout(this.timer);
    this.recovering = false;
    const version = ++this.version;
    if (++this.attempts > 6) {
      this.recovering = false;
      this.hooks.state('Disconnected. Use Reconnect to try again.', 'error', false);
      log('terminal', 'reconnect limit reached');
      return;
    }
    this.hooks.state('Reconnecting…', 'reconnecting', false);
    log('terminal', 'retrying stream');
    this.timer = setTimeout(async () => {
      try {
        const task = await this.api.json(this.path, { signal: requestSignal(this.signal) });
        if (!this.active || version !== this.version) return;
        if (isInteractive(task)) await this.refresh(task, Infinity, version);
        else if (task.status === 'in_progress') this.reconnect();
        else { this.recovering = false; await this.refresh(task, Infinity, version); }
      } catch { if (this.active && version === this.version) this.reconnect(); }
    }, Math.min(500 * 2 ** (this.attempts - 1), 4000));
  }

  beginRecovery() {
    if (this.recovering || !this.active) return;
    this.version++;
    this.recovering = true;
    this.expectRerenderUntil = 0;
    this.close();
    clearTimeout(this.timer);
    this.recover(Date.now() + 15000);
  }

  recover(deadline) {
    const version = this.version;
    this.hooks.state('Waiting for terminal rerender…', 'reconnecting', false);
    if (Date.now() >= deadline) {
      this.recovering = false;
      this.hooks.state('Terminal restart timed out. Use Reconnect to try again.', 'error', false);
      log('terminal', 'rerender recovery limit reached');
      return;
    }
    this.timer = setTimeout(async () => {
      try {
        const task = await this.api.json(this.path, { signal: requestSignal(this.signal, Math.max(1, deadline - Date.now())) });
        if (!this.active || version !== this.version) return;
        if (isInteractive(task)) {
          await this.refresh(task, deadline, version);
          if (this.active && version === this.version) this.recovering = false;
          return;
        }
        if (task.status !== 'in_progress') {
          this.recovering = false;
          await this.refresh(task, deadline, version);
          return;
        }
      } catch { if (!this.active || version !== this.version) return; }
      this.recover(deadline);
    }, 1000);
  }

  pauseForDrain() {
    this.suspend();
    const version = this.version;
    this.hooks.state('Catching up terminal output…', 'reconnecting', false);
    log('terminal', 'output pressure; stream paused', { bytes: this.pendingBytes });
    Promise.all([...this.pendingWrites]).then(() => {
      if (this.active && this.version === version) return this.resume();
    });
  }

  suspend() {
    this.suspended=true;this.version++;this.resumeAbort?.abort();this.recovering=false;
    clearTimeout(this.timer);this.close();
  }

  async resume() {
    if(!this.active || !this.suspended)return;
    this.suspended=false;const version=++this.version;
    this.resumeAbort=new AbortController();
    const signal=requestSignal(AbortSignal.any([this.signal,this.resumeAbort.signal]));
    this.hooks.state('Refreshing cached terminal…','connecting',false);
    try {
      const task=await this.api.json(this.path,{signal});
      if(!this.active || version!==this.version)return;
      const [size,response]=await Promise.all([this.size(task,signal),this.api.request(this.path+'/output?bytes=1048576',{signal})]);
      const bytes=new Uint8Array(await response.arrayBuffer()),cursor=outputCursor(response.headers.get('X-Output-Total'));
      if(!this.active || version!==this.version)return;
      if(!terminalSize(size))throw new Error('Invalid terminal dimensions');
      const current=this.hooks.currentSize?.();
      const remaining=this.needsReplacement ? null : retainedOutput(this.cursor,cursor,bytes);
      this.hooks.updateTask?.(task);
      this.needsReplacement=true;
      if(remaining && current?.cols===size.cols && current?.rows===size.rows)await this.hooks.write(remaining);
      else await this.hooks.replace(bytes,size,task);
      if(!this.active || version!==this.version)return;
      this.cursor=cursor;this.task=task;
      this.needsReplacement=false;
      if(isInteractive(task))this.open();else this.hooks.state('Recorded output','recorded',false);
      log('terminal','cached output resumed',{bytes:remaining?.byteLength??bytes.byteLength});
    } catch(error) {
      if(this.active && version===this.version && !this.suspended)this.reconnect();
    }
  }

  close() {
    clearTimeout(this.connectionTimer);
    clearTimeout(this.healthyTimer);
    this.source?.close();
    this.source = null;
  }

  destroy() {
    this.active = false; this.resumeAbort?.abort();
    this.recovering = false;
    clearTimeout(this.timer);
    this.close();
  }
}
