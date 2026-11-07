import { LocalAlertSettings } from './settings-alerts.js';
import { node } from '../../shared/dom.js';
import { BrowserPushSettings } from './settings-push.js';
import { DeviceTokens } from './settings-tokens.js';
import { ShareShortcut } from './settings-share.js';

export class NotificationSettings {
  constructor(context) {
    Object.assign(this, context); this.host.replaceChildren(node('h2', '', 'Notifications & devices')); this.children = [];
    for (const Section of [LocalAlertSettings, BrowserPushSettings, DeviceTokens, ShareShortcut]) {
      const fields = node('fieldset', 'settings-device-section'), message = node('p', 'settings-note'); message.setAttribute('role', 'status'); this.host.append(fields, message);
      this.children.push(new Section({ ...context, host: fields, status: (text, error) => { message.textContent = text; message.classList.toggle('error-text', !!error); }, busy: (value, options) => this.setBusy(value, options) }));
    }
  }
  setBusy(value, options) { for (const fields of this.host.querySelectorAll('fieldset')) fields.disabled = value; this.busy(value, options); }
  destroy() { for (const child of this.children) child.destroy(); }
}
