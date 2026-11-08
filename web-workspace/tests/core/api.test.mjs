import test from 'node:test';
import assert from 'node:assert/strict';
import { Api, log } from '../../core/api.js';

test('keeps credentials in memory and attaches authorization only to relative API requests', async t => {
  const original = globalThis.fetch;
  const calls = [];
  globalThis.fetch = async (path, options) => { calls.push({ path, options }); return new Response('{"tasks":[]}'); };
  t.after(() => { globalThis.fetch = original; });
  const api = new Api();
  api.setToken(' test-token ');
  assert.deepEqual(await api.json('/api/tasks'), { tasks: [] });
  assert.equal(calls[0].options.headers.get('Authorization'), 'Bearer test-token');
  assert.equal(calls[0].options.redirect, 'error');
  for (const path of ['https://example.com/api/tasks', '//example.com/api/tasks', '/api/../private']) {
    await assert.rejects(api.request(path));
  }
  assert.equal(calls.length, 1);
  api.setToken('');
  await api.request('/api/tasks');
  assert.equal(calls[1].options.headers.get('Authorization'), null);
});

test('exposes HTTP status without including response contents or tokens in error messages', async t => {
  const original = globalThis.fetch;
  globalThis.fetch = async () => new Response('private error body', { status: 401 });
  t.after(() => { globalThis.fetch = original; });
  await assert.rejects(new Api().request('/api/tasks'), error => {
    assert.equal(error.status, 401);
    assert.doesNotMatch(error.message, /private/);
    return true;
  });
});

test('stream URL preserves cursor and encodes credential; logging discards content fields', () => {
  const oldLocation = globalThis.location;
  globalThis.location = { origin: 'http://127.0.0.1:7750' };
  const oldDebug = console.debug;
  const events = [];
  console.debug = (...args) => events.push(args);
  try {
    const api = new Api();
    api.setToken('a&b');
    const url = new URL(api.streamUrl('/api/tasks/a/stream?since=12'));
    assert.equal(url.searchParams.get('token'), 'a&b');
    assert.equal(url.searchParams.get('since'), '12');
    log('test', 'operation', { token: 'secret', text: 'private', status: 200, bytes: 123 });
    assert.deepEqual(events[0][1], { status: 200, bytes: 123 });
  } finally { globalThis.location = oldLocation; console.debug = oldDebug; }
});

test('share shortcut generation stays local, validates its origin and clears with credentials', () => {
  const api = new Api(); api.setToken('isolated&token');
  const url = new URL(api.shareURL('https://argus.example.invalid'));
  assert.equal(url.pathname, '/share'); assert.equal(url.searchParams.get('token'), 'isolated&token'); assert.equal(url.searchParams.get('text'), '[Shortcut Input]');
  assert.equal(new URL(api.shareURL('http://127.0.0.1:17744')).port, '17744');
  for (const origin of ['http://example.com', 'javascript:alert(1)', 'https://user:pass@example.com', 'https://example.com/path', 'https://example.com/?token=other']) assert.throws(() => api.shareURL(origin));
  api.setToken(''); assert.throws(() => api.shareURL('https://argus.example.invalid'));
});

test('only explicit error-response requests expose build output while generic errors stay sanitized', async t => {
  const original = globalThis.fetch, calls = []; globalThis.fetch = async (_path, options) => { calls.push(options); return new Response('{"output":"fixture build output"}', { status: 500 }); }; t.after(() => { globalThis.fetch = original; });
  const api = new Api(), response = await api.request('/api/update', { method: 'POST', allowErrorResponse: true });
  assert.equal(response.status, 500); assert.deepEqual(await response.json(), { output: 'fixture build output' }); assert.equal('allowErrorResponse' in calls[0], false);
  await assert.rejects(api.request('/api/update', { method: 'POST' }), error => error.status === 500 && !error.message.includes('fixture build output'));
});
