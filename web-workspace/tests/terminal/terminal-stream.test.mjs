import test from 'node:test';
import assert from 'node:assert/strict';
import { TerminalStream } from '../../terminal/terminal-stream.js';
import { TerminalOutput } from '../../terminal/terminal-output.js';

test('reconnect refresh uses the new snapshot cursor after backend replacement', async t => {
  const OriginalEventSource = globalThis.EventSource;
  const opened = [];
  globalThis.EventSource = class {
    constructor(url) { opened.push(url); }
    addEventListener() {}
    close() {}
  };
  t.after(() => { globalThis.EventSource = OriginalEventSource; });
  for (const freshCursor of [12, 1200, 2400]) {
    await t.test(`new cursor ${freshCursor} replaces the old cursor 1200`, async child => {
      child.mock.timers.enable({ apis: ['setTimeout'] });
      const controller = new AbortController();
      const replaced = [];
      const requested = [];
      const task = { id: 'isolated-task', status: 'in_progress', live: true };
      const bytes = new TextEncoder().encode('fresh output');
      const api = {
        json: async path => { requested.push(path); return path.endsWith('/size') ? { cols: 120, rows: 40 } : task; },
        request: async path => {
          requested.push(path);
          return new Response(bytes, { headers: { 'X-Output-Total': String(freshCursor) } });
        },
        streamUrl: path => path,
      };
      const stream = new TerminalStream(api, task, controller.signal, {
        state() {},
        replace: async (data, size) => { replaced.push({ data, size }); },
      });
      try {
        stream.cursor = 1200;
        stream.attempts = 3;
        stream.reconnect();
        child.mock.timers.tick(4000);
        await new Promise(setImmediate);
        assert.equal(stream.cursor, freshCursor);
        assert.equal(stream.attempts, 4, 'automatic refresh preserves retry budget');
        assert.equal(opened.at(-1), `/api/tasks/isolated-task/stream?since=${freshCursor}`);
        assert.deepEqual(requested, ['/api/tasks/isolated-task', '/api/tasks/isolated-task/size', '/api/tasks/isolated-task/output?bytes=1048576']);
        assert.deepEqual(replaced, [{ data: bytes, size: { cols: 120, rows: 40 } }]);
      } finally { controller.abort(); }
    });
  }
});

for (const interrupted of ['initial snapshot', 'cached append', 'cached replacement']) {
  for (const overlap of [false, true]) test(`suspending during ${interrupted} replaces interrupted output on return (${overlap ? 'pending' : 'settled'} renderer)`, async t => {
    const original = globalThis.EventSource, opened = [];
    globalThis.EventSource = class {
      constructor(url) { opened.push(url); }
      addEventListener() {}
      close() {}
    };
    t.after(() => { globalThis.EventSource = original; });
    const task = { id: 'cached-render', status: 'in_progress', live: true };
    let history = 'ABC', painted = '', finish;
    const size = { cols: 80, rows: 24 };
    const term = {
      buffer: { active: { viewportY: 0, baseY: 0 } },
      reset() { painted = ''; }, resize() {},
      write(bytes, callback) { painted += new TextDecoder().decode(bytes); finish = callback; },
    };
    const output = new TerminalOutput(term, { querySelector() {}, addEventListener() {}, removeEventListener() {} });
    const api = {
      json: async path => path.endsWith('/size') ? size : task,
      request: async () => new Response(history, { headers: { 'X-Output-Total': String(Buffer.byteLength(history)) } }),
      streamUrl: path => path,
    };
    const abort = new AbortController();
    t.after(() => { abort.abort(); output.destroy(); });
    const stream = new TerminalStream(api, task, abort.signal, {
      state() {}, currentSize: () => size,
      write: bytes => output.write(bytes), replace: (bytes, size) => output.replace(bytes, size),
    });
    let rendering = stream.start();
    await new Promise(setImmediate);
    if (interrupted !== 'initial snapshot') {
      finish(); await rendering;
      stream.suspend();
      history = interrupted === 'cached append' ? 'ABCDEF' : 'XY';
      rendering = stream.resume();
      await new Promise(setImmediate);
    }
    stream.suspend();
    let returning;
    if (overlap) { returning = stream.resume(); await new Promise(setImmediate); }
    finish(); await rendering;
    assert.equal(painted, history, 'The interrupted renderer still paints its bytes');
    returning ??= stream.resume();
    await new Promise(setImmediate);
    finish(); await returning;
    assert.equal(painted, history, 'Returning must show each snapshot byte exactly once');
    assert.equal(stream.cursor, Buffer.byteLength(history));
    assert.equal(opened.at(-1), `/api/tasks/cached-render/stream?since=${stream.cursor}`);
  });
}

test('suspended cached output releases its source and resumes without replay duplication', async t => {
  const original=globalThis.EventSource,opened=[];
  globalThis.EventSource=class{constructor(url){opened.push(url);}addEventListener(){}close(){}};
  t.after(()=>{globalThis.EventSource=original;});
  for(const [name,cursor,replace] of [['contiguous',12,false],['reset',20,true],['evicted',5,true]])await t.test(name,async()=>{
    const abort=new AbortController(),written=[],replaced=[];let closed=0;
    const task={id:'cached',status:'in_progress',live:true};
    const api={json:async path=>path.endsWith('/size')?{cols:80,rows:24}:task,request:async()=>new Response(new Uint8Array([10,11,12,13,14]),{headers:{'X-Output-Total':'15'}}),streamUrl:path=>path};
    const stream=new TerminalStream(api,task,abort.signal,{state(){},currentSize:()=>({cols:80,rows:24}),write:async bytes=>written.push([...bytes]),replace:async bytes=>replaced.push([...bytes])});
    stream.cursor=cursor;stream.source={close(){closed++;}};stream.suspend();assert.equal(closed,1);assert.equal(stream.source,null);
    await stream.resume();assert.equal(stream.cursor,15);assert.equal(opened.at(-1),'/api/tasks/cached/stream?since=15');
    assert.deepEqual(written,replace?[]:[[12,13,14]]);assert.deepEqual(replaced,replace?[[10,11,12,13,14]]:[]);abort.abort();
  });
});
