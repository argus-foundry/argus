import test from 'node:test';
import assert from 'node:assert/strict';
import { TerminalInput } from '../../terminal/terminal-input.js';
import { TerminalStream } from '../../terminal/terminal-stream.js';

const task = { id: 'task-a', status: 'in_progress', live: true };

test('composed messages serialize paste and submit without mixing commands', async () => {
  const writes = [];
  const api = { inputTask: () => task, json: async () => task, request: async (path, options) => writes.push({ path, body: options.body }) };
  const input = new TerminalInput(api, task, new AbortController().signal, () => true);
  await Promise.all([input.send('one'), input.send('two')]);
  assert.deepEqual(writes.map(value => value.body), ['\x1b[200~one\x1b[201~', '\r', '\x1b[200~two\x1b[201~', '\r']);
  assert.equal(writes.every(value => value.path === '/api/tasks/task-a/input'), true);
});

test('switch after paste cancels submit rather than delivering enter to another task', async () => {
  const writes = [];
  const abort = new AbortController();
  const api = { inputTask: () => task, json: async () => task, request: async (path, options) => {
    writes.push({ path, body: options.body });
    abort.abort();
  } };
  const input = new TerminalInput(api, task, abort.signal, () => true);
  await assert.rejects(input.send('partially pasted message'));
  assert.equal(writes.length, 1);
  assert.equal(writes[0].path, '/api/tasks/task-a/input');
  assert.doesNotMatch(writes[0].body, /\r/);
});

test('dead compose targets reject and pasted Ctrl+Z is removed before the input endpoint', async () => {
  const writes = [];
  const api = { inputTask: () => task, json: async () => ({ ...task, live: false }), request: async (path,options) => writes.push({path,body:options.body}) };
  const input = new TerminalInput(api, task, new AbortController().signal, () => true);
  await assert.rejects(input.send('message'));
  assert.deepEqual(writes, []);
  await input.raw('prefix\x1asuffix');
  assert.deepEqual(writes, [{path:'/api/tasks/task-a/input',body:'prefixsuffix'}]);
});

test('snapshot cursor, raw UTF-8 bytes and stream continuation form one history', async t => {
  const original = globalThis.EventSource;
  const sources = [];
  globalThis.EventSource = class {
    constructor(url) { this.url = url; sources.push(this); this.events = {}; }
    addEventListener(name, handler) { this.events[name] = handler; }
    close() { this.closed = true; }
  };
  t.after(() => { globalThis.EventSource = original; });
  const writes = [], states = [];
  const api = {
    request: async () => new Response(new Uint8Array([27, 91, 48, 109, 0xe2, 0x9c, 0x93]), { headers: { 'X-Output-Total': '700' } }),
    streamUrl: path => path,
  };
  const stream = new TerminalStream(api, task, new AbortController().signal, {
    write: async bytes => writes.push([...bytes]), state: (...state) => states.push(state),
  });
  t.after(() => stream.destroy());
  await stream.start();
  assert.match(sources[0].url, /since=700$/);
  sources[0].onopen();
  sources[0].onmessage({ data: Buffer.from([0xe2, 0x9c, 0x93]).toString('base64') });
  assert.equal(stream.cursor, 703);
  sources[0].onerror();
  assert.equal(sources[0].closed, true);
  assert.equal(states.at(-1)[1], 'reconnecting');
  stream.open();
  assert.match(sources[1].url, /since=703$/);
  assert.deepEqual(writes, [[27, 91, 48, 109, 226, 156, 147], [226, 156, 147]]);
});

test('late snapshot after task selection changes cannot open the old stream', async t => {
  let resolveSnapshot;
  const api = { request: () => new Promise(resolve => { resolveSnapshot = resolve; }) };
  const abort = new AbortController();
  let writes = 0;
  const stream = new TerminalStream(api, task, abort.signal, { write: async () => writes++ });
  t.after(() => stream.destroy());
  const attaching = stream.start();
  abort.abort();
  resolveSnapshot(new Response('old task', { headers: { 'X-Output-Total': '8' } }));
  await attaching;
  assert.equal(writes, 0);
  assert.equal(stream.active, false);
});
