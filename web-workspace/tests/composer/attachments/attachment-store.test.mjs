import test from 'node:test';
import assert from 'node:assert/strict';
import { AttachmentStore } from '../../../composer/attachments/attachment-store.js';

const settle = () => new Promise(setImmediate);
const file = name => new File(['isolated contents'], name, { type: 'image/png' });

test('uploads use captured task, files multipart and browser Content-Type', async t => {
  const requests = [];
  const store = new AttachmentStore({ json: async (path, options) => {
    requests.push({ path, options }); return { paths: ['./.context/screen.png'] };
  } });
  t.after(() => store.clear());
  store.add('task/a', [file('screen.png')]);
  await settle();
  assert.equal(requests[0].path, '/api/tasks/task%2Fa/upload');
  assert.equal(requests[0].options.method, 'POST');
  assert.equal(requests[0].options.headers, undefined);
  assert.equal(requests[0].options.body.get('files').name, 'screen.png');
  assert.equal(store.list('task/a')[0].path, './.context/screen.png');
  assert.equal(store.blocked('task/a'), false);
});

test('late upload results stay with their task and removed results cannot return', async t => {
  const releases = [];
  const store = new AttachmentStore({ json: () => new Promise(resolve => releases.push(resolve)) });
  t.after(() => store.clear());
  store.add('a', [file('a.png')]);
  store.add('b', [file('b.png')]);
  const first = store.list('a')[0], second = store.list('b')[0];
  const revoke = t.mock.method(URL, 'revokeObjectURL');
  store.remove(first);
  assert.equal(first.abort.signal.aborted, true);
  releases[1]({ paths: ['./.context/b.png'] });
  releases[0]({ paths: ['./.context/a.png'] });
  await settle();
  assert.deepEqual(store.list('a'), []);
  assert.equal(second.path, './.context/b.png');
  assert.equal(revoke.mock.calls.some(call => call.arguments[0] === first.url), true);
});

test('timed-out uploads are unconfirmed and retry only after an explicit action', async t => {
  t.mock.timers.enable({ apis: ['setTimeout'] });
  let calls = 0;
  const store = new AttachmentStore({ json: async (path, { signal }) => {
    if (++calls > 1) return { paths: ['./.context/retried.png'] };
    return new Promise((resolve, reject) => signal.addEventListener('abort', () => reject(new DOMException('Timed out', 'AbortError'))));
  } });
  t.after(() => store.clear());
  store.add('a', [file('screen.png')]);
  assert.equal(store.blocked('a'), true);
  t.mock.timers.tick(280000);
  await settle();
  const item = store.list('a')[0];
  assert.equal(item.state, 'error');
  assert.match(item.error, /may have completed/);
  assert.equal(calls, 1);
  await store.upload(item);
  assert.equal(item.state, 'ready');
  assert.equal(item.path, './.context/retried.png');
});

test('consuming a sent snapshot preserves newer attachments and clear aborts pending files', async t => {
  let release;
  const store = new AttachmentStore({ json: async () => ({ paths: ['./.context/screen.png'] }) });
  t.after(() => store.clear());
  store.add('a', [file('first.png')]);
  await settle();
  const sent = store.ready('a');
  store.api = { json: () => new Promise(resolve => { release = resolve; }) };
  store.add('a', [file('second.png')]);
  const next = store.list('a')[1];
  store.consume(sent);
  assert.deepEqual(store.list('a'), [next]);
  store.clear();
  assert.equal(next.abort.signal.aborted, true);
  release({ paths: ['./.context/second.png'] });
  await settle();
  assert.deepEqual(store.list('a'), []);
});
