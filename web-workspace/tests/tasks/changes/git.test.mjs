import test from 'node:test';
import assert from 'node:assert/strict';
import { changedFiles, gitPath, diffLines, splitLines } from '../../../tasks/changes/git.js';

test('preserves literal paths and decodes git quoted UTF-8 paths', () => {
  assert.equal(gitPath(' leading and trailing '), ' leading and trailing ');
  assert.equal(gitPath('"notes\\tname.md"'), 'notes\tname.md');
  assert.equal(gitPath('"caf\\303\\251.md"'), 'café.md');
});

test('working changes override branch status and renames select the destination', () => {
  assert.deepEqual(changedFiles({ BranchFiles: 'M\tshared.txt\nR100\told.txt\tnew.txt',
    Status: ' D shared.txt\n?? draft/\nR  "old name.md" -> "new name.md"' }), [
    { path: 'draft/', status: '??', directory: true },
    { path: 'new name.md', status: 'R', directory: false },
    { path: 'new.txt', status: 'R100', directory: false },
    { path: 'shared.txt', status: 'D', directory: false },
  ]);
});

test('an arrow inside an ordinary quoted filename is not a rename', () => {
  assert.deepEqual(changedFiles({ Status: '?? "before -> after.txt"\n M "unchanged -> name.md"' }), [
    { path: 'before -> after.txt', status: '??', directory: false },
    { path: 'unchanged -> name.md', status: 'M', directory: false },
  ]);
});

test('unified hunks number only actual content and pair asymmetric replacements', () => {
  const rows = diffLines('diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -10,3 +10,2 @@\n context\n-old\n-another\n+new');
  assert.equal(rows[2].kind, 'meta');
  assert.deepEqual(rows.slice(4).map(({ kind, oldNumber, newNumber }) => ({ kind, oldNumber, newNumber })), [
    { kind: 'context', oldNumber: 10, newNumber: 10 },
    { kind: 'removed', oldNumber: 11, newNumber: '' },
    { kind: 'removed', oldNumber: 12, newNumber: '' },
    { kind: 'added', oldNumber: '', newNumber: 11 },
  ]);
  const split = splitLines(rows.slice(4));
  assert.equal(split[1].left.text, '-old');
  assert.equal(split[1].right.text, '+new');
  assert.equal(split[2].left.text, '-another');
  assert.equal(split[2].right, undefined);
});

test('file headers after a second diff do not inherit the previous hunk', () => {
  const rows = diffLines('diff --git a/one b/one\n@@ -1 +1 @@\n-old\n+new\ndiff --git a/two b/two\n--- a/two\n+++ b/two\n@@ -4 +4 @@\n same');
  assert.deepEqual(rows.slice(4, 7).map(row => row.kind), ['meta', 'meta', 'meta']);
  assert.equal(rows.at(-1).oldNumber, 4);
  assert.equal(rows.at(-1).newNumber, 4);
});

test('diff preview stops after the line budget and one overflow sentinel', () => {
  const diff = '@@ -0,0 +1,1000000 @@\n' + '+generated line\n'.repeat(1000000);
  const rows = diffLines(diff, 6001);
  assert.equal(rows.length, 6001);
  assert.equal(rows.at(-1).newNumber, 6000);
  assert.equal(rows.at(-1).index, 6000);
});

test('bounded diff scanning preserves empty and trailing lines without splitting the input', t => {
  t.mock.method(String.prototype, 'split', () => { throw new Error('Full input split'); });
  assert.deepEqual(diffLines('@@ -3 +4 @@\n same\n', 4).map(row => row.text), ['@@ -3 +4 @@', ' same', '']);
  assert.equal(diffLines('', 1).length, 1);
  assert.equal(diffLines('+one\n+two', 1).length, 1);
  assert.equal(diffLines('+one', 0).length, 0);
});
