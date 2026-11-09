import { notice } from '../../shared/dom.js';
import { log } from '../../core/api.js';

export class RailActions {
  constructor(api,changed,pinChanged=()=>{}){Object.assign(this,{api,changed,pinChanged,pending:new Set(),pinPosts:new Set(),pinned:new Set(),knownPins:new Set(),generation:0});this.abort=new AbortController();this.pinEvents=new Map();this.pinObservations=new Map();}
  remember(task,pinned){
    this.knownPins.add(task.id);if(pinned&&!task.archived)this.pinned.add(task.id);else this.pinned.delete(task.id);
  }
  inspect(task){if(!this.knownPins.has(task.id)&&typeof task.pinned==='boolean')this.remember(task,task.pinned);}
  select(task){if(!task)return;this.inspect(task);}
  beginSnapshot(){return new Map(this.pinObservations);}
  reconcile(tasks,observations=this.beginSnapshot()){
    for(const task of tasks){
      if(!this.pinPosts.has(task.id)&&(observations.get(task.id)||0)===(this.pinObservations.get(task.id)||0)){
        if(typeof task.pinned==='boolean'){
          this.pinObservations.set(task.id,(this.pinObservations.get(task.id)||0)+1);
          this.remember(task,task.pinned);
        }
      }
    }
    this.retain(tasks);
  }
  retain(tasks){
    for(const task of tasks)if(this.knownPins.has(task.id))task.pinned=this.pinned.has(task.id);
    const active=new Set(tasks.filter(task=>!task.archived).map(task=>task.id));
    for(const id of this.pinned)if(!active.has(id))this.pinned.delete(id);
    for(const id of this.knownPins)if(!active.has(id))this.knownPins.delete(id);
  }
  async event(event,tasks){
    if(event.type!=='task.updated'||!event.payload?.changed_fields?.includes('pinned'))return;
    const task=tasks.find(task=>task.id===event.task_id);if(!task)return;
    const observation=(this.pinObservations.get(task.id)||0)+1;this.pinObservations.set(task.id,observation);
    this.pinEvents.get(task.id)?.abort();const abort=new AbortController(),generation=this.generation;this.pinEvents.set(task.id,abort);
    try{
      const snapshot=await this.api.json('/api/tasks/'+encodeURIComponent(task.id),{signal:AbortSignal.any([abort.signal,this.abort.signal,AbortSignal.timeout(10000)])});
      if(abort.signal.aborted||generation!==this.generation||this.pinObservations.get(task.id)!==observation)return;
      if(typeof snapshot.pinned!=='boolean')return;
      this.pinObservations.set(task.id,(this.pinObservations.get(task.id)||0)+1);
      task.pinned=snapshot.pinned;this.remember({...task,archived:snapshot.archived},snapshot.pinned);this.pinChanged();log('rail-actions','pin reconciled');
    }catch(error){if(!abort.signal.aborted&&generation===this.generation)log('rail-actions','pin reconciliation failed',{status:error.status||0});}
    finally{if(this.pinEvents.get(task.id)===abort)this.pinEvents.delete(task.id);}
  }
  async run(task,action){
    if(action!=='pin'||this.pending.has(task.id))return;
    const generation=this.generation,observation=(this.pinObservations.get(task.id)||0)+1;this.pinObservations.set(task.id,observation);this.pending.add(task.id);this.pinPosts.add(task.id);
    const base='/api/tasks/'+encodeURIComponent(task.id),signal=AbortSignal.any([this.abort.signal,AbortSignal.timeout(15000)]);
    try{
      this.pinEvents.get(task.id)?.abort();
      const pinned=!(typeof task.pinned==='boolean'?task.pinned:this.pinned.has(task.id));
      const result=await this.api.json(base+'/pin',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({pinned}),signal});
      if(generation!==this.generation||signal.aborted)return;
      const current=this.pinObservations.get(task.id)===observation;
      if(current){this.pinObservations.set(task.id,observation+1);Object.assign(task,{pinned:result.pinned,archived:result.archived});this.remember(task,result.pinned);}
      else log('rail-actions','newer pin observation retained');
      this.pinPosts.delete(task.id);
      notice(current?(pinned?'Task pinned.':'Task unpinned.'):'Pin change confirmed.');
      log('rail-actions','updated');await this.changed();
    }catch(error){if(generation===this.generation){notice(`Change was not confirmed. Refresh before retrying. ${error.message}`);log('rail-actions','update failed',{status:error.status||0});}}
    finally{if(generation===this.generation){this.pinPosts.delete(task.id);this.pending.delete(task.id);}}
  }
  clear(){for(const abort of this.pinEvents.values())abort.abort();this.pinEvents.clear();this.pinObservations.clear();this.generation++;this.abort.abort();this.abort=new AbortController();this.pending.clear();this.pinPosts.clear();this.pinned.clear();this.knownPins.clear();}
}
