import { log } from '../../core/api.js';

export async function readHierarchy(api, signal) {
  try {
    const roster = await api.json('/api/hera', { signal });
    log('task-hierarchy', 'loaded', { count: roster.orchestrators?.length || 0 });
    return { roster };
  } catch (error) {
    if (signal.aborted || error.name === 'AbortError' || [401, 403].includes(error.status)) throw error;
    log('task-hierarchy', 'load failed', { status: error.status || 'error' });
    return { roster: {}, warning: 'Task nesting unavailable. Refresh tasks to retry.' };
  }
}

export function taskParents(tasks, roster = {}) {
  const known = new Set(tasks.map(task => task.id));
  const parents = new Map(), attention = new Set(), failed = new Set(tasks.filter(task=>task.failure).map(task=>task.id));
  const orchestrators = [...(roster.orchestrators || [])].sort((a, b) => a.id - b.id);
  const roles = new Map(orchestrators.flatMap(orch => (orch.roles || []).map(role => [`${orch.id}:${role.role_id}`, role])));
  function bind(child, parent) {
    if (!known.has(child) || !known.has(parent) || child === parent || parents.has(child)) return;
    for (let ancestor = parent; ancestor; ancestor = parents.get(ancestor)) if (ancestor === child) return;
    parents.set(child, parent);
  }
  const bindings = [...orchestrators.flatMap(orch => orch.roles || []), ...(roster.freelance || [])];
  for (const role of bindings) {
    if (!role.live || !role.task_id || role.archived) continue;
    if (role.needs_input || role.status === 'blocked') attention.add(role.task_id);
    if (role.status === 'failed') failed.add(role.task_id);
  }
  for (const orch of orchestrators) {
    const bound = (orch.roles || []).filter(role => role.live && role.task_id);
    const coordinator = bound.find(role => role.kind === 'coordinator');
    for (const role of bound) {
      if (role.kind === 'worker' && coordinator) bind(role.task_id, coordinator.task_id);
    }
    const bridge = roles.get(`${orch.bridge_parent_orch_id}:${orch.bridge_parent_role_id}`);
    if (coordinator && bridge?.live) bind(coordinator.task_id, bridge.task_id);
  }
  return { parents, attention, failed };
}

export function taskForest(tasks, hierarchy) {
  const entries = new Map(tasks.map(task => [task.id, { task, children: [], needsInput: !task.archived && (task.needs_input || hierarchy.attention.has(task.id)), failed: !task.archived && hierarchy.failed.has(task.id) }]));
  const roots = [];
  for (const entry of entries.values()) {
    const parent = entries.get(hierarchy.parents.get(entry.task.id));
    if (parent) parent.children.push(entry); else roots.push(entry);
  }
  function rollup(entry) {
    for (const child of entry.children) {
      rollup(child);
      entry.needsInput ||= child.needsInput;
      entry.failed ||= child.failed;
    }
  }
  roots.forEach(rollup);
  return roots;
}
