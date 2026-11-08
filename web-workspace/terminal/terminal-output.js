// Defer writes during wheel/touch movement without pulling readers out of history.
export class TerminalOutput {
  constructor(term, host) {
    this.term = term;
    this.pending = [];
    this.bytes = 0;
    this.active = true;
    this.scrolling = false;
    this.touching = false;
    this.listeners = [];
    const listen = (element, name, handler) => {
      if (!element) return;
      element.addEventListener(name, handler, { passive: true });
      this.listeners.push(() => element.removeEventListener(name, handler));
    };
    listen(host.querySelector('.xterm-viewport'), 'scroll', () => {
      this.scrolling = true;
      this.settle();
    });
    listen(host, 'touchstart', () => { this.touching = true; });
    for (const event of ['touchend', 'touchcancel']) listen(host, event, () => {
      this.touching = false;
      this.settle();
    });
  }

  settle() {
    clearTimeout(this.timer);
    this.timer = setTimeout(() => {
      if (!this.active) return;
      this.scrolling = false;
      this.flush();
    }, 180);
  }

  write(bytes) {
    if (!this.active) return Promise.resolve();
    return new Promise(resolve => {
      this.pending.push({ bytes, resolve });
      this.bytes += bytes.byteLength;
      this.flush();
    });
  }

  replace(bytes, size) {
    if (!this.active) return Promise.resolve();
    this.pending.splice(0).forEach(item => item.resolve());
    this.bytes = bytes.byteLength;
    return new Promise(resolve => {
      this.pending.push({ bytes, size, resolve, reset: true });
      this.flush();
    });
  }

  flush() {
    if (!this.active || this.writing || !this.pending.length) return;
    // Bound gesture buffering. xterm itself preserves the normal-screen viewport.
    if (!this.pending[0].reset && (this.scrolling || this.touching) && this.bytes < 2 * 1024 * 1024) return;
    const item = this.pending.shift();
    this.bytes -= item.bytes.byteLength;
    this.writing = item;
    let readingLine = null;
    if (item.reset) {
      const buffer = this.term.buffer.active;
      if (buffer.viewportY < buffer.baseY) readingLine = buffer.viewportY;
      this.term.reset();
      this.term.resize(item.size.cols, item.size.rows);
    }
    this.term.write(item.bytes, () => {
      if (this.active && readingLine !== null) this.term.scrollToLine(Math.min(readingLine, this.term.buffer.active.baseY));
      item.resolve();
      this.writing = null;
      this.flush();
    });
  }

  destroy() {
    this.active = false;
    clearTimeout(this.timer);
    this.listeners.forEach(remove => remove());
    this.writing?.resolve();
    this.pending.splice(0).forEach(item => item.resolve());
  }
}
