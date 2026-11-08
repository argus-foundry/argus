import {resolvePR} from '../../../tasks/task-information.js';
import test from 'node:test';
import assert from 'node:assert/strict';
import { taskSettingsURL, taskAccounts } from '../../../tasks/settings/task-settings-policy.js';

test('task settings routes preserve main task/review and clear competing settings routes', () => {
  const url = taskSettingsURL('http://localhost/?task=main&view=review&file=a.js&settings=repository%3Aold', 'target');
  assert.equal(url.searchParams.get('task'), 'main'); assert.equal(url.searchParams.get('view'), 'review'); assert.equal(url.searchParams.get('file'), 'a.js');
  assert.equal(url.searchParams.get('task_settings'), 'target'); assert.equal(url.searchParams.has('settings'), false);
  assert.equal(taskSettingsURL(url, null).searchParams.has('task_settings'), false);
});
test('account moves require the resolved Claude backend and another compatible account', () => {
  const accounts = [{ name: 'default', supports: { claude: true } }, { name: 'second', supports: { claude: true } }, { name: 'codex', supports: { codex: true } }];
  assert.deepEqual(taskAccounts({ backend: 'custom', account: '' }, accounts, [{ name: 'custom', kind: 'claude' }]).map(item => item.name), ['second']);
  assert.deepEqual(taskAccounts({ backend: 'custom' }, accounts, [{ name: 'custom', kind: 'codex' }]), []);
  assert.deepEqual(taskAccounts({ backend: 'claude' }, accounts, []), []);
});
for (const value of ['javascript:alert(1)', 'data:text/html,test', '', '/relative', undefined]) test(`PR link rejects unsafe or absent value ${String(value)}`, () => {
  assert.equal(resolvePR([{ namespace: 'pr', key: 'url', value }]), null);
});
test('PR link reads only cached PR URL metadata', () => {
  assert.equal(resolvePR([{ namespace: 'other', key: 'url', value: 'https://example.com' }, { namespace: 'pr', key: 'url', value: 'https://github.com/example/repo/pull/1' }]), 'https://github.com/example/repo/pull/1');
});
