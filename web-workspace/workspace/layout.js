import { $, preference, savePreference } from '../shared/dom.js';

export class WorkspaceLayout {
  constructor(onChange) {
    this.onChange = onChange;
    this.compact = matchMedia('(max-width:1099px)');
    const saved = preference('layout', {});
    this.visible = this.compact.matches ? { nav: false, tools: false } : { nav: saved.nav !== false, tools: saved.tools !== false };
    this.widths = { nav: this.bound('nav', Number(saved.navWidth) || 256), tools: this.bound('tools', Number(saved.toolsWidth) || 340) };
    this.focusPair = null;
    for (const rail of ['nav', 'tools']) {
      $(`toggle-${rail}`).addEventListener('click', () => this.toggle(rail));
      document.querySelector(`[data-close="${rail}"]`).addEventListener('click', () => this.close(rail));
      this.resize(rail);
    }
    $('focus-agent').addEventListener('click', () => this.focusAgent());
    $('backdrop').addEventListener('click', () => this.closeDrawer());
    document.addEventListener('keydown', event => this.drawerKeys(event));
    this.compact.addEventListener('change', () => {
      this.visible = this.compact.matches ? { nav: false, tools: false } : { nav: true, tools: true };
      this.focusPair = null;
      this.render();
    });
    window.addEventListener('resize', () => this.render());
    this.render();
  }
  bound(rail, value) { return Math.min(rail === 'nav' ? 380 : 520, Math.max(rail === 'nav' ? 220 : 290, value)); }
  toggle(rail) {
    this.focusPair = null;
    this.visible[rail] = !this.visible[rail];
    if (this.compact.matches && this.visible[rail]) this.visible[rail === 'nav' ? 'tools' : 'nav'] = false;
    this.render();
    if (this.compact.matches && this.visible[rail]) this.rail(rail).querySelector('button,input')?.focus();
  }
  close(rail) { this.visible[rail] = false; this.focusPair = null; this.render(); $(`toggle-${rail}`).focus(); }
  closeDrawer() { const rail = this.visible.tools ? 'tools' : 'nav'; this.close(rail); }
  rail(rail) { return $(rail === 'nav' ? 'navigation' : 'tools'); }
  focusAgent() {
    if (this.focusPair) { this.visible = this.focusPair; this.focusPair = null; }
    else { this.focusPair = { ...this.visible }; this.visible = { nav: false, tools: false }; }
    this.render();
  }
  constrainWidths(preferredRail) {
    if (this.compact.matches) return;
    const rails = ['nav', 'tools'].filter(rail => this.visible[rail]);
    const available = $('workspace').clientWidth - 480 - rails.length;
    let overflow = rails.reduce((sum, rail) => sum + this.widths[rail], 0) - available;
    for (const rail of preferredRail ? [preferredRail, preferredRail === 'nav' ? 'tools' : 'nav'] : ['tools', 'nav']) {
      if (!rails.includes(rail) || overflow <= 0) continue;
      const reduction = Math.min(overflow, this.widths[rail] - (rail === 'nav' ? 220 : 290));
      this.widths[rail] -= reduction; overflow -= reduction;
    }
  }
  render(preferredRail) {
    this.constrainWidths(preferredRail);
    const workspace = $('workspace');
    for (const rail of ['nav', 'tools']) {
      workspace.classList.toggle(`${rail}-hidden`, !this.visible[rail]);
      const control = $(`toggle-${rail}`);
      const label = rail === 'nav' ? 'tasks' : 'tools';
      control.textContent = `${this.visible[rail] ? 'Hide' : 'Show'} ${label}`;
      control.setAttribute('aria-expanded', String(this.visible[rail]));
      control.title = `${this.visible[rail] ? 'Hide' : 'Show'} ${label}`;
      workspace.style.setProperty(`--${rail === 'nav' ? 'nav' : 'tools'}-width`, `${this.widths[rail]}px`);
      const other = rail === 'nav' ? 'tools' : 'nav';
      const maximum = Math.min(rail === 'nav' ? 380 : 520, workspace.clientWidth - 480 - 1 - (this.visible[other] ? this.widths[other] + 1 : 0));
      $(`${rail}-resize`).setAttribute('aria-valuemax', String(Math.max(rail === 'nav' ? 220 : 290, maximum)));
      $(`${rail}-resize`).setAttribute('aria-valuenow', String(this.widths[rail]));
    }
    $('focus-agent').setAttribute('aria-pressed', String(!!this.focusPair));
    $('focus-agent').textContent = this.focusPair ? 'Restore layout' : 'Focus agent';
    const drawer = this.compact.matches && (this.visible.nav || this.visible.tools);
    $('backdrop').hidden = !drawer;
    $('main').inert = drawer;
    if (!this.compact.matches) savePreference('layout', { ...this.visible, navWidth: this.widths.nav, toolsWidth: this.widths.tools });
    this.onChange?.();
  }
  resize(rail) {
    const separator = $(`${rail}-resize`);
    separator.addEventListener('keydown', event => {
      let width = this.widths[rail];
      if (event.key === 'Home') width = rail === 'nav' ? 220 : 290;
      else if (event.key === 'End') width = rail === 'nav' ? 380 : 520;
      else if (['ArrowLeft', 'ArrowRight'].includes(event.key)) width += (event.key === 'ArrowRight' ? 1 : -1) * (rail === 'nav' ? 1 : -1) * (event.shiftKey ? 32 : 8);
      else return;
      event.preventDefault();
      this.widths[rail] = this.bound(rail, width);
      this.render(rail);
    });
    separator.addEventListener('pointerdown', event => {
      if (event.button !== 0) return;
      event.preventDefault();
      separator.focus();
      separator.setPointerCapture(event.pointerId);
      const startX = event.clientX, startWidth = this.widths[rail];
      const move = next => {
        this.widths[rail] = this.bound(rail, startWidth + (next.clientX - startX) * (rail === 'nav' ? 1 : -1));
        this.render(rail);
      };
      const end = () => { separator.removeEventListener('pointermove', move); separator.removeEventListener('pointerup', end); separator.removeEventListener('pointercancel', end); };
      separator.addEventListener('pointermove', move);
      separator.addEventListener('pointerup', end);
      separator.addEventListener('pointercancel', end);
    });
  }
  drawerKeys(event) {
    if (!this.compact.matches || !(this.visible.nav || this.visible.tools)) return;
    if (document.querySelector('dialog[open]')) return;
    if (event.key === 'Escape') { event.preventDefault(); this.closeDrawer(); return; }
    if (event.key !== 'Tab') return;
    const header = [...document.querySelector('.workspace-header').querySelectorAll('button:not(:disabled)')];
    const rail = [...this.rail(this.visible.nav ? 'nav' : 'tools').querySelectorAll('button:not(:disabled),input,a,summary,[tabindex="0"]')];
    const items = [...header, ...rail].filter(item => item.getClientRects().length);
    const index = items.indexOf(document.activeElement);
    if (event.shiftKey && index === 0) { event.preventDefault(); items.at(-1)?.focus(); }
    else if (!event.shiftKey && index === items.length - 1) { event.preventDefault(); items[0]?.focus(); }
  }
  taskSelected() { if (this.compact.matches && (this.visible.nav || this.visible.tools)) this.closeDrawer(); }
}
