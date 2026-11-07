import test from 'node:test';
import assert from 'node:assert/strict';
import { taskParents, taskForest, readHierarchy } from '../../../workspace/rail/task-hierarchy.js';
import { taskIconState } from '../../../workspace/rail/task-status-icon.js';
import { workspaceShortcutAction } from '../../../workspace/shortcuts/workspace-shortcut-actions.js';
const role = (role_id, kind, task_id, extra = {}) => ({ role_id, kind, task_id, live: true, ...extra });
const tasks = ['a', 'b', 'c', 'd', 'free'].map(id => ({ id }));

test('nested bridges bind different coordinator tasks once and roll up blocked roles', () => {
  const roster = { orchestrators: [
    { id: 2, bridge_parent_orch_id: 1, bridge_parent_role_id: 2, roles: [role(3, 'coordinator', 'c'), role(4, 'worker', 'd', { status: 'blocked' })] },
    { id: 1, roles: [role(1, 'coordinator', 'a'), role(2, 'worker', 'b'), role(5, 'freelance', 'free')] },
  ] };
  const hierarchy = taskParents(tasks, roster);
  assert.deepEqual([...hierarchy.parents], [['b', 'a'], ['d', 'c'], ['c', 'b']]);
  const forest = taskForest(tasks, hierarchy);
  assert.deepEqual(forest.map(entry => entry.task.id), ['a', 'free']);
  assert.equal(forest[0].needsInput, true);
  assert.equal(forest[0].children[0].children[0].children[0].task.id, 'd');
  assert.equal(forest[1].needsInput, false);
});

test('missing, detached and cyclic bindings keep every task reachable', () => {
  const hierarchy = taskParents(tasks, { orchestrators: [
    { id: 1, roles: [role(1, 'coordinator', 'a'), role(2, 'worker', 'b'), role(3, 'worker', 'c', { live: false })] },
    { id: 2, roles: [role(4, 'coordinator', 'b'), role(5, 'worker', 'a')] },
    { id: 3, roles: [role(6, 'coordinator', 'missing'), role(7, 'worker', 'd')] },
  ] });
  assert.deepEqual([...hierarchy.parents], [['b', 'a']]);
  assert.equal(taskForest(tasks, hierarchy).length, 4);
});

test('multiple bindings choose a deterministic first parent and self bridges add no cycle', () => {
  const hierarchy = taskParents(tasks, { orchestrators: [
    { id: 2, bridge_parent_orch_id: 1, bridge_parent_role_id: 2, roles: [role(3, 'coordinator', 'b'), role(4, 'worker', 'c')] },
    { id: 1, roles: [role(1, 'coordinator', 'a'), role(2, 'worker', 'b'), role(5, 'worker', 'c')] },
  ] });
  assert.deepEqual([...hierarchy.parents], [['b', 'a'], ['c', 'a']]);
});

test('filtered parents promote children and archived children never roll up attention', () => {
  const hierarchy = taskParents(tasks, { orchestrators: [{ id: 1, roles: [role(1, 'coordinator', 'a'), role(2, 'worker', 'b')] }] });
  const forest = taskForest([{ id: 'a' }, { id: 'b', archived: true, needs_input: true }], hierarchy);
  assert.equal(forest[0].needsInput, false);
  assert.equal(taskForest([{ id: 'b' }], hierarchy)[0].task.id, 'b');
});

test('roster reads log counts and degrade non-auth failures', async () => {
  const signal = new AbortController().signal;
  const roster = { orchestrators: [] };
  assert.deepEqual(await readHierarchy({ json: async () => roster }, signal), { roster });
  const result = await readHierarchy({ json: async () => { throw new Error('unavailable'); } }, signal);
  assert.match(result.warning, /Task nesting unavailable/);
  assert.deepEqual(result.roster, {});
});

for (const status of [401, 403]) test(`roster authentication failure ${status} propagates`, async () => {
  const error = Object.assign(new Error('auth'), { status });
  await assert.rejects(readHierarchy({ json: async () => { throw error; } }, new AbortController().signal), error);
});

test('aborted roster requests propagate without a warning fallback', async () => {
  const abort = new AbortController(); abort.abort();
  const error = new Error('aborted');
  await assert.rejects(readHierarchy({ json: async () => { throw error; } }, abort.signal), error);
});

test('only live non-archived failed roles supply a task failure signal', () => {
  const hierarchy = taskParents(tasks, { orchestrators: [{ id: 1, roles: [
    role(1, 'worker', 'a', { status: 'failed' }),
    role(2, 'worker', 'b', { status: 'failed', archived: true }),
    role(3, 'worker', 'c', { status: 'failed', live: false }),
  ] }] });
  assert.deepEqual([...hierarchy.failed], ['a']);
});

for (const [extra, icon] of [
  [{ status: 'blocked' }, 'attention'],
  [{ needs_input: true }, 'attention'],
  [{ status: 'idle' }, 'neutral'],
  [{ status: 'failed' }, 'failed'],
  [{ status: 'failed', archived: true }, 'neutral'],
  [{ status: 'failed', live: false }, 'neutral'],
  [{ status: 'blocked', archived: true }, 'neutral'],
  [{ status: 'blocked', live: false }, 'neutral'],
]) {
  test(`nested and hoisted freelance attention agree for ${JSON.stringify(extra)}`, async () => {
    const task = { id: 'free', needs_input: false };
    const binding = role(5, 'freelance', task.id, { orch_id: 1, ...extra });
    const results = [];
    for (const hoisted of [false, true]) {
      const roster = { orchestrators: [{ id: 1, roles: hoisted ? [] : [binding] }], freelance: hoisted ? [binding] : [] };
      const hierarchy = taskParents([task], roster);
      const entry = taskForest([task], hierarchy)[0];
      let selected;
      await workspaceShortcutAction('attention', {
        state: () => ({ tasks: [task] }), navigation: { hierarchy }, selectTask: task => { selected = task.id; },
      });
      results.push({ attention: [...hierarchy.attention], needsInput: entry.needsInput, failed: [...hierarchy.failed], icon: taskIconState(task, { ...entry, failed: hierarchy.failed.has(task.id) }).kind, selected });
    }
    assert.deepEqual(results[1], results[0]);
    assert.equal(results[1].needsInput, icon === 'attention');
    assert.equal(results[1].icon, icon);
    assert.equal(results[1].selected, icon === 'attention' ? task.id : undefined);
  });
}
