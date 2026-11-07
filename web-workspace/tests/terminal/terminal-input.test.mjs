import test from 'node:test';
import assert from 'node:assert/strict';
import { TerminalInput } from '../../terminal/terminal-input.js';

const task = { id: 'isolated-shell', status: 'in_progress', live: true };

function inputFixture(bracketed, json = async () => task) {
  const writes = [];
  const api = { inputTask: () => task, json, request: async (path, options) => writes.push({ path, body: options.body }) };
  return { writes, input: new TerminalInput(api, task, new AbortController().signal, () => true, bracketed) };
}

test('plain shell sends one unwrapped line and a separate submit', async () => {
  const { writes, input } = inputFixture(() => false);
  await input.send('printf "verified\\n"');
  assert.deepEqual(writes, [
    { path: '/api/tasks/isolated-shell/input', body: 'printf "verified\\n"' },
    { path: '/api/tasks/isolated-shell/input', body: '\r' },
  ]);
});

test('bracketed paste mode protects multiline text before separate submit', async () => {
  const { writes, input } = inputFixture(() => true);
  await input.send('first\nsecond');
  assert.deepEqual(writes.map(write => write.body), ['\x1b[200~first\nsecond\x1b[201~', '\r']);
});

test('plain shell refuses multiline compose before writing any bytes', async () => {
  const { writes, input } = inputFixture(() => false);
  assert.throws(() => input.send('first\nsecond'), /Use the terminal or enable bracketed paste/);
  assert.equal(writes.length, 0);
});

test('paste mode is rechecked after live verification before writing', async () => {
  let bracketed = true;
  const { writes, input } = inputFixture(() => bracketed, async () => {
    bracketed = false;
    return task;
  });
  await assert.rejects(input.send('first\nsecond'), /Use the terminal or enable bracketed paste/);
  assert.equal(writes.length, 0);
});

test('embedded Ctrl+Z is stripped while all other control bytes preserve order', async () => {
  const {writes,input}=inputFixture(()=>true);
  await input.raw('a\x03\x1a\x19\x1b[b\x1a');
  assert.deepEqual(writes.map(write=>write.body),['a\x03\x19\x1b[b']);
});
test('lone Ctrl+Z is swallowed without posting or disabling input', async () => {
  const {writes,input}=inputFixture(()=>true);
  await input.raw('\x1a\x1a');await input.raw('still connected');
  assert.deepEqual(writes.map(write=>write.body),['still connected']);
});
