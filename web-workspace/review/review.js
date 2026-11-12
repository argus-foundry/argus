import {ToolRefresh} from '../tasks/tools-live.js';
import {captureFocus} from '../shared/focus-recovery.js';
import { $, node, button, empty, notice } from '../shared/dom.js';
import { log } from '../core/api.js';
import { renderDiff } from './review-diff.js';
import { artifactPath, previewBlob, renderArtifact, sizeLabel, PREVIEW_LIMIT } from './artifact-view.js';

export class TaskReview {
  constructor(api, onOpen, onSelection = () => {}) {
    this.api = api;
    this.onOpen = onOpen;
    this.onSelection = onSelection;
    this.selections = new Map();
    this.urls = new Set();
    this.generation = 0;
    this.diffLive=new ToolRefresh({visible:()=>this.visible(),read:(id,tool,signal)=>this.api.json(`/api/tasks/${encodeURIComponent(id)}/git/diff?path=${encodeURIComponent(this.diffSelection.path)}`,{signal}),apply:result=>this.applyDiff(result),failed:error=>this.failDiff(error),log:(message,fields)=>log('review-live',message,fields)});
    this.wake=()=>{const visible=this.visible();if(!visible)this.diffLive.suspend();if(visible&&!this.wasVisible)this.diffLive.trigger('visible');this.wasVisible=visible;};
    document.addEventListener?.('visibilitychange',this.wake);
    for(const event of ['focus','blur'])globalThis.window?.addEventListener(event,this.wake);
    if(globalThis.MutationObserver){this.visibilityObserver=new MutationObserver(this.wake);for(const target of [document.body,$('review-panel'),$('workspace')].filter(Boolean))this.visibilityObserver.observe(target,{attributes:true,attributeFilter:['class','hidden','inert','style']});}

  }
  cancel() { this.generation++; this.diffLive.stop(); this.abort?.abort();this.diffRestore?.();this.diffRestore=null;this.diffSelection=null;this.hasDiff=false;this.diffError=null; this.abort = new AbortController(); this.urls.forEach(url => URL.revokeObjectURL(url)); this.urls.clear(); }
  selectTask(task, location) {
    this.cancel();
    this.task = task;
    if (task && location !== undefined) {
      if (location) this.selections.set(task.id, location);
      else this.selections.delete(task.id);
    }
    const selection = task && this.selections.get(task.id);
    if (selection?.type === 'diff') this.openDiff(task, selection.path, false);
    else if (selection?.artifact) this.openArtifact(task, selection.artifact, false);
    else if (selection?.type === 'artifact') this.restoreArtifact(task, selection.filename);
    else empty($('review-panel'), 'See the work take shape.', 'Choose a changed file or registered artifact from task tools.');
  }
  async restoreArtifact(task, filename) {
    this.cancel();
    const restore=captureFocus($('review-panel'),()=>document.getElementById('tab-review'),{signal:this.abort.signal});
    const generation = this.generation;
    empty($('review-panel'), '', 'Finding registered artifact…');
    try {
      const result = await this.api.json(`/api/tasks/${encodeURIComponent(task.id)}/artifacts`, { signal: this.abort.signal });
      if (generation !== this.generation || this.task?.id !== task.id) return;
      const artifact = result.artifacts?.find(item => item.filename === filename);
      log('review', 'artifact location resolved', { count: artifact ? 1 : 0 });
      if (artifact) { restore(); await this.openArtifact(task, artifact, false); }
      else empty($('review-panel'), 'Artifact unavailable', 'This artifact is no longer registered for the task. Choose another artifact from task tools.', 'alert');
    } catch (error) {
      if (generation !== this.generation || error.name === 'AbortError') return;
      log('review', 'artifact location failed', { status: error.status || 'error' });
      const box = empty($('review-panel'), 'Artifact unavailable', error.message, 'alert');
      box.append(button('Retry', () => this.restoreArtifact(task, filename)));
    } finally { restore(); }
  }
  visible() {
    const panel=$('review-panel');
    return !!panel&&!panel.hidden&&!panel.closest('[hidden],[inert]')&&!document.hidden&&document.hasFocus?.()!==false&&!!panel.getClientRects?.().length&&getComputedStyle(panel).visibility!=='hidden'&&!document.body.classList?.contains('settings-mode');
  }
  async openDiff(task, path, activate = true) {
    this.cancel();
    this.diffRestore=captureFocus($('review-panel'),()=>document.getElementById('tab-review'),{signal:this.abort.signal});
    const previous=this.selections.get(task.id);
    this.diffSelection={type:'diff',path,mode:previous?.mode||'unified'};
    this.selections.set(task.id,this.diffSelection);
    if(activate)this.onOpen();
    empty($('review-panel'),'','Loading diff…');this.wasVisible=this.visible();
    return this.diffLive.select(task.id,'diff');
  }
  applyDiff(result) {
    if(this.task?.id!==this.diffLive.task)return;
    const restore=captureFocus($('review-panel'),()=>document.getElementById('tab-review'),{signal:this.abort.signal});
    this.diffError?.remove();this.diffError=null;
    this.drawDiff(this.diffSelection,result.Diff||'',result.comparison);this.hasDiff=true;
    restore();this.diffRestore?.();this.diffRestore=null;
    log('review','diff loaded',{count:result.Diff?.length||0});
  }
  failDiff(error) {
    if(this.task?.id!==this.diffLive.task)return;
    const restore=captureFocus($('review-panel'),()=>document.getElementById('tab-review'),{signal:this.abort.signal});
    this.diffError?.remove();
    const box=this.hasDiff?node('div','tool-error'):empty($('review-panel'),'Diff unavailable',error.message,'alert');
    if(this.hasDiff){box.setAttribute('role','alert');box.append(node('p','','Diff unavailable. '+error.message));$('review-panel').prepend(box);}
    box.append(button('Retry',()=>this.diffLive.trigger('retry')));
    this.diffError=box;restore();this.diffRestore?.();this.diffRestore=null;log('review','diff failed',{status:error.status||0});
  }
  event(event) {if(!this.visible())this.wasVisible=false;this.diffLive.event(event);}
  drawDiff(selection, diff, comparison) { renderDiff($('review-panel'), { ...selection, diff, comparison }, mode => { selection.mode = mode; this.drawDiff(selection, diff, comparison); this.onSelection(); }); }
  async openArtifact(task, artifact, activate = true) {
    this.cancel();
    const restore=captureFocus($('review-panel'),()=>document.getElementById('tab-review'),{signal:this.abort.signal});
    const generation = this.generation;
    const isCurrent = () => generation === this.generation && this.task?.id === task.id;
    this.selections.set(task.id, { type: 'artifact', artifact });
    if (activate) this.onOpen();
    const toolbar = node('div', 'review-toolbar');
    const title=node('strong', 'review-title', artifact.name || artifact.filename);title.dataset.tooltip=artifact.name || artifact.filename;title.dataset.tooltipOverflow='';title.setAttribute('aria-label', artifact.name || artifact.filename);toolbar.append(title);
    const download = button('Download', () => this.download(task, artifact, download, isCurrent), 'artifact-download');
    toolbar.append(download);
    const content = node('div', 'artifact-preview');
    $('review-panel').replaceChildren(toolbar, node('p', 'preview-note', `${artifact.type} · ${sizeLabel(artifact.size)}${artifact.type === 'html' ? ' · Safe preview, scripts disabled' : artifact.type === 'markdown' ? ' · Markdown source' : ''}`), content);
    if (artifact.size > PREVIEW_LIMIT) { empty(content, 'Ready to download', 'This artifact is too large to preview here. Download it to view the full file.'); restore(); return; }
    empty(content, '', 'Loading artifact…');
    try {
      const response = await this.api.request(artifactPath(task, artifact), { signal: this.abort.signal });
      const blob = await previewBlob(response);
      if (!isCurrent()) return;
      content.replaceChildren();
      await renderArtifact(content, artifact, blob, value => this.makeURL(value), isCurrent);
      log('review', 'artifact loaded', { count: blob.size });
    } catch (error) {
      if (!isCurrent() || error.name === 'AbortError') return;
      const box = empty(content, 'Preview unavailable', error.message, 'alert');
      box.append(button('Retry preview', () => this.openArtifact(task, artifact)));
      log('review', 'artifact failed', { status: error.status || 'error' });
    } finally { restore(); }
  }
  makeURL(blob) { const url = URL.createObjectURL(blob); this.urls.add(url); return url; }
  async download(task, artifact, control, isCurrent) {
    const restore=captureFocus(control,()=>document.getElementById('tab-review'),{signal:this.abort.signal});
    control.disabled = true;
    control.textContent = 'Downloading…';
    try {
      const response = await this.api.request(artifactPath(task, artifact), { signal: this.abort.signal });
      const blob = await response.blob();
      if (!isCurrent()) return;
      const anchor = node('a');
      const url = this.makeURL(blob);
      anchor.href = url;
      anchor.download = artifact.filename.split('/').at(-1);
      anchor.hidden = true;
      document.body.append(anchor);
      anchor.click();
      anchor.remove();
      setTimeout(() => { if (this.urls.delete(url)) URL.revokeObjectURL(url); }, 0);
      log('review', 'artifact download', { count: blob.size });
    } catch (error) {
      if (isCurrent() && error.name !== 'AbortError') { notice(`Download failed. ${error.message}`); log('review', 'download failed', { status: error.status || 'error' }); }
    } finally { if (isCurrent()) { control.disabled = false; control.textContent = 'Download'; } restore(); }
  }
  clear() { this.cancel(); this.task = null; this.selections.clear(); empty($('review-panel'), 'See the work take shape.', 'Choose a changed file or registered artifact from task tools.'); }
}
