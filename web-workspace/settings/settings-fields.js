import { node, button } from '../shared/dom.js';

export function field(container, label, name, value = '', type = 'text', hint = '') {
  const wrapper = node('label', `settings-field ${type === 'checkbox' ? 'settings-check' : ''}`);
  const input = node(type === 'textarea' ? 'textarea' : 'input');
  input.name = name;
  if (type !== 'textarea') input.type = type;
  if (type === 'checkbox') input.checked = !!value; else input.value = value;
  const caption = node('span', '', label); caption.id = `settings-${name}-label`; input.setAttribute('aria-labelledby', caption.id);
  wrapper.append(caption, input);
  if (hint) { const note = node('small', '', hint); note.id = `settings-${name}-hint`; input.setAttribute('aria-describedby', note.id); wrapper.append(note); }
  container.append(wrapper);
  return input;
}
export function select(container, label, name, choices, current = '', hint = '') {
  const wrapper = node('label', 'settings-field'), input = node('select'); input.name = name;
  const values = [...choices];
  if (!values.some(([value]) => value === current)) values.push([current, current || 'Default']);
  for (const [value, text] of values) { const option = node('option', '', text); option.value = value; input.append(option); }
  const caption = node('span', '', label); caption.id = `settings-${name}-label`; input.setAttribute('aria-labelledby', caption.id);
  input.value = current; wrapper.append(caption, input);
  if (hint) { const note = node('small', '', hint); note.id = `settings-${name}-hint`; input.setAttribute('aria-describedby', note.id); wrapper.append(note); } container.append(wrapper); return input;
}
export function actions(form, saveLabel, cancel) {
  const row = node('div', 'settings-actions'), save = node('button', 'primary', saveLabel); save.type = 'submit'; row.append(save);
  if (cancel) row.append(button('Cancel', cancel));
  form.append(row); return save;
}
export function values(form) {
  return Object.fromEntries([...form.elements].filter(input => input.name).map(input => [input.name, input.type === 'checkbox' ? input.checked : input.value]));
}
export function settingsSignal(signal) { return AbortSignal.any([signal, AbortSignal.timeout(15000)]); }
