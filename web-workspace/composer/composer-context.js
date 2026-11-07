import { $, node } from '../shared/dom.js';
import { log } from '../core/api.js';
import { contextSnapshot, tokenCount } from './composer-context-policy.js';

const pct = value => `${Number(value.toFixed(1))}%`;
const date = value => new Date(value).toLocaleString(undefined, { dateStyle:'medium', timeStyle:'short' });

export class ComposerContext {
  constructor(api) {
    this.api = api; this.toggle = $('context-toggle');
    if (!this.toggle) return;
    this.panel = node('section','context-popover'); this.panel.id = 'composer-context';
    this.panel.setAttribute('popover','auto'); this.panel.setAttribute('aria-label','Context and usage');
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
  select(task) {
    this.controller?.abort(); clearInterval(this.timer);
    this.panel?.hidePopover(); this.task = task; this.data = {}; this.key = '';
    if (!this.toggle) return;
    this.toggle.disabled = !task; this.render();
    if (task && this.api) {
      this.refresh(); this.timer = setInterval(() => this.refresh(),15000);
    }
  }
  identity(raw) { this.data.raw = raw; this.render(); }
  async refresh() {
    if (!this.task || !this.api) return;
    if (document.hidden) { log('composer-context','skipped hidden poll'); return; }
    this.controller?.abort();
    const controller = this.controller = new AbortController(), task = this.task;
    const paths = {bridge:`/api/tasks/${encodeURIComponent(task.id)}/bridge`,meta:`/api/tasks/${encodeURIComponent(task.id)}/meta?namespace=hera`,accounts:'/api/accounts',backends:'/api/backends'};
    const results = await Promise.allSettled(Object.entries(paths).map(async ([key,path]) => [key,await this.api.json(path,{signal:controller.signal})]));
    if (this.controller !== controller || controller.signal.aborted || this.task !== task) { log('composer-context','obsolete readings ignored',{stale:true}); return; }
    const data = {raw:this.data.raw};
    let count = 0;
    for (const result of results) {
      if (result.status === 'fulfilled') { data[result.value[0]] = result.value[1]; count++; }
      else log('composer-context','reading unavailable',{status:result.reason?.status});
    }
    this.data = data; this.render();
    log('composer-context','readings refreshed',{count});
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
    const key = JSON.stringify(view); if (this.key === key) return; this.key = key;
    const context = view.context, fill = context?.percent;
    const ring = this.toggle.querySelector('.context-ring');
    ring.dataset.known = String(fill != null); ring.style.setProperty('--context-fill',`${fill ?? 0}%`);
    this.toggle.setAttribute('aria-label',fill == null ? 'Context usage unavailable. Open usage details' : `Context ${pct(fill)} used${context.recorded ? ', last recorded' : ''}. Open usage details`);
    this.toggle.dataset.tooltip = this.toggle.getAttribute('aria-label');
    this.panel.replaceChildren();
    const heading = node('div','context-heading'); heading.append(node('h2','','Context'));
    if (context?.used != null) heading.append(node('span','',`${tokenCount(context.used)}${context.limit ? ` / ${tokenCount(context.limit)}` : ' tokens'}`));
    this.panel.append(heading);
    if (fill != null) this.panel.append(this.bar(fill,'Context window used'),this.row('Window used',pct(fill)));
    if (!context) this.panel.append(node('p','context-note','Context window usage is not available for this session.'));
    else {
      if (fill == null) this.panel.append(node('p','context-note','Context window limit is not exposed.'));
      this.panel.append(node('p','context-note',`${context.recorded ? 'Last recorded' : 'Measured'}${context.at ? ` ${date(context.at)}` : ''}`));
    }
    if (view.windows.length) {
      const group = node('div','context-group'); group.append(node('h3','',view.kind === 'claude' ? 'Claude usage' : 'Usage'));
      for (const window of view.windows) group.append(this.row(window.label,`${pct(window.left)} left`),this.bar(window.left,`${window.label} remaining`),node('p','context-note',`Resets ${date(window.reset)}`));
      this.panel.append(group);
    }
    if (view.identity.length) {
      const group = node('div','context-group');
      for (const [label,value] of view.identity) group.append(this.row(label,value));
      this.panel.append(group);
    }
    this.position();
  }
  position() {
    if (!this.panel?.matches(':popover-open')) return;
    const rect = this.toggle.getBoundingClientRect(), width = Math.min(360,innerWidth-24);
    Object.assign(this.panel.style,{width:`${width}px`,left:`${Math.max(12,Math.min(rect.right-width,innerWidth-width-12))}px`,bottom:`${innerHeight-rect.top+8}px`,maxHeight:`${Math.max(80,rect.top-20)}px`});
  }
}
