import test from 'node:test';
import assert from 'node:assert/strict';
import { taskIconState, taskStatusIcon } from '../../../workspace/rail/task-status-icon.js';
const cases = [
  ['attention overrides working and PR', { status: 'in_progress', live: true, needs_input: true, pr_state: 'approved' }, undefined, 'attention', 'Needs input'],
  ['descendant attention overrides failure', {}, { needsInput: true, failed: true }, 'attention', 'Descendant needs input'],
  ['failed bound role', {}, { failed: true }, 'failed', 'Failed'],
  ['running overrides open PR', { status: 'in_progress', live: true, pr_state: 'awaiting-review' }, undefined, 'working', 'Running'],
  ['idle without PR', { status: 'in_progress', live: true, idle: true }, undefined, 'neutral', 'Waiting'],
  ['offline is not working', { status: 'in_progress', live: false }, undefined, 'neutral', 'Session offline'],
  ['pending', { status: 'pending' }, undefined, 'neutral', 'Pending'],
  ['draft PR', { pr_state: 'draft' }, undefined, 'pr-open', 'Draft PR'],
  ['open PR', { pr_state: 'awaiting-review' }, undefined, 'pr-open', 'PR open'],
  ['changes requested', { pr_state: 'changes-requested' }, undefined, 'pr-open', 'PR changes requested'],
  ['approved PR', { pr_state: 'approved' }, undefined, 'pr-approved', 'PR approved'],
  ['review without PR', { status: 'in_review' }, undefined, 'neutral', 'In review'],
  ['terminal PR is combined REST state', { pr_state: 'merged-closed' }, undefined, 'merged', 'PR merged or closed'],
  ['complete', { status: 'complete' }, undefined, 'merged', 'Complete'],
  ['unknown PR has no implied review', { status: 'pending', pr_state: 'unknown' }, undefined, 'neutral', 'Pending'],
  ['archived task has no live attention', { archived: true, needs_input: true, pr_state: 'approved' }, undefined, 'neutral', 'Archived'],
];
for (const [name, task, attention, kind, label] of cases) test(name, () => assert.deepEqual(taskIconState(task, attention), { kind, label }));

test('live busy sessions show activity even when workflow status is in review or complete', () => {
  for (const status of ['in_review', 'complete']) assert.deepEqual(taskIconState({ status, live: true, idle: false }), { kind: 'working', label: 'Running' });
});

function svgDocument(t) {
  const original = globalThis.document;
  t.after(() => { globalThis.document = original; });
  globalThis.document = {
    createElementNS(namespaceURI, tagName) {
      return {
        namespaceURI, tagName, attributes: {}, dataset: {}, children: [],
        classList: { add() {} },
        setAttribute(name, value) { this.attributes[name] = String(value); },
        append(child) { this.children.push(child); },
      };
    },
  };
}

test('working Scribe preserves its accessible identity and approved line and nib geometry', t => {
  svgDocument(t);
  const icon = taskStatusIcon({ live: true, idle: false });
  assert.equal(icon.dataset.status, 'working');
  assert.equal(icon.dataset.tooltip, 'Running');
  assert.equal(icon.attributes['aria-label'], 'Running');
  assert.equal(icon.attributes.role, 'img');
  assert.equal(icon.attributes.viewBox, '0 0 16 16');
  assert.equal(icon.children.length, 5);
  const [ghost, ...drawing] = icon.children;
  assert.deepEqual(ghost.attributes, { class: 'task-scribe-ghost', d: 'M2 4h6M2 8h7M2 12h5' });
  const nib = drawing.pop();
  assert.deepEqual(drawing.map(line => line.attributes), [
    { class: 'task-scribe-line', d: 'M2 4h6', pathLength: '10' },
    { class: 'task-scribe-line task-scribe-two', d: 'M2 8h7', pathLength: '10' },
    { class: 'task-scribe-line task-scribe-three', d: 'M2 12h5', pathLength: '10' },
  ]);
  for (const line of [ghost, ...drawing]) {
    assert.equal(line.tagName, 'path');
    assert.equal(line.namespaceURI, icon.namespaceURI);
  }
  assert.equal(nib.tagName, 'g');
  assert.equal(nib.attributes.class, 'task-scribe-nib');
  assert.equal(nib.children.length, 1);
  assert.equal(nib.children[0].tagName, 'path');
  assert.equal(nib.children[0].attributes.d, 'm11 1.8 2.8 2.8L12 6.4 9.2 3.6Z');
});

test('higher precedence icons retain their path glyph instead of the Scribe', t => {
  svgDocument(t);
  for (const [task, state, kind, label] of [
    [{ live: true, archived: true }, undefined, 'neutral', 'Archived'],
    [{ live: true, needs_input: true }, undefined, 'attention', 'Needs input'],
    [{ live: true }, { failed: true }, 'failed', 'Failed'],
    [{ live: true, pr_state: 'merged-closed' }, undefined, 'merged', 'PR merged or closed'],
  ]) {
    const icon = taskStatusIcon(task, state);
    assert.equal(icon.dataset.status, kind);
    assert.equal(icon.attributes['aria-label'], label);
    assert.equal(icon.children.length, 1);
    assert.equal(icon.children[0].tagName, 'path');
    assert.ok(icon.children[0].attributes.d);
  }
});

test('idle review without a cached PR stays neutral', () => {
  assert.deepEqual(taskIconState({ status: 'in_review', live: true, idle: true }), { kind: 'neutral', label: 'In review' });
});

test('terminal PR state and input attention retain their precedence over activity', () => {
  assert.deepEqual(taskIconState({ status: 'in_review', live: true, pr_state: 'merged-closed' }), { kind: 'merged', label: 'PR merged or closed' });
  assert.deepEqual(taskIconState({ status: 'in_review', live: true, needs_input: true }), { kind: 'attention', label: 'Needs input' });
});
