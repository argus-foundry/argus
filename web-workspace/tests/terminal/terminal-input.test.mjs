import test from 'node:test';
import assert from 'node:assert/strict';
import { TerminalInput } from '../../terminal/terminal-input.js';

const task = { id: 'isolated-shell', status: 'in_progress', live: true };

function inputFixture(bracketed, json = async () => task, backend = 'shell', kind, rosterRead) {
  const writes = [], abort = new AbortController();
  const api = { inputTask: () => task, json: (path, options) => path === '/api/backends' ? (rosterRead ? rosterRead() : Promise.resolve({backends:[{name:backend,kind}]})) : json(path, options), request: async (path, options) => writes.push({ path, body: options.body }) };
  return { writes, abort, input: new TerminalInput(api, {...task,backend}, abort.signal, () => true, bracketed) };
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
for (const [kind,suppressed] of [['claude',true],['',true],['codex',false],['pi',false],['opencode',false],['antigravity',false],['cursor',false],['omp',false],[undefined,false]]) {
  test(`focus reports preserve input except Claude-family focus-out (${kind})`, async () => {
    const {writes,input}=inputFixture(()=>true,undefined,'backend-alias',kind);
    await new Promise(setImmediate);
    for (const data of ['\x1b[O','\x1b[I','\x1b[Ox','\x1bOA','\x1bOB','\x1bOC','\x1bOD']) await input.raw(data);
    const expected=['\x1b[I','\x1b[Ox','\x1bOA','\x1bOB','\x1bOC','\x1bOD'];
    if (!suppressed) expected.unshift('\x1b[O');
    assert.deepEqual(writes.map(write=>write.body),expected);
  });
}

test('failed roster holds focus-out while unmatched roster forwards it', async () => {
 for (const [json,expected] of [[async()=>{throw new Error('offline');},['after']],[async()=>({backends:[]}),['\x1b[O','after']]]) {
  const {writes,input,abort}=inputFixture(()=>false,undefined,'backend-alias','claude',json);
  try {
   await new Promise(setImmediate);
   await input.raw('\x1b[O');await input.raw('after');
   assert.deepEqual(writes.map(write=>write.body),expected);
  } finally {abort.abort();}
 }
});
