import { log } from '../core/api.js';

export function readWorkspaceLocation(url) {
  const params = new URL(url).searchParams;
  const task = params.get('task') || null;
  const view = params.get('view') === 'review' ? 'review' : 'agent';
  const mode = params.get('mode') === 'split' ? 'split' : 'unified';
  let selection = null;
  if (params.get('file')) selection = { type: 'diff', path: params.get('file'), mode };
  else if (params.get('artifact')) selection = { type: 'artifact', filename: params.get('artifact') };
  const settings = params.has('settings') ? (params.get('settings') || 'general') : null;
  return { task, view, selection, settings, taskSettings: params.get('task_settings') || null };
}
export function workspaceURL(url, task, view, selection) {
  const next = new URL(url);
  next.search = '';
  next.hash = '';
  if (task) {
    next.searchParams.set('task', task.id);
    if (view === 'review') next.searchParams.set('view', 'review');
    if (selection?.type === 'diff') {
      next.searchParams.set('file', selection.path);
      if (selection.mode === 'split') next.searchParams.set('mode', 'split');
    } else if (selection?.type === 'artifact') next.searchParams.set('artifact', selection.artifact?.filename || selection.filename);
  }
  return next;
}
export class WorkspaceHistory {
  constructor(onRestore) {
    this.onRestore = onRestore;
    this.restoring = false;
    this.position = window.history.state?.workspacePosition ?? 0;
    this.replace(location.href, window.history.state);
    window.addEventListener('popstate', () => this.traverse());
  }
  push(url, state = null) {
    this.position++;
    window.history.pushState({ ...state, workspacePosition: this.position }, '', url);
  }
  replace(url, state = null) {
    window.history.replaceState({ ...state, workspacePosition: this.position }, '', url);
  }
  traverse() {
    const target = window.history.state?.workspacePosition ?? 0;
    if (this.recovering) {
      if (target === this.position) { this.recovering = false; log('workspace-history', 'refused traversal restored'); }
      else window.history.go(this.position - target);
      return;
    }
    const previous = this.position;
    this.position = target;
    if (this.restore() === false && target !== previous) {
      this.position = previous;
      this.recovering = true;
      log('workspace-history', 'traversal refused during save');
      window.history.go(this.position - target);
    }
  }
  read() { return readWorkspaceLocation(location.href); }
  suspend(action) {
    const previous = this.restoring;
    this.restoring = true;
    try { return action(); }
    finally { this.restoring = previous; }
  }
  restore() { return this.suspend(() => this.onRestore(this.read())); }
  write(task, view, selection, replace = false) {
    if (this.restoring) return;
    const next = workspaceURL(location.href, task, view, selection);
    if (next.href === location.href) return;
    this[replace ? 'replace' : 'push'](next);
  }
}
