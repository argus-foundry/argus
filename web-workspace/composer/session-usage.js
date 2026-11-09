import {SessionUsageView} from './session-usage-view.js';
import {log} from '../core/api.js';
import {tokenCount} from './composer-context-policy.js';

export function usageCounters(value) {
 return [['Input','input_tokens'],['Cached input (included in input)','cached_input_tokens'],['Output','output_tokens']].map(([label,key])=>[label,typeof value?.[key]==='number'&&Number.isFinite(value[key])&&value[key]>=0?tokenCount(value[key]):'Unavailable']);
}
export class SessionUsage {
 constructor(api,changed){Object.assign(this,{api,changed,generation:0,data:null});}
 select(task){this.cancel();this.task=task;this.session=task?.session_id;this.expectedSession=undefined;this.data=null;this.error=null;this.unsupportedUntil=0;}
 cancel(){this.view?.cancel();this.generation++;this.abort?.abort();this.flight=null;this.queued=false;this.busy=false;}
 event(event){
  if(event.task_id!==this.task?.id)return;
  if(['session.started','session.exited'].includes(event.type)){this.select(this.task);this.session=undefined;this.changed();this.refresh();}
  if(event.type==='task.context_changed'&&event.payload?.session_id&&event.payload.session_id!==this.session){
   this.cancel();this.data=null;this.session=event.payload.session_id;this.expectedSession=this.session;this.changed();this.refresh();return;
  }
  if(event.type!=='task.usage_changed')return;
  const session=event.payload?.session_id;
  if(session)this.expectedSession=session;
  if(session&&this.session&&session!==this.session){this.cancel();this.data=null;this.session=session;this.changed();}
  if(session&&!this.session)this.session=session;
  if(this.flight){this.queued=true;return;}this.refresh();
 }
 refresh(){return this.read(false);}
 older(){return this.read(true);}
 read(older){
  if(!this.task||!this.api)return Promise.resolve();
  if(globalThis.document?.hidden){log('session-usage','deferred hidden read');return Promise.resolve();}
  if(!older&&this.unsupportedUntil>Date.now())return Promise.resolve();
  if(this.flight)return this.flight;
  if(older&&!this.data?.next_cursor)return Promise.resolve();
  const generation=this.generation,task=this.task,session=older?this.data.session_id:this.expectedSession;
  const abort=this.abort=new AbortController(),query=new URLSearchParams({limit:'50'});
  if(older&&session)query.set('session_id',session);
  if(older)query.set('before',this.data.next_cursor);
  this.busy=true;this.changed();
  const flight=this.fetch(task,query,older,session,generation,abort).finally(()=>{
   if(this.flight!==flight)return;this.flight=null;this.busy=false;this.changed();
   if(this.queued){this.queued=false;this.refresh();}
  });
  this.flight=flight;return flight;
 }
 async fetch(task,query,older,session,generation,abort){
  try{
   const path=`/api/tasks/${encodeURIComponent(task.id)}/usage?${query}`,options={signal:AbortSignal.any([abort.signal,AbortSignal.timeout(10000)])};
   let result;
   if(this.api.request){
    const response=await this.api.request(path,{...options,allowErrorResponse:true});
    if(!older&&(response.status===404||response.headers.get('content-type')?.includes('text/html'))){
     if(generation===this.generation&&!abort.signal.aborted){this.data=null;this.error=null;this.unsupportedUntil=Date.now()+60000;log('session-usage','telemetry unsupported');}return;
    }
    if(!response.ok)throw Object.assign(new Error('Usage request failed'),{status:response.status});
    result=await response.json();
   }else result=await this.api.json(path,options);
   if(generation!==this.generation||abort.signal.aborted||this.task!==task)return;
   if(result?.task_id!==task.id||(session||this.expectedSession)&&result.session_id!==(session||this.expectedSession))throw new Error('Usage session changed. Refresh usage.');
   const turns=older?[...this.data.turns,...(result.turns||[])]:result.turns||[];
   this.data={...result,turns:[...new Map(turns.map(turn=>[turn.turn_id,turn])).values()].slice(0,500)};
   this.session=result.session_id;this.expectedSession=undefined;this.error=null;log('session-usage','loaded',{count:this.data.turns.length});
  }catch(error){
   if(generation!==this.generation||abort.signal.aborted)return;
   this.error=error.status===401||error.status===403?'Token expired. Reconnect to read usage.':'Usage unavailable. '+error.message;
   if(!older){this.data=null;this.expectedSession=undefined;this.session=undefined;}log('session-usage','read failed',{status:error.status||0});
  }
 }
 render(){
  if(globalThis.document?.hidden&&this.view)return this.view.group;
  this.view ||= new SessionUsageView(()=>this.refresh(),()=>this.older());
  return this.view.update(this.data,this.error,this.busy);
 }
}
