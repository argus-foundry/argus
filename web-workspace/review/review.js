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
  }
  cancel() { this.generation++; this.abort?.abort(); this.abort = new AbortController(); this.urls.forEach(url => URL.revokeObjectURL(url)); this.urls.clear(); }
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
  async openDiff(task, path, activate = true) {
    this.cancel();
    const restore=captureFocus($('review-panel'),()=>document.getElementById('tab-review'),{signal:this.abort.signal});
    const generation = this.generation;
    const previous = this.selections.get(task.id);
    const selection = { type: 'diff', path, mode: previous?.mode || 'unified' };
    this.selections.set(task.id, selection);
    if (activate) this.onOpen();
    empty($('review-panel'), '', 'Loading diff…');
    try {
      const result = await this.api.json(`/api/tasks/${encodeURIComponent(task.id)}/git/diff?path=${encodeURIComponent(path)}`, { signal: this.abort.signal });
      if (generation !== this.generation || this.task?.id !== task.id) return;
      const diff = result.Diff || '';
      this.drawDiff(selection, diff, result.comparison);
      log('review', 'diff loaded', { count: diff.length });
    } catch (error) {
      if (generation !== this.generation || error.name === 'AbortError') return;
      const box = empty($('review-panel'), 'Diff unavailable', error.message, 'alert');
      box.append(button('Retry', () => this.openDiff(task, path)));
      log('review', 'diff failed', { status: error.status || 'error' });
    } finally { restore(); }
  }
  event(event) {
    if(event.type!=='resync'&&!(event.type==='worktree.changed'&&event.task_id===this.task?.id))return;
    const selection=this.selections.get(this.task?.id);
    if(selection?.type==='diff')this.openDiff(this.task,selection.path,false);
  }
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
