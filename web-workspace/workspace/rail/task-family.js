import {node} from '../../shared/dom.js';
import {railChevron} from './rail-chevron.js';
import {log} from '../../core/api.js';

export function taskFamily(navigation,item,entry,query,key,coordinatorMap) {
  const {task,children}=entry;
  if(!children.length)return item;
  const collapseKey=task.id;
  const family = node('details', 'task-family');
  family.open = !navigation.collapsedTasks.has(collapseKey) || !!query;
  const summary = node('summary', 'task-disclosure');
  summary.dataset.parentTaskId = task.id;summary.dataset.rowKey=key;
  summary.setAttribute('aria-label', `Toggle children of ${task.name}`);
  const describeDisclosure = () => { summary.dataset.tooltip = `${family.open ? 'Collapse' : 'Expand'} ${children.length} ${children.length === 1 ? 'task' : 'tasks'}`; };
  describeDisclosure();
  summary.append(railChevron('task-caret'), item);
  family.append(summary);
  const descendants = node('div', 'task-children');
  descendants.append(...children.map(child => navigation.taskItem(child, query, coordinatorMap)));
  family.append(descendants);
  const setExpanded = expanded => {
    family.open = expanded;
    describeDisclosure();
    if(navigation.selection?.active)navigation.selection.prune();
    if (family.open) navigation.collapsedTasks.delete(collapseKey); else navigation.collapsedTasks.add(collapseKey);
    log('task-hierarchy', 'disclosure', { expanded: family.open });
  };
  summary.addEventListener('click', event => {
    if (event.target.closest('button,input')) return;
    event.preventDefault();
    setExpanded(!family.open);
  });
  summary.addEventListener('keydown', event => {
    if (event.target !== summary || !['ArrowRight', 'ArrowLeft'].includes(event.key)) return;
    event.preventDefault();
    setExpanded(event.key === 'ArrowRight');
  });
  return family;
}
