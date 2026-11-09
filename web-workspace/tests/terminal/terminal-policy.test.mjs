import test from 'node:test';
import assert from 'node:assert/strict';
import { composePaste, decodeOutput, isCtrlZ, isInteractive, outputCursor, terminalSize } from '../../terminal/terminal-policy.js';

test('live PTY is authoritative even when waiting idle at a prompt', () => {
  assert.equal(isInteractive({ status: 'in_progress', live: true, idle: true }), true);
  assert.equal(isInteractive({ status: 'in_progress', live: false, idle: false }), false);
  assert.equal(isInteractive({ status: 'in_review', live: true }), true);
  assert.equal(isInteractive({ status: 'in_progress', live: true, archived: true }), false);
  assert.equal(isInteractive({ status: 'in_progress', idle: false }), true);
  assert.equal(isInteractive({ status: 'in_progress', live: true, archived: false, archive_pending: true }), false);
});

test('Ctrl+Z guard covers Shift and alternate layouts without breaking browser undo', () => {
  assert.equal(isCtrlZ({ ctrlKey: true, key: 'Z', shiftKey: true }), true);
  assert.equal(isCtrlZ({ ctrlKey: true, key: 'я', keyCode: 90 }), true);
  assert.equal(isCtrlZ({ ctrlKey: true, key: 'z', metaKey: true }), false);
  assert.equal(isCtrlZ({ ctrlKey: true, key: 'z', altKey: true }), false);
});

test('raw output preserves UTF-8 and escape bytes without text decoding', () => {
  const bytes = Uint8Array.from([0x1b, 0x5b, 0x32, 0x4a, 0xe2, 0x82, 0xac, 0xff]);
  assert.deepEqual(decodeOutput(Buffer.from(bytes).toString('base64')), bytes);
  assert.throws(() => decodeOutput('!broken!'));
});

test('cursor accepts only exact nonnegative byte offsets', () => {
  for (const value of ['0', '1024', '9007199254740991']) assert.equal(outputCursor(value), Number(value));
  for (const value of [null, '', '-1', '2.5', 'NaN', '1e6', '9007199254740992']) {
    assert.throws(() => outputCursor(value));
  }
});

test('sizes reject zero, impossible geometry and noninteger values', () => {
  assert.equal(terminalSize({ cols: 120, rows: 40 }), true);
  for (const size of [{ cols: 0, rows: 40 }, { cols: 80, rows: 0 }, { cols: 1001, rows: 20 },
    { cols: 2.5, rows: 20 }, { cols: '80', rows: 20 }]) assert.equal(terminalSize(size), false);
});

test('compose sends one protected paste without trailing dictation newline', () => {
  assert.equal(composePaste('first\nsecond\r\n'), '\x1b[200~first\nsecond\x1b[201~');
  for (const value of ['', '\n', 'before\x1b[201~after', 'pause\x1a', 'nul\0']) {
    assert.throws(() => composePaste(value));
  }
});

test('compose counts UTF-8 bytes and paste markers against daemon limit', () => {
  assert.equal(new TextEncoder().encode(composePaste('a'.repeat(65524))).length, 65536);
  assert.throws(() => composePaste('a'.repeat(65525)));
  assert.throws(() => composePaste('€'.repeat(22000)));
});

test('plain shell compose omits paste markers and refuses embedded line breaks', () => {
  assert.equal(composePaste('printf "verified\\n"\r\n', false), 'printf "verified\\n"');
  for (const value of ['first\nsecond', 'first\rsecond']) {
    assert.throws(() => composePaste(value, false), /Use the terminal or enable bracketed paste/);
  }
  for (const value of ['pause\x1a', 'nul\0', 'before\x1b[201~after']) {
    assert.throws(() => composePaste(value, false), /control characters/);
  }
  assert.equal(new TextEncoder().encode(composePaste('a'.repeat(65536), false)).length, 65536);
  assert.throws(() => composePaste('a'.repeat(65537), false), /64 KB/);
});

test('fit hint requires a valid measured difference from authoritative PTY cells', async () => {
  const { terminalFitNeeded } = await import('../../terminal/terminal-policy.js');
  assert.equal(terminalFitNeeded({cols:80,rows:24},{cols:160,rows:60}),true);
  assert.equal(terminalFitNeeded({cols:80,rows:24},{cols:80,rows:24}),false);
  assert.equal(terminalFitNeeded({cols:80,rows:24},null),false);
  assert.equal(terminalFitNeeded({cols:80,rows:24},{cols:0,rows:0}),false);
});

test('retained output appends only unseen bytes and detects replay gaps or reset counters', async () => {
  const { retainedOutput } = await import('../../terminal/terminal-policy.js');
  assert.deepEqual([...retainedOutput(12,15,new Uint8Array([10,11,12,13,14]))],[12,13,14]);
  assert.equal(retainedOutput(5,15,new Uint8Array([10,11,12,13,14])),null);
  assert.equal(retainedOutput(20,15,new Uint8Array([10,11,12,13,14])),null);
  assert.deepEqual([...retainedOutput(15,15,new Uint8Array([10,11,12,13,14]))],[]);
});
