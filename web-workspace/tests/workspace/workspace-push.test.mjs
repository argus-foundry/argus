import test from 'node:test';
import assert from 'node:assert/strict';
import vm from 'node:vm';
import { readFile } from 'node:fs/promises';
test('workspace push worker displays safe defaults and opens only encoded same-origin task URLs', async () => {
  const handlers = {}, shown = [], opened = [];
  const self = { location: { origin: 'http://127.0.0.1:17750' }, registration: { showNotification: async (...args) => shown.push(args) }, clients: { openWindow: async url => opened.push(url) }, addEventListener: (name, callback) => { handlers[name] = callback; } };
  vm.runInNewContext(await readFile(new URL('../../workspace-push.js', import.meta.url), 'utf8'), { self, URL });
  assert.equal(handlers.fetch, undefined);
  let pending; handlers.push({ data: { json: () => ({ title: 'Argus fixture', body: 'Body', taskId: '../?token=unsafe' }) }, waitUntil: promise => { pending = promise; } }); await pending;
  assert.equal(shown[0][0], 'Argus fixture'); assert.equal(shown[0][1].icon, '/icon-192.png');
  handlers.notificationclick({ notification: { data: shown[0][1].data, close() {} }, waitUntil: promise => { pending = promise; } }); await pending;
  const url = new URL(opened[0], self.location.origin); assert.equal(url.origin, self.location.origin); assert.equal(url.pathname, '/'); assert.equal(url.searchParams.get('task'), '../?token=unsafe'); assert.equal(url.searchParams.has('token'), false);
  handlers.push({ data: { json: () => { throw new Error('bad data'); } }, waitUntil: promise => { pending = promise; } }); await pending;
  assert.equal(shown[1][0], 'Argus');
});
