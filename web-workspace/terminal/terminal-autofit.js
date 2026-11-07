import { terminalFitNeeded, terminalSize } from './terminal-policy.js';
import { log } from '../core/api.js';

export class TerminalAutoFit {
  constructor(context) { Object.assign(this,context,{active:true,version:0,skipped:false}); this.timers ||= globalThis; }
  signature() { return JSON.stringify([this.measure(),this.current(),this.allowed(false)]); }
  check() { if (this.signature() !== this.key) this.settle(); }
  settle() {
    if (!this.active) return;
    this.key = this.signature(); this.version++; this.timers.clearTimeout(this.timer); this.skipped = false;
    if (terminalFitNeeded(this.current(),this.measure())) {
      if (this.allowed(false)) {
        const version = this.version;
        this.timer = this.timers.setTimeout(() => { this.timer = null; this.request(false,version).catch(() => {}); },400);
      } else { this.skipped = true; log('terminal-fit','automatic fit deferred',{stale:true}); }
    }
    this.changed();
  }
  async request(manual,version) {
    if (!this.active || (!manual && version !== this.version)) return;
    if (this.pending) { await this.pending.catch(() => {}); return this.request(manual,version); }
    if (!this.allowed(manual)) { this.skipped = true; this.changed(); if (manual) throw new Error('Show a connected live terminal before fitting'); return; }
    const size = this.measure();
    if(!terminalSize(size)){this.skipped=true;this.changed();if(manual)throw new Error('Terminal pane measurement is unavailable or too small');return;}
    if (!manual && !terminalFitNeeded(this.current(),size)) return;
    const context = { automatic:!manual, current:() => this.active && (manual || version === this.version) };
    const pending = Promise.resolve().then(() => this.resize(size,context)); this.pending = pending; this.changed();
    try {
      const applied = await pending;
      if (this.active) { this.skipped = applied === false; this.key = this.signature(); }
      return applied;
    } catch (error) {
      if (this.active) { this.skipped = true; this.version++; log('terminal-fit','automatic or manual fit failed',{status:false}); }
      throw error;
    } finally { if (this.pending === pending) this.pending = null; if (this.active) this.changed(); }
  }
  manual() { this.version++; this.timers.clearTimeout(this.timer); return this.request(true,this.version); }
  destroy() { this.active = false; this.version++; this.timers.clearTimeout(this.timer); }
}
