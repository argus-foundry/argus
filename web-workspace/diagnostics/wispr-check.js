const field = document.getElementById('dictation');
const events = [];
const terminalMode = new URL(location.href).searchParams.get('terminal') === '1';
const directPaste = new URL(location.href).searchParams.get('paste') === 'direct';
function record(event) {
  const entry = { event: event.type, target: event.target.id || event.target.tagName || 'window',
    active: document.activeElement?.id || document.activeElement?.tagName,
    focused: document.hasFocus(), editable: !field.disabled && !field.readOnly,
    composing: Boolean(event.isComposing), prevented: event.defaultPrevented,
    trusted: event.isTrusted, cancelable: event.cancelable, inputType: event.inputType || null,
    length: field.value.length, selectionStart: field.selectionStart, selectionEnd: field.selectionEnd };
  if (event.type === 'paste' && event.clipboardData) {
    const text = event.clipboardData.getData('text/plain');
    entry.clipboardTypes = [...event.clipboardData.types];
    entry.plainTextLength = text.length;
    entry.wouldChangeValue = field.value.slice(0, field.selectionStart) + text + field.value.slice(field.selectionEnd) !== field.value;
  }
  if (event.type === 'beforeinput') entry.dataLength = event.data?.length ?? null;
  events.push(entry);
  if (['paste', 'beforeinput'].includes(event.type)) setTimeout(() => {
    entry.preventedAfterDispatch = event.defaultPrevented;
    entry.lengthAfterDispatch = field.value.length;
    entry.selectionStartAfterDispatch = field.selectionStart;
    entry.selectionEndAfterDispatch = field.selectionEnd;
  }, 0);
  if (events.length > 80) events.shift();
}
for (const type of ['focusin', 'focusout', 'beforeinput', 'input', 'paste', 'compositionstart', 'compositionend', 'visibilitychange']) {
  document.addEventListener(type, record, type === 'paste');
}
for (const type of ['focus', 'blur']) window.addEventListener(type, record);
document.getElementById('clear').onclick = () => { field.value = ''; field.focus(); };
document.getElementById('show').onclick = () => {
  const output = document.getElementById('diagnostics');
  output.hidden = false;
  output.textContent = JSON.stringify({ terminalMode, directPaste, events }, null, 2);
};
if (directPaste) {
  document.getElementById('mode').textContent = 'Plain text box with direct paste handling. No terminal is connected.';
  field.addEventListener('paste', event => {
    if (event.defaultPrevented || field.disabled || field.readOnly) return;
    const text = event.clipboardData?.getData('text/plain');
    if (!text) return;
    event.preventDefault();
    field.setRangeText(text, field.selectionStart, field.selectionEnd, 'end');
    field.dispatchEvent(new InputEvent('input', { bubbles: true, inputType: 'insertFromPaste' }));
  });
}

function script(src) {
  return new Promise((resolve, reject) => {
    const element = document.createElement('script');
    element.src = src; element.onload = resolve; element.onerror = reject;
    document.head.append(element);
  });
}
if (terminalMode) {
  document.getElementById('mode').textContent = 'The same plain text box, with xterm mounted separately below.';
  document.getElementById('compare').href = '?terminal=0';
  document.getElementById('compare').textContent = 'Compare without the terminal';
  const stylesheet = document.createElement('link');
  stylesheet.rel = 'stylesheet'; stylesheet.href = '/vendor/xterm.css'; document.head.append(stylesheet);
  try {
    await script('/vendor/xterm.js');
    const element = document.getElementById('terminal'); element.hidden = false;
    const terminal = new window.Terminal({ cols: 80, rows: 6, screenReaderMode: true });
    terminal.open(element);
    terminal.writeln('Local display only. No shell, agent or API is connected.');
    terminal.onData(data => terminal.write(data));
    window.addEventListener('pagehide', () => terminal.dispose(), { once: true });
    field.focus();
  } catch {
    document.getElementById('mode').textContent = 'The comparison terminal could not load. The plain text box is still available.';
  }
}
