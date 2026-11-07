import test from 'node:test';
import assert from 'node:assert/strict';
import { generalPayload, projectPayload, settingsURL } from '../../settings/settings-policy.js';

test('General saves only owned partial fields and never round-trips sandbox', () => {
  assert.deepEqual(generalPayload({ kbEnabled: true, vaultPath: ' /tmp/vault ', backend: 'bash', shareProject: 'demo', apiEnabled: false }), {
    kb: { enabled: true, metis_vault_path: '/tmp/vault' }, defaults: { backend: 'bash', share_project: 'demo' }, api: { enabled: false },
  });
});
test('project edits preserve returned overrides while changing the tri-state', () => {
  const original = { name: 'demo', path: '/old', sandbox: { enabled: false, deny_read: ['/private'], extra_write: ['/tmp'], allow_apple_events: ['com.apple.finder'] } };
  const result = projectPayload({ name: 'demo', path: '/new', branch: 'main', backend: '', sandbox: 'inherit', denyRead: '/a\n\n /b ', extraWrite: '' }, original);
  assert.deepEqual(result.sandbox, { enabled: null, deny_read: ['/a', '/b'], extra_write: [], allow_apple_events: ['com.apple.finder'] });
  assert.equal(original.path, '/old');
  assert.equal(original.sandbox.enabled, false);
});
test('project validation rejects empty identity and oversized request bodies', () => {
  const values = { name: 'new', path: '/tmp/new', branch: '', backend: '', sandbox: 'true', denyRead: '', extraWrite: '' };
  assert.equal(projectPayload(values).sandbox.enabled, true);
  assert.equal(projectPayload({ ...values, sandbox: 'false' }).sandbox.enabled, false);
  assert.throws(() => projectPayload({ ...values, name: '' }));
  assert.throws(() => projectPayload({ ...values, path: '' }));
  assert.throws(() => projectPayload({ ...values, path: 'x'.repeat(4096) }));
});
test('settings URLs preserve task/review state and remove only the settings marker', () => {
  const original = 'http://127.0.0.1:17750/?task=a&view=review&file=one.md';
  const settings = settingsURL(original, 'projects');
  assert.equal(settings.searchParams.get('settings'), 'projects');
  assert.equal(settings.searchParams.get('file'), 'one.md');
  assert.equal(settingsURL(settings, null).href, original);
});

test('Settings refuses task departure during a pending save', async () => {
  const { WorkspaceSettings } = await import('../../settings/settings-workspace.js');
  const settings = Object.assign(Object.create(WorkspaceSettings.prototype), { active: true, busy: true });
  assert.equal(settings.leaveForTask(), false);
  assert.equal(settings.active, true);
  settings.active = false; settings.busy = false;
  assert.equal(settings.leaveForTask(), true);
});

test('task settings preserves its current view when beforeOpen refuses departure', async () => {
  const { TaskSettingsView } = await import('../../tasks/settings/task-settings.js');
  const view = { active: true, task: { id: 'original' }, callbacks: { beforeOpen: () => false }, dismiss: () => assert.fail('Must preserve current view') };
  assert.equal(TaskSettingsView.prototype.open.call(view, { id: 'other' }), false);
  assert.equal(view.task.id, 'original');
  assert.equal(view.active, true);
});
