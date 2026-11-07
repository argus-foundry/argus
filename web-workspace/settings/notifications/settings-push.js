import {confirmDialog} from '../../shared/confirm-dialog.js';
import { node, button, errorMessage } from '../../shared/dom.js';
import { settingsSignal } from '../settings-fields.js';
import { log } from '../../core/api.js';

export class BrowserPushSettings {
  constructor(context) {
    Object.assign(this, context); this.abort = new AbortController(); this.generation = 0; this.host.append(node('h3', '', 'Desktop alerts'));
    this.controls = node('div'); this.devices = node('div'); this.host.append(node('p','settings-note','Desktop alerts use Web Push through this browser’s service worker. The workspace does not create a second desktop banner.'),this.controls, button('Refresh devices', () => this.loadDevices()), this.devices);
    this.supported = typeof navigator.serviceWorker?.register === 'function' && typeof window.PushManager !== 'undefined' && typeof window.Notification?.requestPermission === 'function';
    this.render(); this.loadDevices(); this.loadBrowser();
  }
  render() {
    this.controls.replaceChildren();
    if (!this.supported) { this.controls.append(node('p', 'settings-note', 'Browser push is unavailable. A secure browser context with service worker and Push API support is required.')); return; }
    this.controls.append(node('p', 'settings-note', this.subscription ? 'This browser has a push subscription.' : 'Notifications are off for this browser.'), button(this.subscription ? 'Disable desktop alerts' : 'Enable desktop alerts', () => this.toggle()));
    this.controls.append(button('Send test notification', async () => { if (await confirmDialog({signal:this.abort.signal,message:'Send a test notification to all registered devices?'})) this.test(); }));
  }
  async loadBrowser() {
    if (!this.supported) return;
    const generation = this.generation;
    try {
      const registration = await navigator.serviceWorker.getRegistration('/'); const subscription = await registration?.pushManager.getSubscription();
      if (!this.abort.signal.aborted && generation === this.generation) { this.registration = registration; this.subscription = subscription; this.render(); }
    } catch (error) { if (!this.abort.signal.aborted && generation === this.generation) this.status(`Browser subscription state unavailable. ${error.message}`, true); }
  }
  async loadDevices() {
    this.read?.abort(); const read = this.read = new AbortController();
    try {
      const { subscriptions = [] } = await this.api.json('/api/push/subscriptions', { signal: settingsSignal(AbortSignal.any([read.signal, this.abort.signal])) });
      if (read.signal.aborted || this.abort.signal.aborted) return;
      this.devices.replaceChildren(node('h4', '', 'Registered devices'));
      if (!subscriptions.length) this.devices.append(node('p', 'settings-note', 'No registered devices.'));
      for (const device of subscriptions) {
        const row = node('div', 'settings-record'); row.append(node('strong', '', device.label || 'Device'), node('p', 'settings-note', device.endpoint_masked || 'Endpoint unavailable'));
        const remove = button('Remove device', () => this.remove(device)); remove.setAttribute('aria-label', `Remove device ${device.label || 'Device'}`); row.append(remove); this.devices.append(row);
      }
      log('settings-push', 'devices loaded', { count: subscriptions.length });
    } catch (error) { if (!read.signal.aborted && !this.abort.signal.aborted) this.devices.replaceChildren(errorMessage(`Device list unavailable. ${error.message}`)); log('settings-push', 'device read failed', { status: error.status }); }
  }
  async operation(action, cancellable = false) {
    if (this.pending) return; this.pending = true; this.generation++; this.busy(true, { cancellable });
    try { await action(); }
    catch (error) { if (!this.abort.signal.aborted) this.status(`Notification operation was not confirmed. ${error.message}`, true); log('settings-push', 'operation failed', { status: error.status }); }
    finally { this.pending = false; this.busy(false); }
  }
  toggle() {
    return this.operation(async () => {
      if (this.subscription) {
        const unsubscribed = await this.subscription.unsubscribe(); if (this.abort.signal.aborted) return;
        if (!unsubscribed) throw new Error('The browser did not confirm subscription removal.');
        this.subscription = null; this.render(); this.status('Browser notifications disabled. Remove its registered device below if it is still listed.'); log('settings-push', 'browser disabled', { status: true }); return;
      }
      if (Notification.permission !== 'granted' && await Notification.requestPermission() !== 'granted') { this.status('Notification permission was denied. Change it in browser settings before retrying.', true); return; }
      if (this.abort.signal.aborted) return;
      this.busy(true);
      const registration = this.registration || await navigator.serviceWorker.register('/workspace-push.js', { scope: '/' });
      if (this.abort.signal.aborted) return;
      this.registration = registration; await this.activated(registration);
      let subscription = await registration.pushManager.getSubscription();
      if (!subscription) {
        const { public_key } = await this.api.json('/api/push/vapid-public-key', { signal: settingsSignal(this.abort.signal) });
        const raw = atob(public_key.replace(/-/g, '+').replace(/_/g, '/')); const key = Uint8Array.from(raw, value => value.charCodeAt(0));
        if (this.abort.signal.aborted) return;
        subscription = await registration.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: key });
      }
      if (this.abort.signal.aborted) return;
      const body = subscription.toJSON();
      await this.api.json('/api/push/subscribe', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ label: navigator.userAgent.slice(0, 80), endpoint: body.endpoint, keys: body.keys }), signal: settingsSignal(this.abort.signal) });
      if (this.abort.signal.aborted) return;
      this.subscription = subscription; this.render(); this.status('Browser notifications enabled.'); await this.loadDevices(); log('settings-push', 'browser enabled', { status: true });
    }, !this.subscription && Notification.permission !== 'granted');
  }
  async activated(registration) {
    const worker = registration.installing || registration.waiting;
    if (registration.active || !worker) return;
    await new Promise((resolve, reject) => {
      const signal = settingsSignal(this.abort.signal);
      const finish = () => { worker.removeEventListener('statechange', changed); signal.removeEventListener('abort', canceled); };
      const changed = () => { if (worker.state === 'activated') { finish(); resolve(); } else if (worker.state === 'redundant') { finish(); reject(new Error('Push worker activation failed.')); } };
      const canceled = () => { finish(); reject(new Error('Push worker activation timed out or was canceled.')); };
      worker.addEventListener('statechange', changed); signal.addEventListener('abort', canceled, { once: true }); changed(); if (signal.aborted) canceled();
    });
  }
  test() { return this.operation(async () => { await this.api.json('/api/push/test', { method: 'POST', signal: settingsSignal(this.abort.signal) }); if (!this.abort.signal.aborted) this.status('Test notification requested for registered devices.'); log('settings-push', 'test requested', { status: true }); }); }
  async remove(device) {
    if (!await confirmDialog({signal:this.abort.signal,message:`Remove notification device ${device.label || 'Device'}?`})) return;
    return this.operation(async () => { await this.api.json(`/api/push/subscribe/${encodeURIComponent(device.id)}`, { method: 'DELETE', signal: settingsSignal(this.abort.signal) }); if (this.abort.signal.aborted) return; this.status('Removed registered device.'); await this.loadDevices(); log('settings-push', 'device removed', { status: true }); });
  }
  destroy() { this.generation++; this.abort.abort(); this.read?.abort(); }
}
