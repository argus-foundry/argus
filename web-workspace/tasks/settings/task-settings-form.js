import {resolvePR} from '../task-information.js';
import { node, button } from '../../shared/dom.js';
import { taskAccounts, sessionActions } from './task-settings-policy.js';

function field(form, labelText, control) {
  const wrapper = node('div', 'task-settings-field'), label = node('label', '', labelText);
  control.id = 'task-field-' + labelText.toLowerCase().replace(/[^a-z0-9]+/g, '-'); label.htmlFor = control.id;
  wrapper.append(label, control); form.append(wrapper); return control;
}
export function taskSettingsForm(host, data, mutate, confirm, archive, switchSession) {
  const { task, accounts, backends, entries } = data;
  const fields = node('fieldset', 'task-settings-fields'); host.replaceChildren(fields);
  const rename = node('form', 'task-settings-form');
  const name = field(rename, 'Task name', node('input')); name.value = task.name; name.required = true;
  const saveName = button('Save name', () => {}); saveName.type = 'submit'; rename.append(saveName);
  rename.addEventListener('submit', event => { event.preventDefault(); if (name.value.trim()) mutate('rename', { name: name.value.trim() }); });
  const workflow = node('form', 'task-settings-form'); const status = field(workflow, 'Workflow status', node('select'));
  for (const [value, label] of [['pending', 'Pending'], ['in_progress', 'In progress'], ['in_review', 'In review'], ['complete', 'Complete']]) {
    const option = node('option', '', label); option.value = value; status.append(option);
  }
  status.value = task.status;
  const saveStatus = button('Save status', () => {}); saveStatus.type = 'submit'; workflow.append(saveStatus, node('small', 'settings-note', 'Changes workflow status only.'));
  workflow.addEventListener('submit', event => { event.preventDefault(); mutate('status', { status: status.value }); });
  fields.append(rename, workflow, node('h2', '', 'Session details'));
  const facts = node('dl', 'task-settings-facts');
  for (const [label, value] of [['Session ID', task.session_id], ['Sandbox override', task.sandbox_override || 'inherit'], ['Sandbox active', typeof task.sandboxed === 'boolean' ? String(task.sandboxed) : null]]) {
    if (!value) continue;
    const row = node('div'); row.append(node('dt', '', label), node('dd', '', value)); facts.append(row);
  }
  fields.append(facts);
  const pr = resolvePR(entries, data.links);
  if (pr) { const link = node('a', '', 'Open pull request'); link.href = pr; link.target = '_blank'; link.rel = 'noopener noreferrer'; fields.append(link); }
  const sessions=node('div','settings-actions');sessions.id='task-session-actions';
  for (const operation of ['stop', 'resume']) {
    const [label,help]=sessionActions[operation],control=button(label,()=>confirm(operation));
    control.dataset.tooltip=help;control.setAttribute('aria-label',label);sessions.append(control);
  }
  const backend=backends.find(backend=>backend.name===task.backend);
  if(switchSession&&(backend?.session_switcher??backend?.kind==='claude')){const control=button('Switch Claude session',()=>switchSession(task));control.id='switch-claude-session';control.dataset.tooltip='Switch the saved Claude conversation for this task';control.setAttribute('aria-label','Switch Claude session');sessions.append(control);}
  const actions=node('div','settings-actions');
  actions.append(button(task.archived ? 'Restore task' : 'Archive task', () => archive(task)), button('Fork task', () => mutate('fork', {})));
  fields.append(node('h2', '', 'Agent and task actions'),sessions,actions);
  fields.append(node('small', 'settings-note', 'Move account is available only for Claude tasks.'));
  const compatible = taskAccounts(task, accounts, backends);
  if (compatible.length) {
    const form = node('form', 'task-settings-form'); const account = field(form, 'Move to account', node('select'));
    for (const item of compatible) { const option = node('option', '', item.label || item.name); option.value = item.name; account.append(option); }
    const force = field(form, 'Interrupt a busy agent', node('input')); force.type = 'checkbox';
    const move = button('Move account', () => {}); move.type = 'submit'; form.append(move);
    form.addEventListener('submit', event => { event.preventDefault(); confirm('account', { account: account.value, force: force.checked }); });
    fields.append(form);
  }
  fields.append(button('Delete task', () => confirm('delete'), 'task-delete'));
  return fields;
}
