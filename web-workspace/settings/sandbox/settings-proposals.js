import {confirmDialog} from '../../shared/confirm-dialog.js';
import { node, button, errorMessage } from '../../shared/dom.js';
import { field, select, values, settingsSignal } from '../settings-fields.js';
import { log } from '../../core/api.js';

export class SandboxProposals {
  constructor(context) { Object.assign(this, context); this.abort = new AbortController(); this.drafts = new Map(); this.load(); }
  async load() {
    this.host.replaceChildren(node('h2', '', 'Write-path proposals'));
    try {
      const result = await this.api.json('/api/sandbox/proposals?status=pending', { signal: settingsSignal(this.abort.signal) });
      if (this.abort.signal.aborted) return;
      const proposals = result.proposals || [], ids = new Set(proposals.map(p => p.id));
      for (const id of this.drafts.keys()) if (!ids.has(id)) this.drafts.delete(id);
      if (!proposals.length) this.host.append(node('p', 'settings-note', 'No pending write-path proposals.'));
      for (const proposal of proposals) this.render(proposal);
      log('settings-proposals', 'loaded', { count: proposals.length });
    } catch (error) {
      if (!this.abort.signal.aborted) this.host.append(errorMessage(`Proposals unavailable. ${error.message}`), button('Retry proposals', () => this.load()));
      log('settings-proposals', 'load failed', { status: error.status });
    }
  }
  render(proposal) {
    const draft = this.drafts.get(proposal.id) || { scope: proposal.project ? 'project' : 'global', entries: structuredClone(proposal.entries || []) }; this.drafts.set(proposal.id, draft);
    const card = node('fieldset', 'settings-proposal'); card.append(node('legend', '', `Task ${proposal.task_id || '?'}${proposal.project ? ' · ' + proposal.project : ''}`));
    const entries = node('div'); card.append(entries);
    const draw = () => {
      entries.replaceChildren();
      draft.entries.forEach((entry, index) => {
        const row = node('div', 'settings-proposal-entry'), input = field(row, 'Proposed path', `proposal-${proposal.id}-${index}`, entry.path);
        input.addEventListener('input', () => { entry.path = input.value; });
        if (entry.reason) row.append(node('p', 'settings-note', entry.reason));
        if (entry.warning) row.append(node('p', 'settings-warning', entry.warning));
        const remove = button('Remove path', () => {
          const ownedFocus = document.activeElement === remove;
          draft.entries.splice(index, 1); draw();
          if (ownedFocus) {
            const inputs = [...entries.querySelectorAll('input')];
            (inputs[Math.min(index, inputs.length - 1)] || add).focus({ preventScroll: true });
            log('settings-proposals', 'path focus restored', { count: inputs.length });
          }
        });
        row.append(remove); entries.append(row);
      });
    }; draw();
    const add = button('Add path', () => { draft.entries.push({ path: '' }); draw(); }); card.append(add);
    const scope = select(card, 'Grant scope', 'scope', [...(proposal.project ? [['project', `Project (${proposal.project})`]] : []), ['global', 'Global']], draft.scope);
    scope.addEventListener('change', () => { draft.scope = scope.value; });
    card.append(button('Approve paths', () => this.decide(proposal, draft, card, 'approve'), 'primary'), button('Reject proposal', () => this.decide(proposal, draft, card, 'reject'))); this.host.append(card);
  }
  async decide(proposal, draft, card, action) {
    const paths = draft.entries.map(entry => entry.path.trim()).filter(Boolean);
    if (action === 'approve' && !paths.length) { this.status('Add at least one path to approve, or reject the proposal.', true); return; }
    const warnings = draft.entries.filter(entry => entry.warning).map(entry => entry.warning).join('\n');
    if (!await confirmDialog({signal:this.abort.signal,message:action === 'approve' ? `Grant ${draft.scope} write access to:\n${paths.join('\n')}${warnings ? '\nWarnings:\n' + warnings : ''}\nApplies to new sessions only.` : 'Reject this write-path proposal?'})) return;
    card.disabled = true; this.busy(true);
    try {
      const result = await this.api.json(`/api/sandbox/proposals/${encodeURIComponent(proposal.id)}/${action}`, { method: 'POST', ...(action === 'approve' ? { headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ scope: draft.scope, paths }) } : {}), signal: settingsSignal(this.abort.signal) });
      if (this.abort.signal.aborted) return;
      if (action === 'approve') this.grantsChanged?.();
      this.status(action === 'approve' ? `Approved ${result.scope || draft.scope} paths for new sessions only. ${(result.warnings || []).join(' ')}` : 'Proposal rejected.');
      this.drafts.delete(proposal.id); await this.load(); log('settings-proposals', action, { status: true });
    } catch (error) {
      if (!this.abort.signal.aborted) this.status(error.status === 403 ? 'A master token is required to decide proposals. Your edits are kept.' : `Decision was not confirmed. Your edits are kept. Refresh before retrying. ${error.message}`, true);
      log('settings-proposals', 'decision failed', { status: error.status });
    } finally { card.disabled = false; this.busy(false); }
  }
  destroy() { this.abort.abort(); this.drafts.clear(); }
}
