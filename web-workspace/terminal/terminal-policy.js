export function isInteractive(task) {
  if (!task || task.archived || task.archive_pending) return false;
  return typeof task.live === 'boolean' ? task.live : task.status === 'in_progress' && !task.idle;
}

export function isCtrlZ(event) {
  return event.ctrlKey && !event.metaKey && !event.altKey &&
    (event.key === 'z' || event.key === 'Z' || event.keyCode === 90);
}

export function terminalSize(size) {
  return size && Number.isInteger(size.cols) && Number.isInteger(size.rows) &&
    size.cols >= 2 && size.rows >= 1 && size.cols <= 1000 && size.rows <= 1000;
}

export function outputCursor(header) {
  if (typeof header !== 'string' || !/^\d+$/.test(header)) throw new Error('Output cursor unavailable');
  const cursor = Number(header);
  if (!Number.isSafeInteger(cursor)) throw new Error('Output cursor out of range');
  return cursor;
}

export function decodeOutput(encoded) {
  return Uint8Array.from(atob(encoded), character => character.charCodeAt(0));
}

export function requestSignal(signal, timeout = 10000) {
  return AbortSignal.any([signal, AbortSignal.timeout(timeout)]);
}

export function composePaste(text, bracketed = true) {
  const value = String(text).replace(/[\r\n]+$/, '');
  if (!value) throw new Error('Enter a message first');
  if (/[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]/.test(value)) {
    throw new Error('Message contains terminal control characters');
  }
  if (!bracketed && /[\r\n]/.test(value)) {
    throw new Error('Multiline compose requires bracketed paste. Use the terminal or enable bracketed paste.');
  }
  const paste = bracketed ? '\x1b[200~' + value + '\x1b[201~' : value;
  if (new TextEncoder().encode(paste).length > 64 * 1024) throw new Error('Message exceeds 64 KB');
  return paste;
}

export function terminalFitNeeded(current, proposed) {
  return Boolean(terminalSize(current) && terminalSize(proposed) &&
    (current.cols !== proposed.cols || current.rows !== proposed.rows));
}

export function retainedOutput(cursor,total,bytes) {
  const start=total-bytes.byteLength;
  return Number.isSafeInteger(cursor)&&cursor>=start&&cursor<=total?bytes.slice(cursor-start):null;
}

export function terminalBusy(task) {
  return task?.live === true && task.idle !== true && !task.needs_input;
}

export function terminalCellSpace({width,height,cellWidth,cellHeight,scrollbar=0}) {
  if (![width,height,cellWidth,cellHeight,scrollbar].every(Number.isFinite) || cellWidth<=0 || cellHeight<=0) return null;
  const candidate=Math.min(1000,Math.floor((width-Math.max(16,scrollbar)-4)/cellWidth));
  const size = { cols:Math.min(1000,Math.floor((width-Math.max(16,scrollbar)-4-terminalGlyphGutter(candidate))/cellWidth)), rows:Math.min(1000,Math.floor(height/cellHeight)) };
  return terminalSize(size) ? size : null;
}

// Reserve cumulative CSS subpixel rounding plus a small glyph overhang.
export function terminalGlyphGutter(cols) { return Math.max(4,Math.ceil(Math.max(0,cols)/64)+2); }
