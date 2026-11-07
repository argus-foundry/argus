import {confirmDialog} from '../../shared/confirm-dialog.js';
import { node, button } from '../../shared/dom.js';
import { field } from '../settings-fields.js';
import { log } from '../../core/api.js';
import { copyCredential } from './settings-credential-copy.js';

export class ShareShortcut {
  constructor({ api, host, status }) {
    this.abort = new AbortController();
    host.append(node('h3', '', 'iOS share sheet'));
    host.append(node('p', 'settings-warning', 'The generated URL contains the connected API token. Treat it like a password. Copying it places the credential in your clipboard and any synced Shortcut backups.'));
    const origin = field(host, 'Original Argus URL', 'shareOrigin', '', 'url', 'Use the original Argus origin reachable from the sharing device, not this optional workspace address.'); origin.placeholder = 'https://your-argus-host';
    const result = node('div');
    const hide = () => { for (const input of result.querySelectorAll('input')) input.value = ''; result.replaceChildren(); };
    const generate = button('Generate share URL', () => {
      hide();
      try {
        const value = api.shareURL(origin.value.trim()), output = field(result, 'iOS share URL', 'shareURL', value); output.readOnly = true;
        result.append(node('p', 'settings-note', 'In Shortcuts, replace [Shortcut Input] in the URL with the Shortcut Input variable, then add Open URLs. Enable the shortcut in the share sheet.'));
        const copy = button('Copy share URL', () => copyCredential(value, status, this.abort.signal));
        const conceal = button('Hide share URL', () => {
          const ownedFocus = document.activeElement === conceal;
          hide();
          if (ownedFocus) { generate.focus({ preventScroll: true }); log('settings-share', 'hide focus restored'); }
        });
        result.append(copy, conceal);
        result.append(button('Test share URL', async () => { if (await confirmDialog({signal:this.abort.signal,message:'Open the original Argus share page with sample text? This leaves the workspace.'})) { const test = new URL(value); test.searchParams.set('text', 'Hello from Argus test'); location.assign(test.href); } }));
      } catch (error) { status(error.message, true); }
    });
    host.append(generate, result);
    this.clear = hide;
  }
  destroy() { this.abort.abort(); this.clear(); }
}
