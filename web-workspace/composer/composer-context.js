import {captureFocus} from '../shared/focus-recovery.js';
import {SessionUsage} from './session-usage.js';
import { $, node } from '../shared/dom.js';
import { log } from '../core/api.js';
import { readContext } from './composer-context-read.js';
import { contextSnapshot, tokenCount } from './composer-context-policy.js';

const pct = value => `${Number(value.toFixed(1))}%`;
const date = value => new Date(value).toLocaleString(undefined, { dateStyle:'medium', timeStyle:'short' });

export class ComposerContext {
  constructor(api) {
    this.sessionUsage = new SessionUsage(api,()=>this.render());
    this.api = api; this.toggle = $('context-toggle');
    if (!this.toggle) return;
    this.panel = node('section','context-popover'); this.panel.id = 'composer-context'; this.panel.tabIndex = 0;
    this.panel.setAttribute('popover','auto'); this.panel.setAttribute('autofocus',''); this.panel.setAttribute('aria-label','Context and usage');
    this.toggle.setAttribute('popovertarget',this.panel.id);
    this.toggle.setAttribute('aria-controls',this.panel.id); this.toggle.setAttribute('aria-expanded','false');
    $('composer').append(this.panel);
    this.panel.addEventListener('beforetoggle', event => this.toggle.setAttribute('aria-expanded',String(event.newState === 'open')));
    this.panel.addEventListener('toggle', event => {
      const open = event.newState === 'open';
      if (open) { this.position(); this.refresh(); }
    });
    window.addEventListener('resize', () => this.position());
    document.addEventListener('visibilitychange', () => { if (!document.hidden) this.refresh(); });
  }
  cancelReads() {
    this.controller?.abort(); this.usageController?.abort(); clearTimeout(this.eventTimer);
    this.contextRead = null; this.usageRead = null; this.contextQueued = false;
  }
  select(task) {
    this.cancelReads(); clearInterval(this.timer);
    this.sessionUsage?.select(task); this.panel?.hidePopover(); this.task = task; this.sessionID = task?.session_id; this.data = {measurement:null}; this.key = '';
    if (!this.toggle) return;
    this.toggle.disabled = !task; this.render();
    if (task && this.api) {
      this.refresh(); this.timer = setInterval(() => this.refresh(),15000);
    }
  }
  invalidate(sessionID) {
    this.cancelReads(); this.sessionID = sessionID;
    this.data = {raw:this.data.raw,measurement:null}; this.render();
    return this.refresh();
  }
  event(event) {
    this.sessionUsage?.event(event);
    if (event.task_id !== this.task?.id) return;
    if (event.type === 'task.context_changed') {
      const sessionID = event.payload?.session_id, previous = this.data.measurement?.session_id || this.sessionID;
      if (sessionID && previous && sessionID !== previous) { this.invalidate(sessionID); return; }
      if (sessionID && !previous) this.sessionID = sessionID;
      if (this.eventTimer) log('composer-context','context event coalesced');
      clearTimeout(this.eventTimer);
      this.eventTimer = setTimeout(() => { this.eventTimer = null; this.refreshMeasurement(true); },100);
    } else if (['session.started','session.exited'].includes(event.type)) this.invalidate();
  }
  identity(raw) { this.data.raw = raw; this.render(); }
  refresh() { return Promise.all([this.refreshMeasurement(),this.refreshUsage(),this.sessionUsage?.refresh()]); }
  canRead() {
    if (!this.task || !this.api) return false;
    if (document.hidden) { log('composer-context','skipped hidden poll'); return false; }
    return true;
  }
  refreshMeasurement(queue = false) {
    if (!this.canRead()) return Promise.resolve();
    if (this.contextRead) { this.contextQueued ||= queue; log('composer-context','measurement refresh coalesced'); return this.contextRead; }
    this.controller?.abort();
    const controller = this.controller = new AbortController(), task = this.task;
    const read = this.readMeasurement(task,controller).finally(() => {
      if (this.contextRead !== read) return;
      this.contextRead = null;
      if (this.contextQueued) { this.contextQueued = false; this.refreshMeasurement(); }
    });
    this.contextRead = read; return read;
  }
  async readMeasurement(task,controller) {
    let measurement = null, meta;
    const signal = AbortSignal.any([controller.signal,AbortSignal.timeout(10000)]);
    try {
      measurement = await readContext(this.api,task.id,signal);
      if (measurement === undefined) {
        log('composer-context','legacy context fallback');
        meta = await this.api.json(`/api/tasks/${encodeURIComponent(task.id)}/meta?namespace=hera`,{signal});
      }
    } catch (error) { log('composer-context','measurement unavailable',{status:error.status}); }
    if (this.controller !== controller || controller.signal.aborted || this.task !== task) { log('composer-context','obsolete readings ignored',{stale:true}); return; }
    const data = {...this.data}; delete data.meta;
    if (measurement === undefined) { delete data.measurement; if (meta) data.meta = meta; }
    else data.measurement = measurement;
    if (this.sessionID && data.measurement) {
      if (data.measurement.session_id !== this.sessionID) {
        data.measurement = null; log('composer-context','obsolete session measurement ignored',{stale:true});
      }
      this.sessionID = undefined;
    }
    this.data = data; this.render(); log('composer-context','measurement refreshed',{count:measurement ? 1 : 0});
  }
  refreshUsage() {
    if (!this.canRead()) return Promise.resolve();
    if (this.usageRead) { log('composer-context','usage refresh coalesced'); return this.usageRead; }
    this.usageController?.abort();
    const controller = this.usageController = new AbortController(), task = this.task;
    const read = this.readUsage(task,controller).finally(() => { if (this.usageRead === read) this.usageRead = null; });
    this.usageRead = read; return read;
  }
  async readUsage(task,controller) {
    const signal = AbortSignal.any([controller.signal,AbortSignal.timeout(10000)]);
    const paths = {bridge:`/api/tasks/${encodeURIComponent(task.id)}/bridge`,accounts:'/api/accounts',backends:'/api/backends'};
    const results = await Promise.allSettled(Object.entries(paths).map(async ([key,path]) => [key,await this.api.json(path,{signal})]));
    if (this.usageController !== controller || controller.signal.aborted || this.task !== task) { log('composer-context','obsolete usage ignored',{stale:true}); return; }
    const data = {...this.data}; for (const key of Object.keys(paths)) delete data[key];
    let count = 0;
    for (const result of results) {
      if (result.status === 'fulfilled') { data[result.value[0]] = result.value[1]; count++; }
      else log('composer-context','usage unavailable',{status:result.reason?.status});
    }
    this.data = data; this.render(); log('composer-context','usage refreshed',{count});
  }
  row(label,value) {
    const row = node('div','context-row'); row.append(node('span','',label),node('span','',value)); return row;
  }
  bar(value,label) {
    const bar = node('progress','context-progress'); bar.max = 100; bar.value = value;
    bar.setAttribute('aria-label',label); return bar;
  }
  render() {
    if (!this.panel) return;
    const view = this.task ? contextSnapshot(this.task,this.data) : {context:null,windows:[],identity:[]};
    const usage=this.sessionUsage?.render();
    const key = JSON.stringify(view); if (this.key === key) return; this.key = key;
    const context = view.context, fill = context?.percent;
    const ring = this.toggle.querySelector('.context-ring');
    ring.dataset.known = String(fill != null); ring.style.setProperty('--context-fill',`${fill ?? 0}%`);
    this.toggle.setAttribute('aria-label',fill == null ? 'Context usage unavailable. Open usage details' : `Context ${pct(fill)} used${context.recorded ? ', last recorded' : ''}. Open usage details`);
    this.toggle.dataset.tooltip = this.toggle.getAttribute('aria-label');
    if(!this.contextBody){this.contextBody=node('div','context-measurement');this.panel.append(this.contextBody);}
    if(usage&&!usage.isConnected)this.panel.append(usage);
    const recover=captureFocus(this.contextBody,()=>this.toggle);
    this.contextBody.replaceChildren();
    const heading = node('div','context-heading'); heading.append(node('h2','','Context'));
    if (context?.used != null) heading.append(node('span','',`${tokenCount(context.used)}${context.limit ? ` / ${tokenCount(context.limit)}` : ' tokens'}`));
    this.contextBody.append(heading);
    if (fill != null) this.contextBody.append(this.bar(fill,'Context window used'),this.row('Window used',pct(fill)));
    if (!context) this.contextBody.append(node('p','context-note','Context window usage is not available for this session.'));
    else {
      if (fill == null) this.contextBody.append(node('p','context-note','Context window limit is not exposed.'));
      this.contextBody.append(node('p','context-note',`${context.recorded ? 'Last recorded' : 'Measured'}${context.age ? ` ${context.age}` : ''}${context.at ? ` (${date(context.at)})` : ''}`));
    }
    if (context?.categories?.length) {
      const group = node('div','context-group'); group.append(node('h3','','Context categories'));
      for (const [label,value] of context.categories) group.append(this.row(label,`${tokenCount(value)} tokens`));
      this.contextBody.append(group);
    }
    if (view.windows.length) {
      const group = node('div','context-group'); group.append(node('h3','',view.kind === 'claude' ? 'Claude usage' : 'Usage'));
      for (const window of view.windows) group.append(this.row(window.label,`${pct(window.left)} left`),this.bar(window.left,`${window.label} remaining`),node('p','context-note',`Resets ${date(window.reset)}`));
      this.contextBody.append(group);
    }
    if (view.identity.length) {
      const group = node('div','context-group');
      for (const [label,value] of view.identity) group.append(this.row(label,value));
      this.contextBody.append(group);
    }
    this.position();recover();
  }
  position() {
    if (!this.panel?.matches(':popover-open')) return;
    const rect = this.toggle.getBoundingClientRect(), width = Math.min(360,innerWidth-24);
    Object.assign(this.panel.style,{width:`${width}px`,left:`${Math.max(12,Math.min(rect.right-width,innerWidth-width-12))}px`,bottom:`${innerHeight-rect.top+8}px`,maxHeight:`${Math.max(80,rect.top-20)}px`});
  }
}
