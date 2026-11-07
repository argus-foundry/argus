import {projectKanban} from './kanban-group.js';
import {taskForest} from './task-hierarchy.js';
import {projectGroup} from './project-group.js';
import {log} from '../../core/api.js';

export function drawTaskGroups(navigation, filtered, query, coordinatorMap) {
  const fragment = document.createDocumentFragment();
  const groups = new Map(query ? [] : navigation.projects.map(project => [project.name, []]));
  for (const entry of taskForest(filtered, navigation.hierarchy)) {
    const project = entry.task.project || 'Other tasks';
    if (!groups.has(project)) groups.set(project, []);
    groups.get(project).push(entry);
  }
  for (const [project, entries] of [...groups].sort(([left], [right]) => left.localeCompare(right))) fragment.append(projectGroup(project, entries, {
    selection:navigation.selection,
    collapsed:navigation.collapsed.has(project) && !query,
    onCollapse:collapsed => { if (collapsed) navigation.collapsed.add(project); else navigation.collapsed.delete(project); if(navigation.selection?.active)navigation.selection.prune(); log('projects', 'disclosure', {status:!collapsed}); },
    onCreate:navigation.onCreate, onConfigure:navigation.actions.projectConfiguration,
    renderTask:entry => navigation.taskItem(entry, query, coordinatorMap),
    renderEntries:entries => projectKanban(navigation,project,entries,query,coordinatorMap),
  }));
  return fragment;
}
