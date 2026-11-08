import {captureFocus} from '../../shared/focus-recovery.js';
import {SSEFrames,patchEvent} from './workspace-events-policy.js';
import {node,button} from '../../shared/dom.js';
import {log} from '../../core/api.js';
export class WorkspaceEvents {
 constructor(api,callbacks){
  Object.assign(this,{api,callbacks,generation:0,pending:[],buffering:true,cursor:0,archivePending:new Map(),archiveRevision:0,snapshotArchives:new Map(),lifetime:new AbortController()});
  document.addEventListener('visibilitychange',()=>{if(this.running&&!document.hidden)this.callbacks.refresh();});
  this.banner=node('div','connection-banner');this.banner.id='connection-banner';this.show(false);this.banner.setAttribute('role','status');
  this.message=node('span');this.retry=button('Retry connection',()=>this.reconnect());this.banner.append(this.message,this.retry);document.querySelector('.workspace-header').after(this.banner);
 }
 recover(){return captureFocus(this.banner,()=>[document.querySelector('.settings-back'),document.getElementById('draft'),document.getElementById('active-filter'),document.getElementById('tab-agent')].find(control=>control&&!control.disabled&&!control.closest('[hidden],[inert]')&&control.getClientRects().length),{signal:this.lifetime.signal});}
 show(visible){const restore=!visible?this.recover():()=>{};this.banner.hidden=!visible;document.body.classList.toggle('events-warning',visible);restore();}
 async start(){this.stop();this.lifetime=new AbortController();this.running=true;this.buffering=true;this.fallback=setInterval(()=>{if(this.running&&!document.hidden)this.callbacks.refresh();else if(this.running)log('events','skipped hidden poll');},30000);await this.open();}
 beginSnapshot(){this.buffering=true;this.snapshotArchives=new Map(this.archivePending);}
 snapshot(tasks,previousTasks=[]){
  let resnapshot=false;for(const event of this.pending){const result=patchEvent(tasks,event);tasks=result.tasks;resnapshot ||= result.resnapshot;}
  const pendingBefore=this.archivePending.size;
  for(const [id,revision] of this.snapshotArchives)if(this.archivePending.get(id)===revision)this.archivePending.delete(id);
  if(pendingBefore!==this.archivePending.size)log('events','archive reconciled',{count:pendingBefore-this.archivePending.size});
  const previous=new Map(previousTasks.map(task=>[task.id,task]));
  tasks=tasks.map(task=>{
   const archive_pending=this.archivePending.has(task.id),retained=previous.get(task.id);
   return {...task,...(archive_pending&&retained?{archived:retained.archived}:{}),archive_pending};
  });
  this.pending=[];this.buffering=false;
  if(resnapshot||this.archivePending.size)this.scheduleSnapshot();return tasks;
 }
 async open(){
  const generation=++this.generation;this.abort?.abort();this.abort=new AbortController();const abort=this.abort,signal=abort.signal;
  this.buffering=true;const timeout=setTimeout(()=>abort.abort(),10000);
  try{
   const response=await this.api.request('/api/events/stream?since='+this.cursor,{signal});clearTimeout(timeout);
   if(!this.running||generation!==this.generation)return;
   if(!response.headers.get('content-type')?.includes('text/event-stream'))throw new Error('Live events unavailable');
   this.show(false);log('events','connected');
   this.consume(response,generation,signal);
  }catch(error){clearTimeout(timeout);if(this.running&&generation===this.generation)this.failed(error);}
 }
 async consume(response,generation,signal){
  try{
   const reader=response.body.getReader(),decoder=new TextDecoder(),parser=new SSEFrames();
   while(!signal.aborted&&generation===this.generation){const {done,value}=await reader.read();if(done)throw new Error('Event stream ended');
    for(const event of parser.feed(decoder.decode(value,{stream:true}))){
     if(Number.isSafeInteger(event.id)&&event.id>0){if(event.id<=this.cursor)continue;this.cursor=event.id;}
     if(['session.started','session.exited'].includes(event.type))this.callbacks.context?.(event);
     if(event.type==='task.context_changed'){
      this.callbacks.context?.(event);
     }else if(event.type==='task.archived'){
      this.archivePending.set(event.task_id,++this.archiveRevision);
      this.callbacks.event(event);this.scheduleSnapshot();
      log('events','archive pending',{count:this.archivePending.size});
     }else if(this.buffering){
      this.pending.push(event);if(this.pending.length>10000)this.pending=[{type:'resync'}];
     }else this.callbacks.event(event);
    }
   }
  }catch(error){if(this.running&&generation===this.generation&&!signal.aborted)this.failed(error);}
 }
 failed(error){
  this.message.textContent='Connection interrupted. Live updates are retrying; tasks and drafts are kept.';this.show(true);log('events','connection failed',{status:error.status||0});
  clearTimeout(this.timer);this.timer=setTimeout(()=>this.reconnect(),8000);
 }
 async reconnect(){if(!this.running)return;const restore=this.recover();clearTimeout(this.timer);this.timer=null;this.retry.disabled=true;try{await this.open();if(this.running)await this.callbacks.refresh();}finally{this.retry.disabled=false;restore();}}
 scheduleSnapshot(){clearTimeout(this.snapshotTimer);this.snapshotTimer=setTimeout(()=>{if(this.running)this.callbacks.refresh();},100);}
 problem(message){this.message.textContent=message;this.show(true);}
 healthy(){if(!this.timer)this.show(false);}
 stop(){this.lifetime.abort();this.running=false;clearInterval(this.fallback);this.generation++;this.abort?.abort();clearTimeout(this.timer);clearTimeout(this.snapshotTimer);this.timer=null;this.pending=[];this.cursor=0;this.archivePending.clear();this.snapshotArchives.clear();this.show(false);this.retry.disabled=false;}
}
