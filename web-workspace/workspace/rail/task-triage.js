import {node} from '../../shared/dom.js';
import {kanbanLabel} from './hera-kanban.js';

export function taskTriage(item,row,{task,query,coordinatorMap,hierarchy}) {
  if(query){
    let root=task.id;for(let parent=hierarchy.parents.get(root);parent;parent=hierarchy.parents.get(root))root=parent;
    const triage=coordinatorMap.get(root);
    if(triage?.root)row.setAttribute('aria-label',`${row.getAttribute('aria-label')}, ${kanbanLabel(triage.kanban_status)}`);
  }
  const coordinator=coordinatorMap.get(task.id);
  if(coordinator?.root&&coordinator.kanban_status==='blocked'){
    item.classList.add('triage-blocked');item.append(node('span','task-triage','Blocked'));
    if(!query)row.setAttribute('aria-label',`${row.getAttribute('aria-label')}, Blocked`);
  }
}
