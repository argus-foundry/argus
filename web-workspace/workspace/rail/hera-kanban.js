import {notice} from '../../shared/dom.js';
import {log} from '../../core/api.js';

export const kanbanStatuses=['active','backlog','blocked','done'];
export const kanbanLabel=status=>kanbanStatuses.includes(status)&&status!=='active'?status[0].toUpperCase()+status.slice(1):'In progress';

export function coordinators(roster={}) {
  const result=new Map();
  for(const orch of [...roster.orchestrators||[]].sort((a,b)=>a.id-b.id)) {
    const coordinator=orch.roles?.find(role=>role.kind==='coordinator'&&role.live&&role.task_id&&!role.archived);
    if(coordinator&&!orch.archived&&!result.has(coordinator.task_id))result.set(coordinator.task_id,{...orch,root:!orch.bridge_parent_orch_id});
  }
  return result;
}

export function kanbanGroups(entries,coordinators) {
  const plain=[],groups=new Map(kanbanStatuses.map(status=>[status,[]]));
  for(const entry of entries) {
    const coordinator=coordinators.get(entry.task.id);
    if(!coordinator?.root){plain.push(entry);continue;}
    groups.get(kanbanStatuses.includes(coordinator.kanban_status)?coordinator.kanban_status:'active').push(entry);
  }
  return {plain,groups:[...groups].filter(([,entries])=>entries.length).map(([status,entries])=>({status,entries:entries.sort((a,b)=>Number(!!coordinators.get(b.task.id).pinned)-Number(!!coordinators.get(a.task.id).pinned))}))};
}

export class HeraKanban {
  constructor(api,render,refresh,feedback=notice) {
    Object.assign(this,{api,render,refresh,feedback,pending:new Set(),overrides:new Map(),generation:0,abort:new AbortController()});
  }
  value(coordinator){return {...coordinator,...this.overrides.get(coordinator.id)};}
  async run(coordinator,operation,status) {
    if(!coordinator||this.pending.has(coordinator.id)||!['pin','kanban'].includes(operation))return;
    if(operation==='kanban'&&(!coordinator.root||!kanbanStatuses.includes(status)))return;
    const generation=this.generation,current=this.value(coordinator);
    const change=operation==='pin'?{pinned:!current.pinned}:{kanban_status:status};
    const action=operation==='pin'?(change.pinned?'pin':'unpin'):'kanban';
    this.pending.add(coordinator.id);this.overrides.set(coordinator.id,change);this.render();
    try {
      await this.api.json(`/api/hera/orchestrators/${coordinator.id}/${action}`,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(operation==='kanban'?{status}:{}),signal:AbortSignal.any([this.abort.signal,AbortSignal.timeout(15000)])});
      if(generation!==this.generation)return;
      log('hera-kanban','updated');
      if(operation==='pin')this.feedback(change.pinned?'Task pinned.':'Task unpinned.');
      await this.refresh();
    }catch(error){
      if(generation!==this.generation)return;
      this.feedback(`Change was not confirmed. Refresh before retrying. ${error.message}`);log('hera-kanban','update failed',{status:error.status||0});
    }finally{
      if(generation===this.generation){this.pending.delete(coordinator.id);this.overrides.delete(coordinator.id);this.render();}
    }
  }
  clear(){this.generation++;this.abort.abort();this.abort=new AbortController();this.pending.clear();this.overrides.clear();}
}
