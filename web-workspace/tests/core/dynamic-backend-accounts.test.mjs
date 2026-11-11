import test from 'node:test';
import assert from 'node:assert/strict';
import vm from 'node:vm';
import { readFile } from 'node:fs/promises';

const html = await readFile(new URL('../../../internal/api/static/index.html', import.meta.url), 'utf8');
const functions = ['backendTool', 'dynamicBackendTools', 'accountSupportsTool']
  .map(name => html.match(new RegExp(`function ${name}\\([\\s\\S]*?\\n}`))?.[0]);
assert.ok(functions.every(Boolean), 'dynamic account helpers remain available in the web form');
const context = { createDynamicTools: [] };
vm.runInNewContext(functions.join('\n'), context);

const backends = [
  { name: 'claude', kind: 'claude' },
  { name: 'codex', kind: 'codex' },
  { name: 'pi', kind: 'custom' },
];

test('dynamic account choices reflect configured tiers and a concrete fallback', () => {
  const tools = context.dynamicBackendTools(backends, [{ Backend: 'pi' }], 'dynamic');
  assert.deepEqual([...tools], []);
  context.createDynamicTools = tools;
  assert.equal(context.accountSupportsTool({ name: 'claude-only', supports: { claude: true } }, 'dynamic'), false);
  assert.equal(context.accountSupportsTool({ name: 'codex-only', supports: { codex: true } }, 'dynamic'), false);
  assert.equal(context.accountSupportsTool({ name: 'default' }, 'dynamic'), true);
});

test('dynamic account choices include only provider families that can be selected', () => {
  const tools = context.dynamicBackendTools(backends, [{ Backend: 'claude' }], 'dynamic');
  assert.deepEqual([...tools], ['claude']);
  context.createDynamicTools = tools;
  assert.equal(context.accountSupportsTool({ name: 'claude-only', supports: { claude: true } }, 'dynamic'), true);
  assert.equal(context.accountSupportsTool({ name: 'codex-only', supports: { codex: true } }, 'dynamic'), false);

  const fallbackTools = context.dynamicBackendTools(backends, [{ Backend: 'pi' }], 'codex');
  assert.deepEqual([...fallbackTools], ['codex']);
});
