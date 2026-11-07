export function sendsMessage(event, composing = false) {
  return event.key === 'Enter' && !event.shiftKey && !event.altKey && !event.repeat &&
    !event.isComposing && !composing && event.keyCode !== 229;
}

export function insertPlaintext(field, text) {
  const before = field.value, start = field.selectionStart, end = field.selectionEnd;
  try { field.ownerDocument.execCommand('insertText', false, text); } catch {}
  if (field.value === before) field.setRangeText(text, start, end, 'end');
}
