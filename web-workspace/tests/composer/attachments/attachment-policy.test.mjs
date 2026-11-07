import test from 'node:test';
import assert from 'node:assert/strict';
import { attachmentMessage, fileDrag, transferFiles, validateFiles, uploadedPath } from '../../../composer/attachments/attachment-policy.js';

test('file drags recognize protected OS payloads without intercepting text drags', () => {
  assert.equal(fileDrag({ types: ['Files'], files: [] }), true);
  assert.equal(fileDrag({ types: ['text/plain'], items: [{ kind: 'file' }] }), true);
  assert.equal(fileDrag({ types: ['text/plain'], files: [] }), false);
  assert.equal(fileDrag(null), false);
  const image = { name: 'screen.png' };
  assert.deepEqual(transferFiles({ files: [image], items: [] }), [image]);
  assert.deepEqual(transferFiles({ items: [{ kind: 'string' }, { kind: 'file', getAsFile: () => image }, { kind: 'file', getAsFile: () => null }] }), [image]);
});

test('upload policy respects existing API caps including the current draft', async t => {
  const mib = 1024 * 1024;
  for (const [name, files, existing, valid] of [
    ['boundary', [{ size: 100 * mib }, { size: 100 * mib }, { size: 50 * mib }], [], true],
    ['per file', [{ size: 100 * mib + 1 }], [], false],
    ['total', [{ size: 100 * mib }, { size: 100 * mib }, { size: 50 * mib + 1 }], [], false],
    ['count', Array.from({ length: 21 }, () => ({ size: 1 })), [], false],
    ['existing draft', [{ size: 1 }], Array.from({ length: 20 }, () => ({ size: 1 })), false],
    ['empty file', [{ size: 0 }], [], false],
  ]) await t.test(name, () => {
    if (valid) assert.doesNotThrow(() => validateFiles(files, existing));
    else assert.throws(() => validateFiles(files, existing));
  });
});

test('attachment paths are message content and never an upload-triggered newline', () => {
  assert.equal(attachmentMessage('Describe', ['./.context/a.png', './.context/b.txt']), 'Describe ./.context/a.png ./.context/b.txt');
  assert.equal(attachmentMessage('', ['./.context/a.png']), './.context/a.png');
  assert.equal(attachmentMessage('Plain text\n', []), 'Plain text\n');
  assert.equal(uploadedPath({ paths: ['./.context/screen.png'] }), './.context/screen.png');
  for (const paths of [[], ['bad'], ['./.context/../bad'], ['./.context/a\nsubmit'], ['./.context/a', './.context/b']]) assert.throws(() => uploadedPath({ paths }));
});

test('attachment messages escape terminal path tokens', async t => {
  for (const [name, text, paths, expected] of [
    ['spaces', '', ['./.context/Screenshot 2026-11-07 at 1.22.14 PM.png'], String.raw`./.context/Screenshot\ 2026-11-07\ at\ 1.22.14\ PM.png`],
    ['quotes', '', [`./.context/it's "ready".png`], String.raw`./.context/it\'s\ \"ready\".png`],
    ['backslashes', '', [String.raw`./.context/a\b.png`], String.raw`./.context/a\\b.png`],
    ['shell characters', '', ['./.context/$(a);&|<>`b`[c]*?{d}!#~^%=.png'], String.raw`./.context/\$\(a\)\;\&\|\<\>` + '\\`b\\`' + String.raw`\[c\]\*\?\{d\}\!\#\~\^\%\=.png`],
    ['multiple attachments', '', ['./.context/one image.png', './.context/two.txt'], String.raw`./.context/one\ image.png ./.context/two.txt`],
    ['text and paths', 'Describe "this"\nKeep $text literal.', ['./.context/one image.png', './.context/two.txt'], 'Describe "this"\nKeep $text literal. ' + String.raw`./.context/one\ image.png ./.context/two.txt`],
    ['simple path', '', ['./.context/screen-1_2.png'], './.context/screen-1_2.png'],
    ['unicode filename', '', ['./.context/café.png'], './.context/café.png'],
  ]) await t.test(name, () => assert.equal(attachmentMessage(text, paths), expected));
});
