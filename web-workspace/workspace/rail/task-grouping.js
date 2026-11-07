// The daemon leaves ready_to_close set when a done worker resumes, so working outranks it (TUI BUG-F).
export function roleStatus(role) {
  if (!role) return '';
  if (!role.live) return 'unbound';
  if (['blocked','failed','working'].includes(role.status)) return role.status;
  return role.ready_to_close ? 'ready' : role.status || '';
}
const rank = role => ({blocked: 4, failed: 3, working: 2, ready: 1})[roleStatus(role)] || 0;
export function taskRoles(roster = {}) {
  const result = new Map();
  for (const role of [...(roster.orchestrators || []).flatMap(orch => orch.roles || []), ...(roster.freelance || [])]) {
    if (!role.live || !role.task_id) continue;
    const previous = result.get(role.task_id);
    if (!previous || rank(role) > rank(previous)) result.set(role.task_id, role);
  }
  return result;
}
