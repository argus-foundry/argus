import test from 'node:test';
import assert from 'node:assert/strict';
import { TerminalStream } from '../../terminal/terminal-stream.js';
import { AgentTerminal } from '../../terminal/terminal.js';

const liveTask = { id: 'isolated-recovery', status: 'in_progress', live: true };
const settle = () => new Promise(setImmediate);

function fixture(t, taskState = () => liveTask) {
  t.mock.timers.enable({ apis: ['setTimeout', 'Date'], now: 100000 });
  const original = globalThis.EventSource;
  const sources = [], states = [], replacements = [];
  globalThis.EventSource = class {
    constructor(url) { this.url = url; this.events = {}; sources.push(this); }
    addEventListener(name, handler) { this.events[name] = handler; }
    close() { this.closed = true; }
  };
  const api = {
    json: async path => path.endsWith('/size') ? { cols: 100, rows: 30 } : taskState(),
    request: async () => new Response('fresh output', { headers: { 'X-Output-Total': '12' } }),
    streamUrl: path => path,
  };
  const stream = new TerminalStream(api, liveTask, new AbortController().signal, {
    write: async () => {}, replace: async bytes => replacements.push(bytes),
    state: (...args) => states.push(args),
  });
  t.after(() => { stream.destroy(); globalThis.EventSource = original; });
  stream.open();
  sources[0].onopen();
  return { stream, sources, states, replacements };
}

test('ten stable seconds restores the retry budget for a later outage', t => {
  const { stream } = fixture(t);
  stream.attempts = 4;
  t.mock.timers.tick(9999);
  assert.equal(stream.attempts, 4);
  t.mock.timers.tick(1);
  assert.equal(stream.attempts, 0);
});

test('actual nonempty stream output restores the retry budget immediately', t => {
  const { stream, sources } = fixture(t);
  stream.attempts = 5;
  sources[0].onmessage({ data: '' });
  assert.equal(stream.attempts, 5);
  sources[0].onmessage({ data: Buffer.from('healthy output').toString('base64') });
  assert.equal(stream.attempts, 0);
});

test('immediately flapping opens remain bounded despite successful snapshot refresh', async t => {
  const { stream, sources, states } = fixture(t);
  for (let attempt = 1; attempt <= 7; attempt++) {
    const source = sources.at(-1);
    source.onopen();
    source.onerror();
    t.mock.timers.tick(4000);
    await settle();
    assert.equal(stream.attempts, attempt);
  }
  assert.equal(sources.length, 7);
  assert.equal(states.at(-1)[1], 'error');
  t.mock.timers.tick(60000);
  await settle();
  assert.equal(sources.length, 7);
  assert.equal(stream.attempts, 7);
});

test('rerender exit waits for live replacement then replays fresh history', async t => {
  let polls = 0;
  const { stream, sources, replacements, states } = fixture(t, () => ({ ...liveTask, live: ++polls > 1 }));
  sources[0].events.exit({ data: '{"rerendering":true}' });
  assert.equal(sources[0].closed, true);
  assert.equal(stream.recovering, true);
  t.mock.timers.tick(1000);
  await settle();
  assert.equal(sources.length, 1);
  t.mock.timers.tick(1000);
  await settle();
  assert.equal(sources.length, 2);
  assert.match(sources[1].url, /since=12$/);
  assert.equal(replacements.length, 1);
  assert.equal(stream.recovering, false);
  sources[1].onopen();
  assert.deepEqual(states.at(-1), ['Live terminal', 'live', true]);
});

test('ordinary exit stays ended and never automatically reopens', async t => {
  const { stream, sources, states } = fixture(t);
  sources[0].events.exit({ data: '{"rerendering":false}' });
  assert.equal(stream.recovering, false);
  assert.deepEqual(states.at(-1), ['Session ended', 'ended', false]);
  t.mock.timers.tick(30000);
  await settle();
  assert.equal(sources.length, 1);
});

test('rerender timeout clears recovery and permits a later recovery attempt', async t => {
  const { stream, sources, states } = fixture(t, () => ({ ...liveTask, live: false }));
  sources[0].events.exit({ data: '{"rerendering":true}' });
  for (let second = 0; second < 15; second++) {
    t.mock.timers.tick(1000);
    await settle();
  }
  assert.equal(stream.recovering, false);
  assert.equal(states.at(-1)[1], 'error');
  assert.match(states.at(-1)[0], /restart timed out/);
  assert.equal(sources.length, 1);
  stream.beginRecovery();
  assert.equal(stream.recovering, true);
});

test('task ending while rerender recovery polls clears recovery', async t => {
  const { stream, sources, states } = fixture(t, () => ({ ...liveTask, status: 'in_review', live: false }));
  sources[0].events.exit({ data: '{"rerendering":true}' });
  t.mock.timers.tick(1000);
  await settle();
  assert.equal(stream.recovering, false);
  assert.deepEqual(states.at(-1), ['Session ended', 'ended', false]);
  assert.equal(sources.length, 1);
});

test('resize expectation handles older exit payload once and is consumed by recovery', async t => {
  const { stream, sources, states } = fixture(t);
  stream.expectRerenderUntil = Date.now() + 20000;
  sources[0].events.exit({ data: '{}' });
  assert.equal(stream.recovering, true);
  assert.equal(stream.expectRerenderUntil, 0);
  t.mock.timers.tick(1000);
  await settle();
  assert.equal(sources.length, 2);
  sources[1].events.exit({ data: '{"rerendering":false}' });
  assert.deepEqual(states.at(-1), ['Session ended', 'ended', false]);
});

test('expired resize expectation cannot misclassify a later ordinary exit', t => {
  const { stream, sources, states } = fixture(t);
  stream.expectRerenderUntil = Date.now() - 1;
  sources[0].events.exit({ data: '{}' });
  assert.equal(stream.recovering, false);
  assert.deepEqual(states.at(-1), ['Session ended', 'ended', false]);
});

test('same-task offline/live refresh supersedes recovery and allows another rerender', async t => {
  const { stream, sources, states } = fixture(t);
  const terminal = new AgentTerminal({}, {});
  Object.assign(terminal, { task: liveTask, abort: new AbortController(), stream });
  stream.beginRecovery();
  await terminal.attach({ ...liveTask, live: false });
  await terminal.attach(liveTask);
  t.mock.timers.tick(500);
  await settle();
  assert.equal(sources.length, 2);
  assert.equal(stream.recovering, false);
  sources[1].onopen();
  sources[1].events.exit({ data: '{"rerendering":true}' });
  assert.equal(stream.recovering, true);
  assert.equal(sources[1].closed, true);
  t.mock.timers.tick(1000);
  await settle();
  assert.equal(sources.length, 3);
  assert.equal(stream.recovering, false);
  sources[2].onopen();
  assert.deepEqual(states.at(-1), ['Live terminal', 'live', true]);
});

test('superseded recovery completion cannot clear a newer recovery', async t => {
  const { stream, sources } = fixture(t);
  let release;
  stream.hooks.replace = () => new Promise(resolve => { release = resolve; });
  stream.beginRecovery();
  t.mock.timers.tick(1000);
  await settle();
  assert.equal(typeof release, 'function');
  stream.hooks.replace = async () => {};
  stream.reconnect();
  t.mock.timers.tick(500);
  await settle();
  sources.at(-1).onopen();
  sources.at(-1).events.exit({ data: '{"rerendering":true}' });
  assert.equal(stream.recovering, true);
  release();
  await settle();
  assert.equal(stream.recovering, true);
  t.mock.timers.tick(1000);
  await settle();
  assert.equal(sources.length, 3);
  assert.equal(stream.recovering, false);
});
