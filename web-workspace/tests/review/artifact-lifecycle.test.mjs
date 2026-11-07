import test from 'node:test';
import assert from 'node:assert/strict';
import { TaskReview } from '../../review/review.js';

function setup(t) {
  const active = new Set(), revoked = [], clicks = [], deferred = [];
  function element(tagName) {
    return {
      tagName, children: [], attributes: {}, dataset: {}, listeners: {},
      contains(target) { return this===target || this.children.some(child=>child.contains?.(target)); },
      append(...children) { this.children.push(...children); },
      replaceChildren(...children) { this.children = children; },
      setAttribute(name, value) { this.attributes[name] = String(value); },
      addEventListener(name, callback) { this.listeners[name] = callback; },
      remove() {},
      click() { assert.ok(active.has(this.href), 'download URL must be live at click'); clicks.push(this.href); },
    };
  }
  const panel = element('div');
  const original = globalThis.document;
  globalThis.document = { createElement: element, getElementById: () => panel, body: element('body') };
  t.after(() => { globalThis.document = original; });
  let sequence = 0;
  t.mock.method(URL, 'createObjectURL', () => { const url = `blob:artifact-${++sequence}`; active.add(url); return url; });
  t.mock.method(URL, 'revokeObjectURL', url => { active.delete(url); revoked.push(url); });
  t.mock.method(globalThis, 'setTimeout', callback => { deferred.push(callback); return deferred.length; });
  const task = { id: 'task-a' };
  const review = new TaskReview({ request: async () => new Response('invalid media', { status: 200 }) }, () => {});
  review.task = task;
  return { review, task, panel, active, revoked, clicks, deferred, element };
}
function descendants(item) { return [item, ...item.children.flatMap(descendants)]; }

for (const type of ['image', 'audio', 'video']) {
  test(`${type} decoder error after HTTP 200 announces failure and Download guidance`, async t => {
    const { review, task, panel } = setup(t);
    await review.openArtifact(task, { type, filename: `broken.${type}` });
    const media = descendants(panel).find(item => item.tagName === (type === 'image' ? 'img' : type));
    assert.ok(media, 'successful fetch must mount the preview before decode failure');
    assert.equal(descendants(panel).some(item => item.attributes.role === 'alert'), false);
    media.listeners.error();
    const alert = descendants(panel).find(item => item.attributes.role === 'alert');
    assert.ok(alert, 'decoder failure must create an alert');
    assert.match(alert.textContent, /could not be/);
    assert.match(alert.textContent, /Use Download/);
  });
  test(`${type} decoder error from a canceled selection leaves the current preview alone`, async t => {
    const { review, task, panel } = setup(t);
    await review.openArtifact(task, { type, filename: `broken.${type}` });
    const media = descendants(panel).find(item => item.tagName === (type === 'image' ? 'img' : type));
    const content = descendants(panel).find(item => item.className === 'artifact-preview');
    const children = [...content.children];
    review.cancel();
    media.listeners.error();
    assert.deepEqual(content.children, children);
  });
}

test('repeated downloads release their URLs after triggering and retain the active preview until cancel', async t => {
  const { review, task, active, clicks, deferred, revoked, element } = setup(t);
  await review.openArtifact(task, { type: 'image', filename: 'preview.png' });
  const [preview] = active;
  for (let index = 0; index < 10; index++) {
    await review.download(task, { filename: 'output.bin' }, element('button'), () => true);
    const download = clicks.at(-1);
    assert.ok(active.has(download), 'cleanup must be deferred until after the click');
    deferred.splice(0).forEach(callback => callback());
    assert.deepEqual([...active], [preview], 'completed downloads must not retain blob URLs');
    assert.deepEqual([...review.urls], [preview], 'completed downloads must leave URL tracking');
  }
  assert.equal(clicks.length, 10);
  assert.equal(revoked.length, 10);
  review.cancel();
  assert.equal(active.size, 0);
  assert.equal(review.urls.size, 0);
});

test('cancel releases a pending download and deferred cleanup preserves a new preview', async t => {
  const { review, task, active, deferred, element } = setup(t);
  review.cancel();
  await review.download(task, { filename: 'output.bin' }, element('button'), () => true);
  review.cancel();
  assert.equal(active.size, 0);
  const preview = review.makeURL(new Blob(['new preview']));
  deferred.splice(0).forEach(callback => callback());
  assert.deepEqual([...active], [preview]);
  assert.deepEqual([...review.urls], [preview]);
});
