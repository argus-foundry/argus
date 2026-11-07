export function taskSettingsURL(url, id) {
  const next = new URL(url); next.searchParams.delete('settings');
  if (id) next.searchParams.set('task_settings', id); else next.searchParams.delete('task_settings');
  return next;
}
export {moveAccounts as taskAccounts} from '../../core/account-policy.js';
export const sessionActions = {
  stop: ['Stop agent', 'Stop the agent and mark this task In review. The worktree is kept.'],
  resume: ['Resume agent', 'Resume the saved conversation or start an agent if no saved session is available.'],
  account: ['Move account', 'Move this Claude task to the selected account and resume its conversation there. A live session can be restarted.'],
  delete: ['Delete task', 'Delete this task, session log and artifacts. Worktree and local/remote branch cleanup is requested. This cannot be undone.'],
};
