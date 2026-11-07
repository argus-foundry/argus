import { $ } from '../shared/dom.js';

export class WorkspaceTaskView {
  constructor(composer, archive) { Object.assign(this, { composer, archive }); }
  state(state, task) {
    $('terminal-state').textContent = state.message;
    $('terminal-state').dataset.kind = state.kind || 'normal';
    $('terminal-feedback').classList.toggle('has-feedback', ['error', 'warning'].includes(state.kind));
    $('fit-session').disabled = !state.interactive;
    $('reconnect-terminal').hidden = !task || !['error', 'warning'].includes(state.kind);
    this.composer.setInteractive(state.interactive);
  }
  fit({needed,size,current}) {
    const control = $('fit-session');
    control.dataset.fitNeeded = String(needed);
    control.dataset.tooltip = needed ? `Fit to pane (${size.cols} × ${size.rows}; current ${current.cols} × ${current.rows})` : 'Fit session to the available pane';
  }
  header(task, connected) {
    $('task-title').textContent = task?.name || 'A little room to focus.';
    $('task-title').title = task ? `${task.name}\n${task.project || 'Unassigned project'}` : connected ? 'Choose a task from your workspace.' : 'Connect to your daemon to pick up where you left off.';
    $('agent-empty').hidden = !!task;
    this.archive.selectTask(task);
  }
}
