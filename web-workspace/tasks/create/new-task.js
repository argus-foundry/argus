import {accountList,creationAccounts,creationAccount} from './new-task-account.js';
import { $, node, button } from '../../shared/dom.js';
import { log } from '../../core/api.js';
import { sendsMessage, insertPlaintext } from '../../composer/composer-policy.js';

export class NewTaskView {
  constructor(api, onCreated, onVisibility) {
    Object.assign(this, { api, onCreated, onVisibility });
    this.drafts = new Map(); this.generation = 0;
    this.panel = node('section', 'new-task-view'); this.panel.id = 'new-task-view'; this.panel.hidden = true;
    this.panel.setAttribute('aria-labelledby', 'new-task-title');
    const title = node('h1', '', 'Start a new task'); title.id = 'new-task-title';
    this.form = node('form', 'new-task-form');
    this.projectField = this.field('Project', 'new-task-project', 'input'); this.projectField.readOnly = true;
    this.name = this.field('Name (optional)', 'new-task-name', 'input'); this.name.placeholder = 'Let Argus name this task';
    this.backend = this.field('Backend (optional)', 'new-task-backend', 'select'); this.backend.append(node('option','','Daemon default')); this.backend.firstChild.value='';
    this.account = this.field('Account', 'new-task-account', 'select'); this.account.hidden=true;this.account.previousElementSibling.hidden=true;this.accounts=[];this.backends=[];
    this.account.addEventListener('change',()=>{this.accountPicked=this.account.value;this.capture();});
    this.backend.addEventListener('input',()=>this.renderAccounts());
    this.prompt = this.field('Prompt', 'new-task-prompt', 'textarea'); this.prompt.rows = 7; this.prompt.placeholder = 'What should the agent work on?';
    this.error = node('p', 'error-text'); this.error.id = 'new-task-error'; this.error.setAttribute('role', 'alert'); this.error.hidden = true;
    this.cancel = button('Cancel', () => this.close(), ''); this.cancel.setAttribute('aria-label', 'Cancel new task');
    this.start = button('Start task', () => {}, 'primary'); this.start.type = 'submit';
    const actions = node('div', 'new-task-actions'); actions.append(this.cancel, this.start);
    this.form.append(this.error, actions); this.panel.append(title, this.form); $('main').append(this.panel);
    for (const field of [this.name, this.prompt, this.backend]) field.addEventListener('input', () => { this.capture(); this.render(); });
    this.prompt.addEventListener('compositionstart', () => { this.composing = true; });
    this.prompt.addEventListener('compositionend', () => { this.composing = false; });
    this.prompt.addEventListener('paste', event => {
      if (this.prompt.readOnly) return;
      const text = event.clipboardData?.getData('text/plain');
      if (!text) return;
      event.preventDefault(); this.prompt.focus({ preventScroll: true }); insertPlaintext(this.prompt, text); this.capture(); this.render();
      log('new-task', 'paste inserted', { status: true });
    });
    this.prompt.addEventListener('keydown', event => {
      if (sendsMessage(event, this.composing)) { event.preventDefault(); this.submit(); }
    });
    this.form.addEventListener('submit', event => { event.preventDefault(); this.submit(); });
    this.panel.addEventListener('keydown', event => {
      if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); this.close(); }
    });
  }
  field(labelText, id, tag) {
    const label = node('label', '', labelText); label.htmlFor = id;
    const field = node(tag); field.id = id; this.form.append(label, field); return field;
  }
  get active() { return !this.panel.hidden; }
  open(project, restoreFocus) {
    if (this.active) this.close(false);
    this.project = project; this.restoreFocus = restoreFocus; this.composing = false;
    const draft = this.drafts.get(project) || {};
    this.projectField.value = project; this.name.value = draft.name || ''; this.prompt.value = draft.prompt || ''; this.backend.value = draft.backend || '';
    this.accountPicked=draft.account||'';this.accounts=[];this.renderAccounts();
    this.loadBackends(draft.backend || '');
    this.error.textContent = draft.error || ''; this.error.hidden = !draft.error;
    this.panel.hidden = false; $('main').classList.add('creating-task');
    this.background = [...$('main').children].filter(child => child !== this.panel).map(child => [child, child.inert]);
    for (const [child] of this.background) child.inert = true;
    this.render(); this.onVisibility(true); this.prompt.focus({ preventScroll: true });
    log('new-task', 'opened', { status: true });
  }
  async loadBackends(selected) {
    this.backendAbort?.abort(); const abort=this.backendAbort=new AbortController(), project=this.project;
    const signal=AbortSignal.any([abort.signal,AbortSignal.timeout(10000)]);
    try {
      const results=await Promise.allSettled([this.api.json('/api/backends',{signal}),this.api.json('/api/accounts?project='+encodeURIComponent(project),{signal}),this.api.json('/api/settings',{signal}),this.api.json('/api/projects/full',{signal})]);
      if(abort.signal.aborted || !this.active || this.project!==project)return;
      const result=results[0].status==='fulfilled'?results[0].value:{};
      this.accounts=results[1].status==='fulfilled'?accountList(results[1].value):[];
      this.defaultBackend=results[2].status==='fulfilled'?results[2].value.defaults?.backend||'':'';
      const projectBackend=results[3].status==='fulfilled'?results[3].value.projects?.find(item=>item.name===project)?.backend:'';
      this.defaultBackend=projectBackend||this.defaultBackend;
      this.backends=result.backends||[];
      const value=this.backend.value || selected; this.backend.replaceChildren(node('option','','Daemon default'));this.backend.firstChild.value='';
      for(const backend of this.backends){const option=node('option','',backend.name);option.value=backend.name;this.backend.append(option);}this.backend.value=value;
      this.renderAccounts();
      for(const result of results)if(result.status==='rejected')log('new-task','optional choice read failed');
      log('new-task','choices loaded',{count:this.backends.length});
    }catch(error){if(!abort.signal.aborted)log('new-task','choices failed');}
  }
  renderAccounts(){
    this.usableAccounts=creationAccounts(this.accounts,this.backends,this.backend.value||this.defaultBackend);
    const selected=creationAccount(this.usableAccounts,this.accountPicked);
    this.account.replaceChildren();for(const account of this.usableAccounts){const option=node('option','',account.label||account.name);option.value=account.name;this.account.append(option);}
    this.account.value=selected||'';this.account.hidden=this.account.previousElementSibling.hidden=!selected;
  }

  capture() {
    if (this.active) this.drafts.set(this.project, { name: this.name.value, prompt: this.prompt.value, backend: this.backend.value, account:this.accountPicked, error: this.error.textContent });
  }
  render() {
    this.name.readOnly = this.prompt.readOnly = !!this.pending; this.backend.disabled = this.account.disabled = !!this.pending;
    this.start.disabled = !!this.pending || !this.prompt.value.trim();
    this.start.textContent = this.pending ? 'Starting…' : 'Start task';
  }
  close(restoreFocus = true) {
    if (!this.active) return;
    if (this.pending && !this.confirmed) {
      this.error.textContent = 'Task creation may have completed. Refresh tasks before retrying.';
      this.capture();
    } else if (!this.confirmed) this.capture();
    this.generation++; this.abort?.abort(); this.backendAbort?.abort(); clearTimeout(this.timer); this.pending = false; this.confirmed = false;
    this.panel.hidden = true; $('main').classList.remove('creating-task');
    for (const [child, inert] of this.background) child.inert = inert;
    this.onVisibility(false);
    if (restoreFocus) this.restoreFocus?.();
    log('new-task', 'closed', { status: true });
  }
  async submit() {
    if (!this.active || this.pending || !this.prompt.value.trim() || this.composing) return;
    this.capture(); this.pending = true; this.confirmed = false;
    const generation = ++this.generation, project = this.project;
    const account=creationAccount(this.usableAccounts||[],this.account.value);
    const request = { project, ...(account ? {account} : {}), ...(this.backend.value ? { backend: this.backend.value } : {}), prompt: this.prompt.value, ...(this.name.value.trim() ? { name: this.name.value.trim() } : {}) };
    const abort = this.abort = new AbortController(); this.timer = setTimeout(() => abort.abort(), 120000);
    this.error.hidden = true; this.error.textContent = ''; this.render();
    try {
      const result = await this.api.json('/api/tasks', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(request), signal: abort.signal });
      if (generation !== this.generation || abort.signal.aborted) return;
      this.confirmed = true; this.drafts.delete(project);
      let task = { ...request, ...result, live: false };
      try { task = { ...task, ...await this.api.json(`/api/tasks/${encodeURIComponent(result.id)}`, { signal: abort.signal }) }; }
      catch (error) {
        if (abort.signal.aborted) return;
        log('new-task', 'detail read failed', { status: error.status || 0 });
      }
      if (generation !== this.generation || abort.signal.aborted) return;
      this.close(false); this.onCreated(task);
      log('new-task', 'created', { status: true });
    } catch (error) {
      if (generation !== this.generation) return;
      this.error.textContent = `Task creation was not confirmed. Your prompt is kept. Refresh tasks before retrying. ${error.message}`;
      this.error.hidden = false; this.capture();
      log('new-task', 'creation failed', { status: error.status || 0 });
    } finally {
      if (generation === this.generation) { clearTimeout(this.timer); this.pending = false; this.render(); }
    }
  }
  clear() {
    this.close(false); this.generation++; this.abort?.abort(); clearTimeout(this.timer);
    this.drafts.clear(); this.name.value = ''; this.prompt.value = ''; this.error.textContent = ''; this.error.hidden = true;
  }
}
