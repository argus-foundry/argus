import { $, node, button } from '../shared/dom.js';
import { AttachmentStore } from './attachments/attachment-store.js';
import { AttachmentDrop } from './attachments/attachment-drop.js';
import { imageFile, transferFiles } from './attachments/attachment-policy.js';

export class ComposerAttachments {
  constructor(api, composer) {
    this.composer = composer;
    this.host = node('div', 'composer-attachments');
    this.chips = node('div', 'attachment-list');
    this.chips.setAttribute('role', 'list'); this.chips.setAttribute('aria-label', 'Message attachments');
    this.status = node('p', 'attachment-status');
    this.status.id = 'attachment-status'; this.status.setAttribute('role', 'status');
    this.errors = node('p', 'sr-only');
    this.errors.setAttribute('role', 'alert'); this.errors.setAttribute('aria-atomic', 'true');
    this.host.append(this.chips, this.status, this.errors);
    $('draft').after(this.host);
    this.picker = node('input'); this.picker.type = 'file'; this.picker.multiple = true; this.picker.hidden = true;
    this.picker.setAttribute('aria-label', 'Choose attachments');
    this.host.append(this.picker);
    $('attach-files')?.addEventListener('click', () => { if (!this.unavailable()) this.picker.click(); });
    this.picker.addEventListener('change', () => { this.add(Array.from(this.picker.files || [])); this.picker.value = ''; });
    this.store = new AttachmentStore(api, () => composer.render());
    this.drop = new AttachmentDrop({
      zones: [$('composer').closest('.composer-wrap') || $('composer'), $('agent-panel')],
      available: () => this.unavailable(), add: files => this.add(files), message: text => { this.message = text; this.render(); },
    });
  }
  unavailable() {
    if (!this.composer.task) return 'Select a live task to attach files.';
    if (!this.composer.interactive) return 'This task is not accepting input. Reconnect before attaching files.';
    if ($('draft').readOnly || $('draft').closest('[inert]') || document.querySelector('dialog[open]')) return 'Close the dialog or drawer before attaching files.';
    return '';
  }
  add(files) {
    this.message = this.unavailable();
    if (!this.message) {
      try { this.store.add(this.composer.task.id, files); this.composer.focus(); }
      catch (error) { this.message = error.message; }
    }
    this.render();
  }
  paste(event) {
    const images = transferFiles(event.clipboardData).filter(imageFile);
    if (!images.length) return false;
    event.preventDefault(); this.add(images); return true;
  }
  render() {
    const task = this.composer.task, items = this.store.list(task?.id);
    const pending = this.composer.pending.has(task?.id), error = this.unavailable();
    const hint = this.message || '';
    const key = JSON.stringify([task?.id, pending, error, hint, items.map(item => [item.id, item.state, item.error])]);
    if (key === this.key) return;
    this.key = key;
    const active = this.chips.contains(document.activeElement) ? document.activeElement : null;
    const focused = active && { id: active.closest('.attachment-chip').dataset.id, action: active.dataset.action,
      index: [...this.chips.children].indexOf(active.closest('.attachment-chip')) };
    this.chips.replaceChildren();
    for (const item of items) {
      const chip = node('div', 'attachment-chip');
      chip.setAttribute('role', 'listitem');
      chip.dataset.state = item.state; chip.dataset.id = String(item.id);
      if (item.url) { const image = node('img'); image.src = item.url; image.alt = ''; chip.append(image); }
      const info = node('div', 'attachment-info');
      info.append(node('span', 'attachment-name', item.file.name || 'Image'), node('span', 'attachment-state', item.error || (item.state === 'ready' ? 'Ready' : 'Uploading…')));
      chip.append(info);
      if (item.state === 'error') {
        const retry = button('Retry upload', () => { if (!this.unavailable()) this.store.upload(item); });
        retry.dataset.action = 'retry';
        retry.disabled = !!error || pending; chip.append(retry);
      }
      const remove = button('Remove', () => this.store.remove(item));
      remove.dataset.action = 'remove';
      remove.setAttribute('aria-label', `Remove ${item.file.name || 'image'}`); remove.disabled = pending;
      chip.append(remove); this.chips.append(chip);
    }
    const failures = items.filter(item => item.state === 'error').map(item => `${item.file.name || 'Image'}: ${item.error}`).join('\n');
    if (this.errors.textContent !== failures) this.errors.textContent = failures;
    this.status.textContent = hint;
    this.status.hidden = !hint;
    this.chips.hidden = !items.length;
    if ($('attach-files')) $('attach-files').disabled = !!error || pending;
    if (focused) this.restoreFocus(focused);
  }
  restoreFocus(focused) {
    const chips = [...this.chips.children];
    const chip = chips.find(item => item.dataset.id === focused.id);
    const available = selector => chip?.querySelector(`${selector}:not(:disabled)`);
    const target = available(`[data-action="${focused.action}"]`) || available('[data-action="remove"]')
      || chips[Math.min(focused.index, chips.length - 1)]?.querySelector('button:not(:disabled)');
    if (target) target.focus({ preventScroll: true });
    else this.composer.focus();
  }
  clear() { this.message = ''; this.drop.clear(); this.store.clear(); }
}
