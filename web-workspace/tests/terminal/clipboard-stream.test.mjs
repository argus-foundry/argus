import test from 'node:test';
import assert from 'node:assert/strict';
import {TerminalStream} from '../../terminal/terminal-stream.js';

test('clipboard frames deliver staging without consuming terminal output bytes', t => {
  const original = globalThis.EventSource, sources = [], values = [];
  globalThis.EventSource = class extends EventTarget {
    constructor() { super(); sources.push(this); }
    close() {}
  };
  t.after(() => { globalThis.EventSource = original; });
  const abort = new AbortController(); t.after(() => abort.abort());
  let refreshes = 0;
  const stream = new TerminalStream({streamUrl: path => path}, {id:'a'}, abort.signal, {
    state() {}, clipboard: value => values.push(value), clipboardRefresh: () => refreshes++,
  });
  stream.cursor = 42; stream.open();
  const source = sources[0]; source.onopen();
  const frame = data => source.dispatchEvent(new MessageEvent('clipboard', {data}));
  frame('{"text":"staged"}'); frame('{"text":""}'); frame('{"cleared":true}');
  frame('{'); frame('null'); frame('{"text":17}');
  assert.deepEqual(values, ['staged', '']);
  assert.equal(refreshes, 2);
  assert.equal(stream.cursor, 42);
  stream.open(); sources[1].onopen(); frame('{"text":"stale"}');
  assert.equal(refreshes, 3);
  assert.deepEqual(values, ['staged', '']);
  abort.abort(); sources[1].dispatchEvent(new MessageEvent('clipboard',{data:'{"text":"late"}'}));
  assert.deepEqual(values, ['staged', '']);
});
