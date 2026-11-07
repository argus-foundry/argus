import test from 'node:test';
import assert from 'node:assert/strict';
import { sendsMessage, insertPlaintext } from '../../composer/composer-policy.js';

test('ordinary Enter sends and Shift+Enter remains a newline', () => {
  assert.equal(sendsMessage({ key: 'Enter' }), true);
  assert.equal(sendsMessage({ key: 'Enter', shiftKey: true }), false);
  assert.equal(sendsMessage({ key: 'Enter', ctrlKey: true }), true);
  assert.equal(sendsMessage({ key: 'Enter', metaKey: true }), true);
});

test('composition confirmation, legacy IME input, repeats and other keys never send', () => {
  for (const event of [{ key: 'Enter', isComposing: true }, { key: 'Enter', keyCode: 229 },
    { key: 'Enter', repeat: true }, { key: 'Enter', altKey: true }, { key: 'v', metaKey: true }]) {
    assert.equal(sendsMessage(event), false);
  }
  assert.equal(sendsMessage({ key: 'Enter' }, true), false);
});

for (const mode of ['edit', 'no-change', 'unsupported']) test(`plaintext insertion replaces the range once with ${mode}`, () => {
  let fallback = 0;
  const field = { value: 'before OLD after', selectionStart: 7, selectionEnd: 10 };
  field.setRangeText = (text, start, end, selection) => {
    fallback++; assert.equal(selection, 'end'); field.value = field.value.slice(0, start) + text + field.value.slice(end);
  };
  field.ownerDocument = { execCommand: (command, unused, text) => {
    assert.equal(command, 'insertText');
    if (mode === 'unsupported') throw new Error('unsupported');
    if (mode === 'edit') field.value = field.value.slice(0, 7) + text + field.value.slice(10);
  } };
  insertPlaintext(field, 'new\ntext');
  assert.equal(field.value, 'before new\ntext after');
  assert.equal(fallback, mode === 'edit' ? 0 : 1);
});

// Identity is read from a captured task, never inferred from a model default.
test('composer identity shows only explicit backend and model', async () => {
  const { composerIdentity } = await import('../../composer/composer-identity.js');
  assert.equal(composerIdentity(null), 'No agent selected');
  assert.equal(composerIdentity({ backend: 'codex' }, { model: 'gpt-6.1-sol' }), 'codex · gpt-6.1-sol');
  assert.equal(composerIdentity({ backend: 'claude' }, {}), 'claude');
});

test('empty composer has no placeholder copy and retains its accessible label', async () => {
  const { readFile } = await import('node:fs/promises');
  const html = await readFile(new URL('../../index.html', import.meta.url), 'utf8');
  const draft = html.match(/<textarea\b[^>]*id="draft"[^>]*>/)[0];
  assert.equal((draft.match(/placeholder="([^"]*)"/)?.[1] || '').trim(), '');
  assert.match(html, /<label for="draft" class="sr-only">Message the selected agent<\/label>/);
});
