export const $ = id => document.getElementById(id);
export function node(tag, className, text) {
  const item = document.createElement(tag);
  if (className) item.className = className;
  if (text !== undefined) item.textContent = text;
  return item;
}
export function button(text, onClick, className = '') {
  const item = node('button', className, text);
  item.type = 'button';
  item.addEventListener('click', onClick);
  return item;
}
export function errorMessage(message, className = 'error-text') {
  const item = node('p', className, message);
  item.setAttribute('role', 'alert');
  return item;
}
export function empty(container, title, message, role) {
  const box = node('div', 'empty-state');
  if (role) box.setAttribute('role', role);
  if (title) box.append(node('h2', '', title));
  box.append(node('p', '', message));
  container.replaceChildren(box);
  return box;
}
export function tabs(container, onSelect) {
  const items = [...container.querySelectorAll('[role="tab"]')];
  function select(item, focus = false) {
    for (const tab of items) {
      tab.setAttribute('aria-selected', String(tab === item));
      tab.tabIndex = tab === item ? 0 : -1;
    }
    if (focus) item.focus();
    onSelect(item);
  }
  for (const item of items) {
    item.addEventListener('click', () => select(item));
    item.addEventListener('keydown', event => {
      const index = items.indexOf(item);
      const next = { ArrowRight: (index + 1) % items.length, ArrowLeft: (index - 1 + items.length) % items.length, Home: 0, End: items.length - 1 }[event.key];
      if (next === undefined) return;
      event.preventDefault();
      select(items[next], true);
    });
  }
  return id => select(items.find(item => item.id === id) || items[0]);
}
let noticeTimer;
export function notice(message) {
  clearTimeout(noticeTimer);
  $('notice').textContent = message;
  $('notice').hidden = false;
  noticeTimer = setTimeout(() => { $('notice').hidden = true; }, 6000);
}
export function preference(key, fallback) {
  try { return JSON.parse(localStorage.getItem(`argus-workspace:${key}`)) ?? fallback; }
  catch { return fallback; }
}
export function savePreference(key, value) {
  try { localStorage.setItem(`argus-workspace:${key}`, JSON.stringify(value)); } catch { /* Layout remains usable without storage. */ }
}
