import {replyText} from '../tasks/messages/coordinator-inbox.js';
import {captureFocus} from '../shared/focus-recovery.js';
import { $ } from '../shared/dom.js';
import { log } from '../core/api.js';
import { sendsMessage, insertPlaintext } from './composer-policy.js';
import { ComposerAttachments } from './composer-attachments.js';
import { ComposerBridge } from './composer-bridge.js';
import { ComposerSuggestion } from './composer-suggestion.js';
import { ComposerContext } from './composer-context.js';
import { ComposerIdentity } from './composer-identity.js';
import { attachmentMessage, imageFile } from './attachments/attachment-policy.js';

function text(element, value) {
  if (element.textContent !== value) element.textContent = value;
}

export class TaskComposer {
  constructor(send, api) {
    this.send = send;
    this.drafts = new Map();
    this.pending = new Set();
    this.errors = new Map();
    this.generation = 0;
    $('draft').addEventListener('input', () => this.captureDraft());
    $('draft').addEventListener('compositionstart', () => { this.composing = true; });
    $('draft').addEventListener('compositionend', () => { this.composing = false; });
    $('composer').addEventListener('submit', event => { event.preventDefault(); this.submit(); });
    $('draft').addEventListener('keydown', event => {
      if (sendsMessage(event, this.composing)) { event.preventDefault(); this.submit(); }
    });
    document.addEventListener('paste', event => this.pasteIntoComposer(event));
    this.bridge = api ? new ComposerBridge(api) : null;
    this.suggestion = new ComposerSuggestion(this.bridge, $('draft'));
    this.context = new ComposerContext(this.bridge);
    this.identity = new ComposerIdentity(api, () => { this.context.identity(this.identity.raw); this.render(); });
    this.attachments = new ComposerAttachments(api, this);
  }
  reply(task,item){
    if(this.task?.id!==task.id)return;
    $('draft').value=replyText($('draft').value,item);this.captureDraft();this.focus();
    log('composer','inbox reply drafted');
  }
  captureDraft() { if (this.task) this.drafts.set(this.task.id, $('draft').value); this.render(); }
  focus() {
    const draft = $('draft');
    if (draft.disabled || draft.closest('[inert]') || document.querySelector('dialog[open]')) return;
    draft.focus({ preventScroll: true });
  }
  pasteIntoComposer(event) {
    const draft = $('draft');
    if (event.defaultPrevented || draft.readOnly || draft.closest('[inert]') || document.querySelector('dialog[open]')) return;
    const active = document.activeElement;
    if (active !== draft && active?.closest('input, textarea, select, [contenteditable], .xterm')) return;
    if (this.attachments.paste(event)) return;
    if (!this.task || draft.disabled) return;
    const text = event.clipboardData?.getData('text/plain');
    if (!text) return;
    event.preventDefault();
    this.focus();
    insertPlaintext(draft, text);
    this.captureDraft();
    log('composer', 'paste inserted', { status: 'ok' });
  }
  select(task) {
    const restore=captureFocus($('composer'),()=>$('draft')?.disabled?$('tab-agent'):$('draft'),{neighbors:false});
    this.composing = false;
    this.task = task;
    this.bridge?.invalidate();
    this.identity.select(task);
    this.context.select(task);
    this.suggestion.select(task);
    this.attachments.message = ''; this.attachments.drop.clear();
    $('draft').value = task ? this.drafts.get(task.id) || '' : '';
    this.interactive = false;
    this.render();restore();
  }
  setInteractive(value) { this.interactive = !!value; this.render(); }
  render() {
    const restore=captureFocus($('composer'),()=>$('draft')?.disabled?$('tab-agent'):$('draft'),{neighbors:false});
    const task = this.task, pending = task && this.pending.has(task.id);
    if ($('draft').disabled !== !task) $('draft').disabled = !task;
    const attached = this.attachments.store.ready(task?.id);
    const unavailable = !task || !this.interactive || !!pending || this.attachments.store.blocked(task.id) || (!$('draft').value.trim() && !attached.length);
    if ($('send').disabled !== unavailable) $('send').disabled = unavailable;
    const sending = String(!!pending);
    if ($('send').dataset.sending !== sending) {
      $('send').dataset.sending = sending;
      $('send').innerHTML = pending
        ? '<svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="5" cy="12" r="1"/><circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/></svg><span class="sr-only">Sending…</span>'
        : '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 20V4m-7 7 7-7 7 7"/></svg>';
    }
    const sendLabel = pending ? 'Sending message' : 'Send message';
    if ($('send').getAttribute('aria-label') !== sendLabel) $('send').setAttribute('aria-label', sendLabel);
    if ($('send').dataset.tooltip !== sendLabel) $('send').dataset.tooltip = sendLabel;
    text($('composer-label'), this.identity.label());
    const error = task && (this.errors.get(task.id) || (task.archived ? 'Task is archived. Restore it before sending again. Your draft is kept.' : task.archive_pending ? 'Task input is blocked while archive state is checked. Your draft is kept.' : ''));
    if ($('composer-status').classList.contains('error') !== !!error) $('composer-status').classList.toggle('error', !!error);
    text($('composer-status'), error || (pending ? 'Sending to this task. Your draft is kept until delivery succeeds.' : task && !this.interactive ? 'The session is not accepting messages. You can keep drafting.' : 'Enter to send · Shift+Enter for a new line'));
    this.suggestion.render(); this.attachments.render();restore();
  }
  async submit() {
    const task = this.task, text = $('draft').value;
    const attached = this.attachments.store.ready(task?.id);
    if (!task || (!text.trim() && !attached.length) || !this.interactive || this.pending.has(task.id) || this.attachments.store.blocked(task.id)) return;
    const generation = this.generation;
    const fromComposer = [$('draft'), $('send')].includes(document.activeElement);
    this.suggestion.newTurn();
    this.pending.add(task.id);
    this.errors.delete(task.id);
    this.drafts.set(task.id, text);
    this.render();
    try {
      await this.send(attachmentMessage(text, attached.map(item => item.path)), task, {attachments:attached.length > 0,imagePaths:attached.filter(item=>imageFile(item.file)).map(item=>item.path)});
      if (generation !== this.generation) return;
      this.attachments.store.consume(attached);
      if (this.drafts.get(task.id) === text) {
        this.drafts.delete(task.id);
        if (this.task?.id === task.id) $('draft').value = '';
      }
      log('composer', 'sent', { status: 'ok' });
      if (this.task?.id === task.id && fromComposer &&
          [$('draft'), $('send'), document.body].includes(document.activeElement)) this.focus();
    } catch (error) {
      if (generation !== this.generation) return;
      this.errors.set(task.id, error.code === 'task_archived' ? 'Message was rejected because the task was archived. Your draft and attachments are kept. Restore the task before sending again.' : `Message was not confirmed. Your draft is kept. Check the terminal before resending. ${error.message}`);
      log('composer', 'send failed', { status: error.status || 'error' });
    } finally {
      if (generation === this.generation) { this.pending.delete(task.id); this.render(); }
    }
  }
  clear() { this.generation++; this.composing = false; this.task = null; this.bridge?.invalidate(); this.identity.select(null); this.context.select(null); this.suggestion.select(null); this.drafts.clear(); this.pending.clear(); this.errors.clear(); $('draft').value = ''; this.interactive = false; this.attachments.clear(); this.render(); }
}
