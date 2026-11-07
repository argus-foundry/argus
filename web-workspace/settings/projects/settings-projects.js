import {confirmDialog} from '../../shared/confirm-dialog.js';
import { node, button, errorMessage } from '../../shared/dom.js';
import { projectForm } from './settings-project-form.js';
import { projectPayload } from '../settings-policy.js';
import { settingsSignal } from '../settings-fields.js';
import { log } from '../../core/api.js';

export class ProjectSettings {
  constructor(api, host, status, changed, busy, { target } = {}) {
    Object.assign(this, { api, host, status, changed, busy, target });
    this.abort = new AbortController(); this.load();
  }
  async load() {
    this.host.replaceChildren(node('p', 'settings-note', 'Loading projects…'));
    try {
      const signal = settingsSignal(this.abort.signal);
      const [projects, backends] = await Promise.all([this.api.json('/api/projects/full', { signal }), this.api.json('/api/backends', { signal })]);
      if (this.abort.signal.aborted) return;
      this.projects = projects.projects || []; this.backends = backends.backends || []; this.render();
      log('settings-projects', 'loaded', { count: this.projects.length });
    } catch (error) {
      if (this.abort.signal.aborted) return;
      this.host.replaceChildren(errorMessage(`Projects unavailable. ${error.message}`), button('Retry Projects', () => this.load()));
      log('settings-projects', 'load failed', { status: error.status || 'error' });
    }
  }
  render() {
    const project=this.target===null?null:this.projects.find(project=>project.name===this.target);
    if(!project&&this.target!==null){this.host.replaceChildren(node('p','settings-note','Choose a repository from navigation, or Add repository.'));if(this.target)this.status('Repository configuration is no longer available. Choose another repository.',true);return;}
    this.edit(project);
  }
  edit(project) {
    this.status('');
    projectForm(this.host,project,this.backends,(values,fields)=>this.save(values,project,fields),()=>this.render());
    const fields=this.host.querySelector('fieldset');
    if(project)fields.append(button('Remove '+project.name,()=>this.remove(project)));
    fields.append(button('Refresh projects',()=>{this.api.invalidateReads();this.load();}));
  }
  async save(values, original, fields) {
    let body;
    try { body = projectPayload(values, original); } catch (error) { this.status(error.message, true); return; }
    fields.disabled = true; this.busy(true); this.status('Saving project…');
    try {
      if (!original) {
        this.api.invalidateReads();
        const result = await this.api.json('/api/projects/full', { signal: settingsSignal(this.abort.signal) });
        if ((result.projects || []).some(project => project.name === body.name)) throw new Error('A project with this name already exists. Choose another name.');
      }
      if (this.abort.signal.aborted) return;
      await this.api.json(original ? '/api/projects/' + encodeURIComponent(original.name) : '/api/projects', {
        method: original ? 'PUT' : 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body), signal: settingsSignal(this.abort.signal),
      });
      if (this.abort.signal.aborted) return;
      this.target=body.name;this.changed(); await this.load();this.status('Saved project.'); log('settings-projects', 'saved');
    } catch (error) {
      if (!this.abort.signal.aborted) this.status(`Save was not confirmed. Your edits are kept. ${error.message}`, true);
      log('settings-projects', 'save failed', { status: error.status || 'error' });
    } finally { fields.disabled = false; this.busy(false); }
  }
  remove(project) {
    this.dialog=confirmDialog({id:'project-delete-dialog',title:`Remove ${project.name}?`,message:'Remove this project configuration from Argus. Existing tasks and worktrees remain.',confirmLabel:'Remove project',signal:this.abort.signal,onConfirm:async()=>{
      this.busy(true);
      try {
        await this.api.json('/api/projects/'+encodeURIComponent(project.name),{method:'DELETE',signal:settingsSignal(this.abort.signal)});
        if(this.abort.signal.aborted)return;
        this.target=undefined;this.changed();await this.load();this.status('Removed project configuration.');log('settings-projects','removed');
      } catch(error){log('settings-projects','remove failed',{status:error.status||0});throw new Error(`Removal was not confirmed. ${error.message}`);}
      finally{this.busy(false);}
    }}).dialog;
  }
  destroy() { this.abort.abort(); this.dialog?.close(); this.dialog?.remove(); }
}
