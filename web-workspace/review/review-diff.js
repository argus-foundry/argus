import { node, button } from '../shared/dom.js';
import { diffLines, splitLines } from '../tasks/changes/git.js';

const DIFF_LINE_LIMIT = 6000;

export function renderDiff(container, selection, onMode) {
  const focused = container.querySelector('.mode-group')?.contains(document.activeElement) ? document.activeElement.textContent : null;
  const toolbar = node('div', 'review-toolbar');
  const title=node('strong', 'review-title', selection.path);title.dataset.tooltip=selection.path;title.dataset.tooltipOverflow='';title.setAttribute('aria-label', selection.path);toolbar.append(title);
  const modes = node('div', 'mode-group');
  modes.setAttribute('role', 'group');
  modes.setAttribute('aria-label', 'Diff layout');
  for (const mode of ['unified', 'split']) {
    const control = button(mode === 'unified' ? 'Unified' : 'Split', () => onMode(mode));
    control.setAttribute('aria-pressed', String(selection.mode === mode));
    modes.append(control);
  }
  toolbar.append(modes);
  container.replaceChildren(toolbar, node('div', 'review-caption', 'Returned git diff · file contents and comparisons can change as the agent works'));
  if (focused) [...modes.children].find(control => control.textContent === focused)?.focus({ preventScroll: true });
  if (!selection.diff) { container.append(node('div', 'empty-state', 'No diff text was returned. This file may be binary, removed, unavailable, or unchanged in the current comparison.')); return; }
  const allLines = diffLines(selection.diff, DIFF_LINE_LIMIT + 1);
  const lines = allLines.slice(0, DIFF_LINE_LIMIT);
  const scroll = node('div', 'diff-content');
  const table = node('table', `diff-table ${selection.mode}`);
  table.setAttribute('aria-label', `${selection.mode} diff of ${selection.path}`);
  const body = node('tbody');
  const header = node('tr', 'meta');
  const labels = selection.mode === 'split' ? ['Old line', 'Before', 'New line', 'After'] : ['Old line', 'New line', 'Content'];
  for (const label of labels) {
    const cell = node('th', '', label);
    cell.setAttribute('scope', 'col');
    header.append(cell);
  }
  if (selection.mode === 'split') {
    body.append(header);
    for (const { left, right } of splitLines(lines)) {
      const row = node('tr');
      for (const [line, side] of [[left, 'left'], [right, 'right']]) {
        row.append(node('td', `line-number ${line?.kind || 'blank'}`, String((side === 'left' ? line?.oldNumber : line?.newNumber) ?? '')));
        row.append(node('td', `diff-code ${line?.kind || 'blank'}`, line?.text ?? ''));
      }
      body.append(row);
    }
  } else {
    const head = node('thead');
    head.append(header);
    table.append(head);
    for (const line of lines) {
      const row = node('tr', line.kind);
      row.append(node('td', 'line-number', String(line.oldNumber)), node('td', 'line-number', String(line.newNumber)), node('td', 'diff-code', line.text));
      body.append(row);
    }
  }
  table.append(body);
  scroll.append(table);
  container.append(scroll);
  if (lines.length < allLines.length) container.append(node('p', 'diff-overflow-note', `Showing the first ${lines.length.toLocaleString()} lines. Use the original UI or a local editor for the full diff.`));
}
