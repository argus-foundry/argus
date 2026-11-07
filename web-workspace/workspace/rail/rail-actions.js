import { notice } from '../../shared/dom.js';
import { log } from '../../core/api.js';

export class RailActions {
  constructor(api,changed,pinChanged=()=>{}){Object.assign(this,{api,changed,pinChanged,pending:new Set(),pinned:new Set(),knownPins:new Set(),generation:0});this.abort=new AbortController();}
  remember(task,pinned){
    this.knownPins.add(task.id);if(pinned&&!task.archived)this.pinned.add(task.id);else this.pinned.delete(task.id);
  }
  inspect(task){if(this.knownPins.has(task.id))return;if(typeof task.pinned==='boolean'){this.remember(task,task.pinned);return;}return this.select(task);}
  async select(task){
    this.pinRead?.abort();this.pinReadTask=task?.id;if(!task || task.archived)return;if(typeof task.pinned==='boolean'){this.remember(task,task.pinned);this.pinChanged();log('rail-actions','pin from task',{status:task.pinned});return;}const abort=this.pinRead=new AbortController(),generation=this.generation;
    try{const raw=await this.api.json('/api/tasks/'+encodeURIComponent(task.id)+'/raw',{signal:AbortSignal.any([abort.signal,this.abort.signal,AbortSignal.timeout(10000)])});if(abort.signal.aborted||generation!==this.generation)return;
      this.remember(task,!!raw.pinned&&!raw.archived);this.pinChanged();log('rail-actions','pin read');
    }catch(error){if(!abort.signal.aborted)log('rail-actions','pin read failed');}
  }
  reconcile(tasks){for(const task of tasks)if(typeof task.pinned==='boolean')this.remember(task,task.pinned);const active=new Set(tasks.filter(task=>!task.archived).map(task=>task.id));for(const id of this.pinned)if(!active.has(id))this.pinned.delete(id);for(const id of this.knownPins)if(!active.has(id))this.knownPins.delete(id);if(this.pinReadTask&&!active.has(this.pinReadTask))this.pinRead?.abort();}
  async run(task,action){
    if(action!=='pin'||this.pending.has(task.id))return;
    const generation=this.generation;this.pending.add(task.id);
    const base='/api/tasks/'+encodeURIComponent(task.id),signal=AbortSignal.any([this.abort.signal,AbortSignal.timeout(15000)]);
    try{
      this.pinRead?.abort();
      const raw=await this.api.json(base+'/raw',{signal});
      if(raw.archived)throw new Error('Restore the task before pinning it.');
      const pinned=!raw.pinned;
      await this.api.json(base+'/raw',{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify({...raw,pinned}),signal});
      if(generation!==this.generation)return;
      this.remember(task,pinned);
      notice(pinned?'Task pinned.':'Task unpinned.');
      log('rail-actions','updated');await this.changed();
    }catch(error){if(generation===this.generation){notice(`Change was not confirmed. Refresh before retrying. ${error.message}`);log('rail-actions','update failed',{status:error.status||0});}}
    finally{if(generation===this.generation)this.pending.delete(task.id);}
  }
  clear(){this.pinRead?.abort();this.generation++;this.abort.abort();this.abort=new AbortController();this.pending.clear();this.pinned.clear();this.knownPins.clear();}
}
