import {copyNotice} from '../../shared/clipboard.js';
import { shortcutAction, shortcutHelp } from './shortcuts-policy.js';
import { $, node, button, notice } from '../../shared/dom.js';
import { log } from '../../core/api.js';

export class WorkspaceShortcuts {
  constructor(actions, terminal) {
    Object.assign(this, { actions, terminal });
    this.help = button('', () => this.showHelp(), 'footer-gear');
    const icon = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    icon.setAttribute('viewBox', '0 0 24 24'); icon.setAttribute('aria-hidden', 'true');
    const keys = document.createElementNS(icon.namespaceURI, 'path');
    keys.setAttribute('d', 'M3 5h18v14H3zM6 9h1m3 0h1m3 0h1m3 0h.01M6 12h1m3 0h1m3 0h1m3 0h.01M7 15h10');
    icon.append(keys); this.help.append(icon);
    this.help.setAttribute('aria-label', 'Keyboard shortcuts');
    this.help.dataset.tooltip = 'Keyboard shortcuts'; this.help.dataset.tooltipShortcut = '⌥⇧?';
    this.help.id = 'shortcuts-help'; $('workspace-settings').before(this.help);
    this.handle = event => {
      if (event.defaultPrevented || document.querySelector('dialog[open]')) return;
      const inTerminal = !!event.target.closest?.('.xterm');
      const action = shortcutAction(event, inTerminal);
      if (!action || (!inTerminal && event.target.closest?.('input,textarea,select,[contenteditable="true"]') && !['filter','help'].includes(action))) return;
      if (!this.actions.connected()) return;
      event.preventDefault(); event.stopImmediatePropagation();
      Promise.resolve().then(() => this.run(action, event.target)).catch(error => { notice(error.message); log('shortcuts','action failed'); });
    };
    window.addEventListener('keydown', this.handle, true);
  }
  run(action, target) {
    const term = this.terminal(target)?.term;
    const scroll = { 'line-up':-1, 'line-down':1, 'page-up':-(term?.rows || 24), 'page-down':term?.rows || 24 };
    if (action in scroll) { term?.scrollLines(scroll[action]); return; }
    if (action === 'bottom') { term?.scrollToBottom(); return; }
    if (action === 'copy-output') {
      if (!term) return;
      const buffer = term.buffer.active, lines = [];
      for (let index = buffer.viewportY; index < buffer.viewportY + term.rows; index++) lines.push(buffer.getLine(index)?.translateToString(true) || '');
      return copyNotice(lines.join('\n'),'Copied visible output.');
    }
    log('shortcuts', 'activated');
    if (action === 'help') this.showHelp(); else return this.actions.run(action);
  }
  showHelp() {
    this.dialog?.remove();
    const dialog = this.dialog = node('dialog'); dialog.id = 'shortcuts-dialog'; dialog.setAttribute('aria-label','Keyboard shortcuts');
    dialog.append(node('h2','','Keyboard shortcuts'));
    for (const [chord, label] of shortcutHelp) { const row = node('p'); row.append(node('kbd','',chord),document.createTextNode(`  ${label}`)); dialog.append(row); }
    dialog.append(node('p','tool-note','Browser equivalents use Alt. Terminal focus claims only the listed Cmd and Shift chords. Text fields keep native editing.'));
    dialog.append(button('Close',()=>dialog.close()));
    dialog.addEventListener('close',()=>{dialog.remove();this.help.focus();});
    document.body.append(dialog); dialog.showModal();
  }
}
