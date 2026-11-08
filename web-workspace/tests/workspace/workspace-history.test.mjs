import test from 'node:test';
import assert from 'node:assert/strict';
import { WorkspaceHistory } from '../../workspace/workspace-history.js';
import { restoreWorkspaceView } from '../../workspace/workspace-route.js';

function browserHistory(t) {
  const entries = [{ url: 'http://localhost/?task=a', state: null }];
  let cursor = 0, pop;
  const native = {
    get state() { return entries[cursor].state; },
    pushState(state, _, url) { entries.splice(++cursor); entries.push({ state, url: String(url) }); },
    replaceState(state, _, url) { entries[cursor] = { state, url: String(url) }; },
    go(delta) { cursor += delta; assert.ok(entries[cursor]); queueMicrotask(() => pop()); },
  };
  const previous = { window: globalThis.window, location: globalThis.location };
  globalThis.location = { get href() { return entries[cursor].url; } };
  globalThis.window = { history: native, addEventListener: (_, handler) => { pop = handler; } };
  t.after(() => Object.assign(globalThis, previous));
  return { native, entries };
}

for (const target of [{ task: 'b' }, { settings: 'sandbox' }, { taskSettings: 'b' }]) {
  test(`history restoration checks Settings save boundary before ${JSON.stringify(target)}`, () => {
    const untouched = () => assert.fail('Pending save view must remain mounted');
    assert.equal(restoreWorkspaceView(target, {
      settings: { canLeave: () => false, dismiss: untouched, open: untouched },
      newTask: { close: untouched }, taskSettings: { dismiss: untouched, open: untouched },
      tasks: [], selectTask: untouched, unavailable: untouched,
    }), false);
  });
}

for (const direction of [-1, 1]) test(`refused ${direction === -1 ? 'Back' : 'Forward'} restores the entry and allows a later traversal`, async t => {
  const { native, entries } = browserHistory(t);
  let busy = false;
  const restored = [];
  const history = new WorkspaceHistory(state => { restored.push(state); return !busy; });
  history.write({ id: 'b' }, 'agent');
  if (direction === 1) { native.go(-1); await Promise.resolve(); }
  const currentURL = location.href, currentState = native.state;
  busy = true;
  native.go(direction);
  await Promise.resolve(); await Promise.resolve();
  assert.equal(location.href, currentURL);
  assert.deepEqual(native.state, currentState);
  assert.equal(entries.length, 2);
  assert.equal(restored.length, direction === 1 ? 2 : 1);
  busy = false;
  native.go(direction); await Promise.resolve();
  assert.equal(new URL(location.href).searchParams.get('task'), direction === -1 ? 'a' : 'b');
});

test('Settings and task-settings history writes retain entry positions and surface markers', async t => {
  const { native, entries } = browserHistory(t);
  const history = new WorkspaceHistory(() => {});
  history.push('http://localhost/?task=a&settings=general', { workspaceSettings: true });
  history.replace('http://localhost/?task=a&settings=sandbox', native.state);
  assert.deepEqual(native.state, { workspacePosition: 1, workspaceSettings: true });
  history.push('http://localhost/?task_settings=a', { taskSettings: true });
  assert.deepEqual(native.state, { workspacePosition: 2, taskSettings: true });
  native.go(-1); await Promise.resolve();
  history.replace('http://localhost/?task=a');
  assert.deepEqual(native.state, { workspacePosition: 1 });
  assert.equal(entries.length, 3);
});

test('restoration callbacks replace the traversed entry using its own position', async t => {
  const { native } = browserHistory(t);
  const history = new WorkspaceHistory(() => history.replace(location.href));
  history.write({ id: 'b' }, 'agent');
  native.go(-1); await Promise.resolve();
  assert.equal(native.state.workspacePosition, 0);
});
