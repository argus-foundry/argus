import {node,preference,savePreference} from '../../shared/dom.js';
import {railChevron} from './rail-chevron.js';
import {kanbanGroups,kanbanLabel} from './hera-kanban.js';
import {log} from '../../core/api.js';

export class KanbanDisclosure {
  constructor(){const saved=preference('kanban-groups',{});this.state=saved&&typeof saved==='object'&&!Array.isArray(saved)?saved:{};}
  key(project,status){return JSON.stringify([project,status]);}
  collapsed(project,status){
    if(!['backlog','done'].includes(status))return false;
    const saved=this.state[this.key(project,status)];return typeof saved==='boolean'?saved:true;
  }
  set(project,status,collapsed){if(!['backlog','done'].includes(status))return;this.state[this.key(project,status)]=collapsed;savePreference('kanban-groups',this.state);}
}

function footerLabel(status,entries,navigation) {
  let input=0,attention=0;
  const count=entry=>{
    if(!entry.task.archived){
      if(entry.task.needs_input||navigation.hierarchy.attention.has(entry.task.id))input++;
      else if(navigation.hierarchy.failed.has(entry.task.id))attention++;
    }
    entry.children.forEach(count);
  };
  entries.forEach(count);
  return [`${kanbanLabel(status)} (${entries.length})`,input&&`${input} needs input`,attention&&`${attention} needs attention`].filter(Boolean).join(', ');
}

export function projectKanban(navigation,project,entries,query,coordinatorMap) {
  const {plain,groups}=kanbanGroups(entries,coordinatorMap);
  const current=groups.filter(({status})=>['active','blocked'].includes(status));
  const deferred=groups.filter(({status})=>['backlog','done'].includes(status));
  return [...current.flatMap(({entries})=>entries.map(entry=>navigation.taskItem(entry,query,coordinatorMap))),
    ...plain.map(entry=>navigation.taskItem(entry,query,coordinatorMap)),
    ...deferred.map(({status,entries})=>{
    const group=node('details','kanban-group');group.dataset.kanban=status;
    group.open=!!query||!navigation.kanbanDisclosure.collapsed(project,status);
    const header=node('summary','kanban-header');header.dataset.kanbanKey=navigation.kanbanDisclosure.key(project,status);
    const label=footerLabel(status,entries,navigation);
    header.setAttribute('aria-label',`${label} task groups in ${project}`);
    header.append(railChevron('kanban-chevron'),node('span','',label));group.append(header);
    const content=node('div','kanban-children');content.append(...entries.map(entry=>navigation.taskItem(entry,query,coordinatorMap)));group.append(content);
    const expand=open=>{group.open=open;if(navigation.selection?.active)navigation.selection.prune();navigation.kanbanDisclosure.set(project,status,!open);log('hera-kanban','disclosure',{status:open});};
    header.addEventListener('click',event=>{event.preventDefault();expand(!group.open);});
    header.addEventListener('keydown',event=>{if(['ArrowLeft','ArrowRight'].includes(event.key)){event.preventDefault();expand(event.key==='ArrowRight');}});
    return group;
  })];
}
