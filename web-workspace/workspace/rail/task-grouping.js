// Working is self-reported. Only a busy bound session can outrank a retained ready marker.
export function roleStatus(role, task) {
  if (!role) return '';
  if (!role.live) return 'unbound';
  if (role.status === 'working' && !(task?.live && !task.idle)) return role.ready_to_close ? 'ready' : task?.live ? 'idle' : 'stopped';
  if (['blocked','failed','working'].includes(role.status)) return role.status;
  return role.ready_to_close ? 'ready' : role.status || '';
}
const rank = (role, task) => ({blocked: 4, failed: 3, working: 2, ready: 1})[roleStatus(role, task)] || 0;
export function taskRoles(roster = {}, tasks = []) {
  const result = new Map(), byId = new Map(tasks.map(task => [task.id, task]));
  for (const role of [...(roster.orchestrators || []).flatMap(orch => orch.roles || []), ...(roster.freelance || [])]) {
    if (!role.live || !role.task_id) continue;
    const previous = result.get(role.task_id);
    const task = byId.get(role.task_id);
    if (!previous || rank(role, task) > rank(previous, task)) result.set(role.task_id, role);
  }
  return result;
}
