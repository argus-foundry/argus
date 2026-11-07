import { fileDrag, transferFiles } from './attachment-policy.js';

export class AttachmentDrop {
  constructor({ zones, available, add, message }) {
    Object.assign(this, { zones, available, add, message });
    window.addEventListener('dragover', event => this.hover(event), true);
    window.addEventListener('dragenter', event => this.hover(event), true);
    window.addEventListener('drop', event => this.drop(event), true);
    window.addEventListener('dragleave', event => { if (!(event.relatedTarget instanceof Node) || !this.target?.contains(event.relatedTarget)) this.clear(); });
    window.addEventListener('dragend', () => this.clear());
    window.addEventListener('blur', () => this.clear());
  }
  zone(element) { return element instanceof Node ? this.zones.find(zone => zone?.contains(element)) : null; }
  guard(event) { event.preventDefault(); event.stopPropagation(); }
  hover(event) {
    if (!fileDrag(event.dataTransfer)) return;
    this.guard(event);
    const zone = this.zone(event.target), error = this.available();
    if (zone !== this.target) this.clear();
    this.target = zone;
    zone?.classList.add('file-drag-over');
    zone?.classList.toggle('file-drop-disabled', !!error);
    event.dataTransfer.dropEffect = zone && !error ? 'copy' : 'none';
    if (zone && error) this.message(error);
  }
  drop(event) {
    if (!fileDrag(event.dataTransfer)) return;
    this.guard(event);
    this.clear();
    if (!this.zone(event.target)) { this.message('Drop files onto the composer or agent panel.'); return; }
    this.add(transferFiles(event.dataTransfer));
  }
  clear() { this.target?.classList.remove('file-drag-over', 'file-drop-disabled'); this.target = null; }
}
