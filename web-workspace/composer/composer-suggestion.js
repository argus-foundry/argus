import { log } from '../core/api.js';
import { requestSignal } from '../terminal/terminal-policy.js';

const liveWindow = 45000, pollInterval = 15000;

export class ComposerSuggestion {
  constructor(api, draft) {
    this.api = api; this.draft = draft; this.normalHint = draft.placeholder; this.visible = true;
    document.addEventListener('visibilitychange', () => {
      clearTimeout(this.poll); this.poll = null; clearTimeout(this.debounce);
      this.render();
      if (document.hidden) { this.api?.invalidate?.(); this.cancel(); }
      else if (this.task) this.refresh();
    });
  }
  setVisible(value) {
    if (this.visible === !!value) return;
    this.visible = !!value; clearTimeout(this.debounce); this.cancel(); this.render();
    if (this.visible) this.refresh();
    log('composer-suggestion','visibility changed',{status:this.visible});
  }
  cancel() { this.controller?.abort(); this.controller = null; }
  clear() {
    clearTimeout(this.poll); this.poll = null; clearTimeout(this.expiry);
    this.data = null; this.render();
  }
  select(task) {
    this.cancel(); clearTimeout(this.debounce); this.task = task; this.clear();
    if (task) this.refresh();
  }
  render() {
    const shown = !this.draft.value && this.data && this.data.until > Date.now();
    const placeholder = shown ? this.data.text : this.normalHint;
    if (this.draft.placeholder !== placeholder) this.draft.placeholder = placeholder;
    if (!shown || !this.visible || document.hidden) { clearTimeout(this.poll); this.poll = null; }
    else if (!this.controller && !this.poll) this.poll = setTimeout(() => { this.poll = null; this.refresh(); }, pollInterval);
  }
  newTurn() {
    this.api?.invalidate?.();
    this.cancel(); clearTimeout(this.debounce); this.clear();
  }
  event(event) {
    if (!this.task || event.task_id !== this.task.id) return;
    if (!['task.suggestion_changed','session.started','session.exited','task.status_changed','task.archived','task.deleted'].includes(event.type)) return;
    this.api?.invalidate?.();
    this.cancel(); this.clear(); clearTimeout(this.debounce);
    this.debounce = setTimeout(() => this.refresh(), 100);
  }
  async refresh() {
    if (!this.task || !this.api || !this.visible || document.hidden || this.controller) return;
    clearTimeout(this.poll); this.poll = null;
    const controller = this.controller = new AbortController(), task = this.task;
    const signal = requestSignal(controller.signal);
    try {
      const bridge = await this.api.json(`/api/tasks/${encodeURIComponent(task.id)}/bridge`, {signal});
      if (this.controller !== controller || signal.aborted || this.task !== task) { log('composer-suggestion','obsolete read ignored',{stale:true}); return; }
      const suggestion = bridge?.suggestion, until = Date.parse(bridge?.last_event_at) + liveWindow;
      this.clear();
      if (bridge?.live === true && bridge.task_id === task.id && bridge.session_id && bridge.generation &&
          suggestion?.session_id === bridge.session_id && suggestion.generation === bridge.generation &&
          typeof suggestion.text === 'string' && suggestion.text.trim() && until > Date.now()) {
        this.data = {text:suggestion.text,until};
        this.expiry = setTimeout(() => { this.cancel(); this.clear(); log('composer-suggestion','authority expired'); }, until - Date.now());
      }
      log('composer-suggestion','refreshed',{count:this.data ? 1 : 0});
    } catch (error) {
      if (this.controller !== controller || controller.signal.aborted) return;
      this.clear(); log('composer-suggestion','unavailable',{status:error.status || 0});
    } finally {
      if (this.controller === controller) { this.controller = null; this.render(); }
    }
  }
}
