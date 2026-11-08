import { requestSignal } from '../terminal/terminal-policy.js';

export class ComposerBridge {
  constructor(api) { this.api = api; }
  invalidate() { this.controller?.abort(); this.controller = null; this.read = null; }
  request(path, options = {}) { return this.api.request(path, options); }
  json(path, options = {}) {
    if (!path.endsWith('/bridge')) return this.api.json(path, options);
    if (!this.read) {
      const controller = this.controller = new AbortController();
      this.read = this.api.json(path, {signal:requestSignal(controller.signal)}).finally(() => {
        if (this.controller === controller) { this.controller = null; this.read = null; }
      });
    }
    return this.read;
  }
}
