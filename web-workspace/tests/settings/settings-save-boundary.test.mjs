import test from 'node:test';
import assert from 'node:assert/strict';
import { WorkspaceSettings } from '../../settings/settings-workspace.js';
import { NotificationSettings } from '../../settings/notifications/settings-notifications.js';
import { BrowserPushSettings } from '../../settings/notifications/settings-push.js';

for (const cancel of [true, false]) test(`permission prompt is cancellable and ${cancel ? 'late permission cannot start writes' : 'subscription work blocks departure'}`, async t => {
  let releasePermission, releaseRegistration, registrations = 0;
  const originalNotification = Object.getOwnPropertyDescriptor(globalThis, 'Notification');
  const originalNavigator = Object.getOwnPropertyDescriptor(globalThis, 'navigator');
  Object.defineProperty(globalThis, 'Notification', { configurable: true, value: {
    permission: 'default', requestPermission: () => new Promise(resolve => { releasePermission = resolve; }),
  } });
  Object.defineProperty(globalThis, 'navigator', { configurable: true, value: {
    serviceWorker: { register: () => { registrations++; return new Promise(resolve => { releaseRegistration = resolve; }); } },
  } });
  t.after(() => {
    if (originalNotification) Object.defineProperty(globalThis, 'Notification', originalNotification); else delete globalThis.Notification;
    if (originalNavigator) Object.defineProperty(globalThis, 'navigator', originalNavigator); else delete globalThis.navigator;
  });
  const settings = Object.assign(Object.create(WorkspaceSettings.prototype), {
    active: true, back: {}, navigation: { setBusy() {} },
  });
  const notifications = Object.assign(Object.create(NotificationSettings.prototype), {
    host: { querySelectorAll: () => [] }, busy: (value, options) => settings.setBusy(value, options),
  });
  const push = Object.assign(Object.create(BrowserPushSettings.prototype), {
    abort: new AbortController(), generation: 0,
    busy: (value, options) => notifications.setBusy(value, options),
    status: () => assert.fail('Canceled permission must not announce success'),
  });
  const pending = push.toggle();
  assert.equal(settings.busy, true);
  assert.equal(settings.canLeave(), true);
  if (cancel) push.destroy();
  releasePermission('granted');
  await Promise.resolve();
  if (!cancel) {
    assert.equal(registrations, 1);
    assert.equal(settings.canLeave(), false);
    push.destroy(); releaseRegistration({});
  }
  await pending;
  assert.equal(registrations, cancel ? 0 : 1);
  assert.equal(settings.busy, false);
});

test('restoring a section cancels pre-write work and clears its busy state', async t => {
  const { settingsSections } = await import('../../settings/settings-sections.js');
  let destroyed = false;
  const section = { id: 'cancel-test', label: 'Cancel test', create: () => ({ destroy() {} }) };
  settingsSections.push(section);
  t.after(() => settingsSections.splice(settingsSections.indexOf(section), 1));
  const settings = Object.assign(Object.create(WorkspaceSettings.prototype), {
    connected: true, active: true, busy: true, cancellable: true, generation: 1,
    section: 'notifications', view: { destroy: () => { destroyed = true; } },
    back: {}, navigation: { setBusy() {}, select() {} }, title: {}, message() {},
  });
  settings.open(section.id, { history: false });
  assert.equal(destroyed, true);
  assert.equal(settings.section, section.id);
  assert.equal(settings.busy, false);
});
