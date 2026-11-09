export function taskIconState(task, { needsInput = task.needs_input, failed = !!task.failure } = {}) {
  if (task.archived) return { kind: 'neutral', label: 'Archived' };
  if (needsInput) return { kind: 'attention', label: task.needs_input ? 'Needs input' : 'Descendant needs input' };
  if (failed) return { kind: 'failed', label: 'Failed' };
  if (task.pr_state === 'merged-closed') return { kind: 'merged', label: 'PR merged or closed' };
  if (task.live && !task.idle) return { kind: 'working', label: 'Running' };
  if (task.status === 'complete') return { kind: 'merged', label: 'Complete' };
  if (task.pr_state === 'approved') return { kind: 'pr-approved', label: 'PR approved' };
  const prLabel = { draft: 'Draft PR', 'awaiting-review': 'PR open', 'changes-requested': 'PR changes requested' }[task.pr_state];
  if (prLabel) return { kind: 'pr-open', label: prLabel };
  if (task.status === 'in_review') return { kind: 'neutral', label: 'In review' };
  return { kind: 'neutral', label: task.status === 'pending' ? 'Pending' : task.idle ? 'Waiting' : 'Session offline' };
}

const branch = 'M4 3v7m0-7a1.5 1.5 0 1 0 0 .01M4 11a1.5 1.5 0 1 0 0 .01M12 3a1.5 1.5 0 1 0 0 .01M12 4.5c0 4-8 1-8 6';
const pullRequest = 'M4 3v8m0-8a1.5 1.5 0 1 0 0 .01M4 11a1.5 1.5 0 1 0 0 .01M12 11a1.5 1.5 0 1 0 0 .01M12 9.5V6a3 3 0 0 0-3-3H8m2-2L8 3l2 2';
const paths = {
  neutral: branch,
  'pr-open': pullRequest,
  'pr-approved': 'M4 3v8m0-8a1.5 1.5 0 1 0 0 .01M4 11a1.5 1.5 0 1 0 0 .01M8 7l2 2 4-5',
  merged: 'M4 3v8m0-8a1.5 1.5 0 1 0 0 .01M4 11a1.5 1.5 0 1 0 0 .01M12 11a1.5 1.5 0 1 0 0 .01M4 4.5c0 4 8 1 8 6',
  attention: 'M8 1.5a6.5 6.5 0 1 0 0 13 6.5 6.5 0 1 0 0-13M8 4.5v4M8 11h.01',
  failed: 'M8 1.5a6.5 6.5 0 1 0 0 13 6.5 6.5 0 1 0 0-13M5.5 5.5l5 5m0-5-5 5',
};

export function taskStatusIcon(task, state) {
  const { kind, label } = taskIconState(task, state);
  const icon = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  icon.classList.add('task-status-icon');
  icon.dataset.status = kind;
  icon.setAttribute('viewBox', '0 0 16 16');
  icon.setAttribute('role', 'img');
  icon.setAttribute('aria-label', label);
  icon.dataset.tooltip = label;
  if (kind === 'working') {
    const strokes = [
      ['task-scribe-ghost', 'M2 4h6M2 8h7M2 12h5'],
      ['task-scribe-line', 'M2 4h6'],
      ['task-scribe-line task-scribe-two', 'M2 8h7'],
      ['task-scribe-line task-scribe-three', 'M2 12h5'],
    ];
    for (const [className, geometry] of strokes) {
      const line = document.createElementNS(icon.namespaceURI, 'path');
      line.setAttribute('class', className);
      line.setAttribute('d', geometry);
      if (className !== 'task-scribe-ghost') line.setAttribute('pathLength', 10);
      icon.append(line);
    }
    const nib = document.createElementNS(icon.namespaceURI, 'g');
    nib.setAttribute('class', 'task-scribe-nib');
    const tip = document.createElementNS(icon.namespaceURI, 'path');
    tip.setAttribute('d', 'm11 1.8 2.8 2.8L12 6.4 9.2 3.6Z');
    nib.append(tip);
    icon.append(nib);
    return icon;
  }
  const path = document.createElementNS(icon.namespaceURI, 'path');
  path.setAttribute('d', paths[kind]);
  icon.append(path);
  return icon;
}
