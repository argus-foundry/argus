import {captureFocus} from '../shared/focus-recovery.js';
import { node, button } from '../shared/dom.js';
import { settingsSections } from './settings-sections.js';
import { settingsSignal } from './settings-fields.js';
import { settingsIcon } from './settings-icon.js';
import { log } from '../core/api.js';

export class SettingsNavigation {
  constructor(api, open, close) {
    Object.assign(this, { api, open, projects: [], query: '', selected: 'general' });
    this.element = node('div', 'settings-navigation');
    this.back = button('← Back', close, 'settings-back'); this.back.dataset.tooltip = 'Back'; this.back.dataset.tooltipShortcut = 'Esc';
    this.selector = node('select', 'settings-selector'); this.selector.setAttribute('aria-label', 'Settings section');
    this.selector.addEventListener('change', () => this.open(this.selector.value));
    this.search = node('input', 'settings-search'); this.search.type = 'search'; this.search.placeholder = 'Search settings'; this.search.setAttribute('aria-label', 'Search settings');
    this.nav = node('nav', 'settings-nav'); this.nav.setAttribute('aria-label', 'Settings sections');
    this.note = node('p', 'settings-note'); this.note.setAttribute('role', 'status');
    this.clear = button('×', () => { this.search.value = ''; this.searchChanged(); this.search.focus(); }, 'settings-search-clear');
    this.clear.setAttribute('aria-label', 'Clear settings search'); this.clear.dataset.tooltip = 'Clear settings search'; this.clear.hidden = true;
    this.search.addEventListener('input', () => this.searchChanged());
    const searchRow = node('div', 'settings-search-row'); const shortcut = node('kbd', '', '/'); shortcut.setAttribute('aria-hidden', 'true'); this.shortcut = shortcut; searchRow.append(this.search, shortcut, this.clear);
    this.element.append(this.back, this.selector, searchRow, this.nav, this.note); this.render();
  }
  async load() {
    this.abort?.abort(); const abort = this.abort = new AbortController();
    try {
      const result = await this.api.json('/api/projects/full', { signal: settingsSignal(abort.signal) });
      if (abort.signal.aborted) return;
      const restore=captureFocus(this.note,()=>this.search,{signal:abort.signal});
      this.projects = result.projects || []; this.note.replaceChildren(); this.render(); restore();
      log('settings-navigation', 'repositories loaded', { count: this.projects.length });
    } catch (error) {
      if (abort.signal.aborted) return;
      const restore=captureFocus(this.note,()=>this.search,{signal:abort.signal});
      this.note.replaceChildren(node('span', '', 'Repository list unavailable. '), button('Retry repositories', () => this.load())); restore();
      log('settings-navigation', 'repository read failed', { status: error.status || 'error' });
    }
  }
  entry(group, id, label, keywords = '') {
    const control = button(label, () => this.open(id), 'settings-nav-entry'); control.dataset.section = id;
    control.dataset.search = `${label} ${keywords}`.toLowerCase();
    if (id.startsWith('repository:')) { control.dataset.tooltip = label; control.dataset.tooltipOverflow = ''; }
    control.prepend(settingsIcon(id, label)); group.append(control);
    const option = node('option', '', label); option.value = id; this.optionGroup.append(option);
  }
  render() {
    const restore=captureFocus(this.nav,()=>this.search);
    this.nav.replaceChildren(); this.selector.replaceChildren();
    for (const groupName of [...new Set(settingsSections.map(section => section.group)), 'Repositories']) {
      this.optionGroup = node('optgroup'); this.optionGroup.label = groupName; this.selector.append(this.optionGroup);
      const group = node('section', 'settings-nav-group'); group.dataset.group = groupName;
      const heading = node('h2', 'settings-nav-heading');
      heading.textContent = groupName;
      group.append(heading);
      for (const section of settingsSections.filter(section => section.group === groupName)) this.entry(group, section.id, section.label, section.keywords);
      if (groupName === 'Repositories') {
        for (const project of this.projects) this.entry(group, `repository:${project.name}`, project.name, project.path);
        this.entry(group, 'add-repository', 'Add repository', '');
      }
      this.nav.append(group);
    }
    this.select(this.selected); this.setBusy(this.busy); this.filter();
    restore();
  }
  searchChanged() { this.query = this.search.value.trim().toLowerCase(); this.clear.hidden = !this.search.value; this.shortcut.hidden = !!this.search.value; this.filter(); }
  filter() {
    for (const group of this.nav.children) {
      let matches = 0;
      for (const control of group.querySelectorAll('.settings-nav-entry')) { control.hidden = !!this.query && !control.dataset.search.includes(this.query); if (!control.hidden) matches++; }
      group.hidden = !matches;
    }
  }
  select(id) { this.selected = id; this.selector.value = id; for (const control of this.nav.querySelectorAll('[data-section]')) control.setAttribute('aria-pressed', String(control.dataset.section === id)); }
  setBusy(value) { this.busy = !!value; this.selector.disabled = this.busy; for (const control of this.nav.querySelectorAll('button')) control.disabled = this.busy; }
  destroy(clear = false) { this.abort?.abort(); if (clear) { this.projects = []; this.search.value = ''; this.query = ''; this.clear.hidden = true; this.shortcut.hidden = false; this.note.replaceChildren(); this.render(); } }
}
