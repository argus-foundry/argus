import { log } from '../core/api.js';
import { terminalFitNeeded, terminalCellSpace, terminalGlyphGutter } from './terminal-policy.js';

export class TerminalGeometry {
  constructor(term, fit, host, container, changed) {
    Object.assign(this,{term,fit,host,container,changed,active:true});
    this.observer = new ResizeObserver(() => this.schedule());
    this.observer.observe(container); this.observer.observe(host);
    const screen = host.querySelector('.xterm-screen'); if (screen) this.observer.observe(screen);
    this.resized = term.onResize(() => this.schedule()); this.schedule();
  }
  metrics() {
    if (!this.active || document.hidden || !this.host.isConnected || !this.host.getClientRects().length) return null;
    const viewport = this.host.querySelector('.xterm-viewport'), dimensions = this.term._core?._renderService?.dimensions, cell = dimensions?.css?.cell;
    if (!viewport || !cell) return null;
    const style = getComputedStyle(this.term.element), box = getComputedStyle(this.container);
    const padding = (a,b) => parseFloat(style[a]) + parseFloat(style[b]);
    const scrollbar = viewport.offsetWidth-viewport.clientWidth;
    return { width:this.host.clientWidth-padding('paddingLeft','paddingRight'),
      height:this.container.getBoundingClientRect().height-parseFloat(box.borderTopWidth)-parseFloat(box.borderBottomWidth)-parseFloat(box.paddingTop)-parseFloat(box.paddingBottom)-padding('paddingTop','paddingBottom'),
      cellWidth:dimensions.device?.cell?.width/window.devicePixelRatio||cell.width,cellHeight:dimensions.device?.cell?.height/window.devicePixelRatio||cell.height,canvasWidth:dimensions.css.canvas.width,scrollbar };
  }
  propose() { const metrics = this.metrics(); return metrics ? terminalCellSpace(metrics) : null; }
  schedule() {
    if (!this.active || this.frame) return;
    this.frame = requestAnimationFrame(() => { this.frame = null; this.refresh(); });
  }
  refresh() {
    if (!this.active) return;
    const metrics = this.metrics();
    if (metrics) {
      this.host.style.height = `${this.container.clientHeight}px`;
      const glyph=terminalGlyphGutter(this.term.cols);this.term.element.style.setProperty('--terminal-glyph-gutter',`${glyph}px`);
      this.term.element.style.minWidth = `${metrics.canvasWidth+Math.max(16,metrics.scrollbar)+4+glyph}px`;
      const core=this.term._core,textarea=this.host.querySelector('.xterm-helper-textarea');
      if(textarea && !core._compositionHelper?.isComposing){textarea.style.left='-9999em';core._syncTextArea?.();}
    }
    const size = metrics ? terminalCellSpace(metrics) : null, current = {cols:this.term.cols,rows:this.term.rows};
    const needed = terminalFitNeeded(current,size), key = JSON.stringify([size,current,metrics?.width,metrics?.height]);
    if (key === this.key) return;
    this.key = key; this.changed({needed,size,current});
    log('terminal-geometry',size ? 'cell space measured' : 'measurement skipped; pane hidden or unavailable',{stale:!size});
  }
  destroy() { this.active = false; this.observer.disconnect(); this.resized.dispose(); if (this.frame) cancelAnimationFrame(this.frame); }
}
