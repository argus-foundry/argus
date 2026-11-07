import {node, button} from '../../shared/dom.js';
import {taskStatusIcon} from './task-status-icon.js';
import {roleStatus} from './task-grouping.js';

export function compactTaskRow({ task, role, selected, needsInput, ownInput, failed, pinned, select }) {
  const row = button('', () => { if (task) select(task); }, 'task-row');
  row.disabled = !task;row.classList.toggle('needs-attention',!!needsInput&&!task?.archived);
  const name = task?.name || role?.name || 'Task unavailable';
  row.dataset.tooltip = name; row.dataset.tooltipOverflow = '';
  if (task) row.dataset.taskId = task.id;
  if (role) row.dataset.roleId=String(role.role_id);
  if (task?.id === selected) row.setAttribute('aria-current', 'page');
  const own=ownInput ?? (!task?.archived && (!!task?.needs_input || !role?.archived && (!!role?.needs_input || role?.status==='blocked')));
  row.append(taskStatusIcon({ ...task, needs_input: !!own }, { needsInput, failed }), node('span', 'task-name', name));
  if (task?.archive_pending) {
    const pending = node('span','task-archive-pending','·');
    pending.setAttribute('aria-label','Archive pending');pending.dataset.tooltip='Archive pending. Checking current task state.';row.append(pending);
  }
  const status = roleStatus(role);
  if (status) { const tag = node('span', 'task-role-tag', status==='unbound'?'Unavailable':status); tag.dataset.roleStatus = status; tag.dataset.tooltip = `Agent ${status==='unbound'?'unavailable':status}`; tag.setAttribute('aria-label', tag.dataset.tooltip); row.append(tag); }
  if (pinned) { const pin = node('span', 'task-pin', '⌖'); pin.setAttribute('aria-label', 'Pinned'); pin.dataset.tooltip = 'Pinned'; row.append(pin); }
  row.setAttribute('aria-label', [name, ...[...row.querySelectorAll('[aria-label]')].map(icon => icon.getAttribute('aria-label'))].join(', '));
  return row;
}
