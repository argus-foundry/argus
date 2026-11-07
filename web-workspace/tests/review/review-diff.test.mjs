import test from 'node:test';
import assert from 'node:assert/strict';
import { renderDiff } from '../../review/review-diff.js';

function element(tagName) {
  return {
    tagName, children: [], attributes: {}, dataset: {},
    append(...children) { this.children.push(...children); },
    replaceChildren(...children) { this.children = children; },
    setAttribute(name, value) { this.attributes[name] = String(value); },
    addEventListener() {}, querySelector() { return null; },
  };
}
function descendants(item) { return [item, ...item.children.flatMap(descendants)]; }

for (const mode of ['unified', 'split']) test(`${mode} diff columns have scoped table headers`, t => {
  const original = globalThis.document;
  t.after(() => { globalThis.document = original; });
  globalThis.document = { createElement: element };
  const container = element('div');
  renderDiff(container, { path: 'file.txt', mode, diff: '@@ -1 +1 @@\n-before\n+after' }, () => {});
  const headers = descendants(container).filter(item => item.tagName === 'th');
  assert.deepEqual(headers.map(item => item.textContent), mode === 'split'
    ? ['Old line', 'Before', 'New line', 'After'] : ['Old line', 'New line', 'Content']);
  assert.ok(headers.every(item => item.attributes.scope === 'col'));
});
