import { log } from '../core/api.js';

export function composerIdentity(task, raw = {}) {
  if (!task) return 'No agent selected';
  return [raw.backend || task.backend || 'Agent', raw.model].filter(Boolean).join(' · ');
}

export class ComposerIdentity {
  constructor(api, changed) { this.api = api; this.changed = changed; }
  select(task) {
    this.controller?.abort();
    this.task = task; this.raw = {};
    if (!task || !this.api) return;
    const controller = this.controller = new AbortController();
    this.api.json(`/api/tasks/${encodeURIComponent(task.id)}/raw`, { signal: controller.signal }).then(raw => {
      if (this.controller !== controller || controller.signal.aborted) return;
      this.raw = raw; this.changed();
      log('composer', 'identity loaded', { count: 1 });
    }).catch(error => {
      if (controller.signal.aborted || this.controller !== controller) return;
      log('composer', 'identity unavailable', { status: error.status });
    });
  }
  label() { return composerIdentity(this.task, this.raw); }
}
