import test from 'node:test';
import assert from 'node:assert/strict';
import {TerminalStream} from '../../terminal/terminal-stream.js';
import {TaskClipboard} from '../../tasks/task-clipboard.js';

function fixture(t, request) {
  const original = globalThis.document, reads = [], logs = [];
  t.mock.method(console, 'debug', (...args) => logs.push(args));
  const notice = {};
  globalThis.document = {activeElement: null, getElementById: () => notice,
    createElement: () => ({dataset: {}, addEventListener() {}})};
  t.after(() => { globalThis.document = original; });
  let focus = 0;
  const clipboard = new TaskClipboard({request: (path, options) => { reads.push({path, ...options}); return request(path); }},
    {prepend() {}}, () => focus++);
  return {clipboard, reads, logs, notice, focus: () => focus};
}
const staged = text => Response.json({text});

test('selection reads exact task path, 204 hides and empty staging remains present', async t => {
  const {clipboard, reads} = fixture(t, async path => path.includes('/empty/') ? new Response(null, {status:204}) : staged(''));
  await clipboard.select({id:'a/b'});
  assert.equal(reads[0].path, '/api/tasks/a%2Fb/clipboard');
  assert.equal(clipboard.control.hidden, false);
  assert.equal(clipboard.text, '');
  assert.equal(clipboard.control.dataset.tooltip, 'Empty staged text');
  await clipboard.select({id:'a/b'}); assert.equal(reads.length, 1);
  await clipboard.select({id:'empty'}); assert.equal(clipboard.control.hidden, true);
  await clipboard.select(null); assert.equal(clipboard.text, null);
});

test('selection and live revisions fence delayed reads including ignored cancellation', async t => {
  const pending = [];
  const {clipboard, reads} = fixture(t, () => new Promise(resolve => pending.push(resolve)));
  const a = clipboard.select({id:'a'}), b = clipboard.select({id:'b'});
  assert.equal(reads[0].signal.aborted, true);
  clipboard.update('a','wrong task'); assert.equal(clipboard.text, null);
  clipboard.update('b','live');
  pending[1](staged('old b')); await b;
  pending[0](staged('old a')); await a;
  assert.equal(clipboard.text,'live');
  const refresh = clipboard.update('b',undefined);
  pending[2](staged('fresh b')); await refresh;
  assert.equal(clipboard.text,'fresh b');
  const late = clipboard.load(); clipboard.select(null);
  pending[3](staged('after disconnect')); await late;
  assert.equal(clipboard.control.hidden,true);
});

test('clearing restores only owned focus and bounds plaintext preview', async t => {
  const {clipboard, focus} = fixture(t, async () => staged('A'.repeat(140) + '\n<script>'));
  await clipboard.select({id:'a'});
  assert.equal(clipboard.control.dataset.tooltip,'A'.repeat(120)+'…');
  clipboard.update('a', '<b>plain</b>\nnext');
  assert.equal(clipboard.control.dataset.tooltip,'<b>plain</b> next');
  clipboard.update('a',null); assert.equal(focus(),0);
  clipboard.update('a','again'); document.activeElement=clipboard.control;
  clipboard.update('a',null); assert.equal(focus(),1);
});

for (const result of ['failure','invalid']) test(`staging ${result} is isolated and logs no payload`, async t => {
  const {clipboard, logs} = fixture(t, async () => {
    if(result==='failure') throw Object.assign(new Error('secret text'),{status:503});
    return Response.json({text:7});
  });
  await clipboard.select({id:'a'});
  assert.equal(clipboard.control.hidden,true);
  assert.match(JSON.stringify(logs),/load failed/);
  assert.doesNotMatch(JSON.stringify(logs),/secret text/);
  clipboard.update('a','working'); assert.equal(clipboard.text,'working');
});

test('repeated copies preserve exact staging and shared feedback reports failures', async t => {
  t.mock.timers.enable({apis:['setTimeout']});
  const descriptor = Object.getOwnPropertyDescriptor(globalThis,'navigator'), copied = [];
  Object.defineProperty(globalThis,'navigator',{configurable:true,value:{clipboard:{writeText:async text=>copied.push(text)}}});
  t.after(()=>{if(descriptor)Object.defineProperty(globalThis,'navigator',descriptor);else delete globalThis.navigator;});
  const {clipboard, reads, notice} = fixture(t, async()=>staged('hello\nworld'));
  await clipboard.select({id:'a'});
  await clipboard.copy(); await clipboard.copy();
  assert.deepEqual(copied,['hello\nworld','hello\nworld']);
  assert.equal(reads.length,1); assert.equal(clipboard.text,'hello\nworld');
  assert.equal(notice.textContent,'Copied staged text.');
  navigator.clipboard.writeText=async()=>{throw new Error('Denied');};
  assert.equal(await clipboard.copy(),false);
  assert.match(notice.textContent,/Copy failed/);
  assert.equal(clipboard.control.hidden,false);
});

for (const [name, response, expected] of [
  ['empty staging', () => staged(''), ''],
  ['absent staging', () => new Response(null, {status: 204}), null],
]) test(`daemon clear frame rereads ${name} and fences a newer frame`, async t => {
  const pending = [];
  const {clipboard, reads} = fixture(t, () => new Promise(resolve => pending.push(resolve)));
  const selection = clipboard.select({id:'a'});
  pending.shift()(staged('initial')); await selection;
  const original = globalThis.EventSource;
  globalThis.EventSource = class extends EventTarget { close() {} };
  t.after(() => { globalThis.EventSource = original; });
  const abort = new AbortController(); t.after(() => abort.abort());
  let refreshing;
  const stream = new TerminalStream({streamUrl: path => path}, {id:'a'}, abort.signal, {
    state() {}, clipboard: value => clipboard.update('a', value),
    clipboardRefresh: () => { refreshing = clipboard.update('a', undefined); },
  });
  stream.open();
  const frame = value => stream.source.dispatchEvent(new MessageEvent('clipboard', {data:JSON.stringify(value)}));
  frame({cleared:true});
  assert.equal(reads.length, 2);
  pending.shift()(response()); await refreshing;
  assert.equal(clipboard.text, expected);
  assert.equal(clipboard.control.hidden, expected === null);
  frame({cleared:true});
  frame({text:'newer'});
  pending.shift()(response()); await refreshing;
  assert.equal(clipboard.text, 'newer');
  assert.equal(clipboard.control.hidden, false);
});

test('direct clear fences a pending initial GET even when cancellation is ignored', async t => {
  let resolve;
  const {clipboard} = fixture(t, () => new Promise(done => { resolve = done; }));
  const loading = clipboard.select({id:'a'});
  clipboard.update('a', null);
  resolve(staged('obsolete staging')); await loading;
  assert.equal(clipboard.control.hidden, true);
  assert.equal(clipboard.text, null);
});

test('cleared SSE reread returning 204 fences the slower initial GET', async t => {
  const pending = [];
  const {clipboard, reads} = fixture(t, () => new Promise(resolve => pending.push(resolve)));
  const loading = clipboard.select({id:'a'});
  const original = globalThis.EventSource;
  globalThis.EventSource = class extends EventTarget { close() {} };
  t.after(() => { globalThis.EventSource = original; });
  const abort = new AbortController(); t.after(() => abort.abort());
  let refreshing;
  const stream = new TerminalStream({streamUrl: path => path}, {id:'a'}, abort.signal, {
    state() {}, clipboard: value => clipboard.update('a', value),
    clipboardRefresh: () => { refreshing = clipboard.update('a', undefined); },
  });
  stream.open();
  stream.source.dispatchEvent(new MessageEvent('clipboard', {data:'{"cleared":true}'}));
  assert.equal(reads.length, 2);
  pending[1](new Response(null, {status:204})); await refreshing;
  assert.equal(clipboard.control.hidden, true);
  assert.equal(clipboard.text, null);
  pending[0](staged('obsolete staging')); await loading;
  assert.equal(clipboard.control.hidden, true);
  assert.equal(clipboard.text, null);
});
