import test from 'node:test';
import assert from 'node:assert/strict';
import { TerminalStream } from '../../terminal/terminal-stream.js';
import { TerminalOutput } from '../../terminal/terminal-output.js';

const settle = () => new Promise(setImmediate);
const size = { cols: 120, rows: 40 };
const live = { id: 'catchup', status: 'in_progress', live: true };

for (const mode of ['cached resume', 'dropped stream']) test(`${mode} renders final output before declaring the stopped task finished`, async t => {
  t.mock.timers.enable({ apis: ['setTimeout'] });
  const task = { ...live, status: 'in_review', live: false };
  const states = [], painted = [];
  let finish;
  const api = {
    json: async path => { if (path.endsWith('/size')) throw Object.assign(new Error('gone'), { status: 404 }); return task; },
    request: async () => new Response('BEFORE\nFINAL RESULT\n', { headers: { 'X-Output-Total': '20' } }),
  };
  const stream = new TerminalStream(api, live, new AbortController().signal, {
    currentSize: () => size, updateTask() {}, state: (...args) => states.push(args),
    write: bytes => new Promise(resolve => { painted.push(new TextDecoder().decode(bytes)); finish = resolve; }),
    replace: (bytes, dimensions) => new Promise(resolve => { assert.deepEqual(dimensions, size); painted.push(new TextDecoder().decode(bytes)); finish = resolve; }),
  });
  t.after(() => stream.destroy());
  stream.cursor = 7;
  let resuming;
  if (mode === 'cached resume') { stream.suspend(); resuming = stream.resume(); }
  else { stream.reconnect(); t.mock.timers.tick(500); }
  await settle();
  assert.equal(painted.join('').includes('FINAL RESULT'), true);
  assert.equal(['recorded', 'ended'].includes(states.at(-1)[1]), false, 'wait for renderer');
  finish(); await resuming; await settle();
  assert.equal(stream.cursor, 20);
  assert.equal(['recorded', 'ended'].includes(states.at(-1)[1]), true);
  assert.equal(stream.source == null, true);
});

for (const recovery of ['contiguous', 'evicted', 'reset', 'stopped']) test(`slow xterm bounds pending bytes and recovers after drain (${recovery})`, async t => {
  const original = globalThis.EventSource, sources = [], callbacks = [], painted = [];
  globalThis.EventSource = class {
    constructor(url) { this.url = url; sources.push(this); }
    addEventListener() {}
    close() { this.closed = true; }
  };
  const term = {
    buffer: { active: { viewportY: 0, baseY: 0 } }, reset() { painted.length = 0; }, resize() {},
    write(bytes, done) { painted.push(Buffer.from(bytes)); callbacks.push(done); },
  };
  const output = new TerminalOutput(term, { querySelector() {}, addEventListener() {}, removeEventListener() {} });
  // Split one ANSI sequence across the exact pressure boundary.
  const frame = Buffer.alloc(4096, 65); frame[frame.length - 1] = 27;
  const history = Buffer.concat([Buffer.alloc(512 * 4096 - 1, 65), Buffer.from('\x1b[31mFINAL')]);
  const snapshot = recovery === 'reset' ? Buffer.from('RESET') : recovery === 'evicted' ? Buffer.alloc(4 * 1024 * 1024, 66) : history;
  const api = {
    json: async path => {
      if (recovery === 'stopped') {
        if (path.endsWith('/size')) throw Object.assign(new Error('gone'), { status: 404 });
        return { ...live, live: false, status: 'in_review' };
      }
      return path.endsWith('/size') ? size : live;
    },
    request: async () => new Response(snapshot.subarray(-1048576), { headers: { 'X-Output-Total': String(snapshot.length) } }),
    streamUrl: path => path,
  };
  const stream = new TerminalStream(api, live, new AbortController().signal, {
    state() {}, currentSize: () => size,
    write: bytes => output.write(bytes), replace: (bytes, dimensions) => output.replace(bytes, dimensions),
  });
  t.after(() => { stream.destroy(); output.destroy(); globalThis.EventSource = original; });
  stream.open(); const source = sources[0]; source.onopen();
  for (let i = 0; i < 10000; i++) {
    const bytes = i === 511 ? frame : i === 512 ? Buffer.from('[31mFINAL') : Buffer.alloc(4096, 65);
    source.onmessage({ data: bytes.toString('base64') });
  }
  assert.equal(source.closed, true, 'consumer pressure must stop SSE intake');
  assert.ok(output.bytes + (output.writing?.bytes.byteLength || 0) <= 2 * 1024 * 1024);
  assert.equal(sources.length, 1, 'catchup waits for accepted writes to drain');
  while (callbacks.length) callbacks.shift()();
  await settle();
  while (callbacks.length) callbacks.shift()();
  await settle();
  assert.deepEqual(Buffer.concat(painted), ['reset', 'evicted'].includes(recovery) ? snapshot.subarray(-1048576) : history, 'no duplicated, missing or reordered escape bytes');
  assert.equal(stream.cursor, snapshot.length);
  assert.equal(sources.length, recovery === 'stopped' ? 1 : 2);
  if (recovery !== 'stopped') assert.equal(sources[1].url, `/api/tasks/catchup/stream?since=${snapshot.length}`);
});

for (const status of ['in_review', 'in_progress']) test(`size fallback only tolerates a recorded task 404 (${status})`, async t => {
  const task = { ...live, status, live: status === 'in_progress' };
  const api = { json: async () => { throw Object.assign(new Error('gone'), { status: 404 }); } };
  const stream = new TerminalStream(api, task, new AbortController().signal, {});
  t.after(() => stream.destroy());
  if (status === 'in_review') assert.deepEqual(await stream.size(task), { cols: 80, rows: 24 });
  else await assert.rejects(stream.size(task), { status: 404 });
  api.json = async () => { throw Object.assign(new Error('unavailable'), { status: 503 }); };
  await assert.rejects(stream.size(task), { status: 503 });
});

for (const cancel of ['suspend', 'destroy']) test(`pressure drainage cannot reopen after ${cancel}`, async t => {
  const original = globalThis.EventSource, sources = [];
  globalThis.EventSource = class {
    constructor() { sources.push(this); }
    addEventListener() {} close() {}
  };
  let drain, requests = 0;
  const stream = new TerminalStream({ json() { requests++; } }, live, new AbortController().signal, {
    state() {}, write: () => new Promise(resolve => { drain = resolve; }),
  });
  stream.api.streamUrl = path => path;
  t.after(() => { stream.destroy(); globalThis.EventSource = original; });
  stream.open(); sources[0].onopen();
  const data = Buffer.alloc(2 * 1024 * 1024).toString('base64');
  sources[0].onmessage({ data });
  sources[0].onmessage({ data: 'QQ==' });
  stream[cancel](); drain(); await settle();
  assert.equal(requests, 0);
  assert.equal(sources.length, 1);
});

for (const stage of ['read', 'paint']) test(`stopped catchup cannot commit a cursor or ended state after suspension during ${stage}`, async t => {
  const states = [];
  let release;
  const deferred = () => new Promise(resolve => { release = resolve; });
  const api = {
    json: async path => path.endsWith('/size') ? size : { ...live, live: false, status: 'in_review' },
    request: async () => { if (stage === 'read') await deferred(); return new Response('final', { headers: { 'X-Output-Total': '5' } }); },
  };
  const stream = new TerminalStream(api, live, new AbortController().signal, {
    state: (...args) => states.push(args), currentSize: () => size,
    write: async () => { if (stage === 'paint') await deferred(); },
    replace: async () => { if (stage === 'paint') await deferred(); },
  });
  t.after(() => stream.destroy());
  stream.cursor = 2; stream.suspend(); const resuming = stream.resume(); await settle();
  stream.suspend(); release(); await resuming;
  assert.equal(stream.cursor, 2);
  assert.equal(states.some(([, kind]) => ['recorded', 'ended'].includes(kind)), false);
});
